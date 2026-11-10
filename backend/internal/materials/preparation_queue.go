package materials

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"lingoway/internal/generation"
	"lingoway/internal/models"
	"lingoway/internal/models/llm"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Этот файл содержит транзакции очереди. Основной путь обработки находится
// в preparation_worker.go; сетевых вызовов внутри этих транзакций нет.
const preparationLeaseCompletionGracePeriod = 30 * time.Second

// claimNextPreparationRequest берёт ожидающую работу либо восстанавливает
// running с истёкшей арендой. SKIP LOCKED позволяет другим воркерам брать
// свободные строки. Пустая очередь возвращается как nil без ошибки.
func (worker *PreparationWorker) claimNextPreparationRequest(ctx context.Context) (*llm.LLMRequest, error) {
	var preparationRequest llm.LLMRequest
	err := worker.materialService.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		err := transaction.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("operation = ? AND ((status = ? AND available_at <= clock_timestamp()) OR (status = ? AND lease_expires_at <= clock_timestamp()))",
				models.LLMPrepareMaterial, models.LLMQueued, models.LLMRunning).
			Order("available_at, id").First(&preparationRequest).Error
		if err != nil {
			return err
		}
		leaseToken, err := generateRandomUUID()
		if err != nil {
			return err
		}
		levelDetector := worker.materialService.levelDetector
		leaseDuration := worker.options.LLMRequestTimeout + preparationLeaseCompletionGracePeriod
		// Одинаковое время PostgreSQL для захвата и завершения исключает влияние
		// расхождения часов между серверами. Запас нужен на чтение и запись БД.
		err = transaction.Model(&preparationRequest).Updates(map[string]any{
			"status": models.LLMRunning, "lease_token": leaseToken,
			"lease_expires_at": gorm.Expr("clock_timestamp() + (? * interval '1 second')", leaseDuration.Seconds()),
			"attempt_count":    preparationRequest.AttemptCount + 1,
			"provider":         levelDetector.ProviderName(), "model": levelDetector.ModelName(),
		}).Error
		if err != nil {
			return err
		}
		// Получаем значения SQL-выражений, которых ещё нет в структуре в памяти.
		return transaction.First(&preparationRequest, preparationRequest.ID).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &preparationRequest, nil
}

// savePreparationOutcome выбирает один итог: повтор, успех или окончательную ошибку.
// Материал и запрос меняются в общей транзакции с проверкой действующей аренды.
func (worker *PreparationWorker) savePreparationOutcome(ctx context.Context, claimedRequest *llm.LLMRequest, detectedLevel models.CEFRLevel, detectionErr error) error {
	return worker.materialService.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		activeRequest, err := lockPreparationRequestWithActiveLease(transaction, claimedRequest)
		if err != nil {
			return err
		}
		if detectionErr == nil {
			return markMaterialPreparationSucceeded(transaction, activeRequest, detectedLevel)
		}
		if generation.IsRetryableProviderError(detectionErr) && activeRequest.AttemptCount < worker.options.MaxPreparationAttempts {
			return requeuePreparationRequest(transaction, activeRequest)
		}
		return markMaterialPreparationFailed(transaction, activeRequest, detectionErr)
	})
}

func lockPreparationRequestWithActiveLease(transaction *gorm.DB, claimedRequest *llm.LLMRequest) (*llm.LLMRequest, error) {
	var activeRequest llm.LLMRequest
	err := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND status = ? AND lease_token = ? AND lease_expires_at > clock_timestamp()",
			claimedRequest.ID, models.LLMRunning, claimedRequest.LeaseToken).First(&activeRequest).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPreparationLeaseLost
	}
	if err != nil {
		return nil, err
	}
	return &activeRequest, nil
}

func requeuePreparationRequest(transaction *gorm.DB, activeRequest *llm.LLMRequest) error {
	retryDelay := calculatePreparationRetryDelay(activeRequest.AttemptCount)
	return updatePreparationRequestWithActiveLease(transaction, activeRequest, map[string]any{
		"status": models.LLMQueued, "lease_token": nil, "lease_expires_at": nil,
		"available_at": gorm.Expr("clock_timestamp() + (? * interval '1 second')", retryDelay.Seconds()),
	})
}

func markMaterialPreparationSucceeded(transaction *gorm.DB, activeRequest *llm.LLMRequest, detectedLevel models.CEFRLevel) error {
	// Модель определяет только CEFR. Текст копируется из сохранённого источника,
	// поэтому пересказ или выдуманный текст модели не попадёт в материал.
	materialUpdates := map[string]any{
		"status": models.MaterialReady, "level": detectedLevel,
		"error_code": nil, "extracted_text": gorm.Expr("source_text"),
	}
	if err := transaction.Model(&llm.Material{}).
		Where("id = ? AND owner_user_id = ?", *activeRequest.MaterialID, activeRequest.OwnerUserID).
		Updates(materialUpdates).Error; err != nil {
		return err
	}
	resultSummary, err := json.Marshal(map[string]any{"material_id": *activeRequest.MaterialID, "level": detectedLevel})
	if err != nil {
		return err
	}
	return updatePreparationRequestWithActiveLease(transaction, activeRequest, map[string]any{
		"status": models.LLMSucceeded, "result": models.JSON(resultSummary),
		"finished_at": gorm.Expr("clock_timestamp()"), "lease_token": nil, "lease_expires_at": nil,
	})
}

func markMaterialPreparationFailed(transaction *gorm.DB, activeRequest *llm.LLMRequest, detectionErr error) error {
	errorCode := safePreparationFailureCode(detectionErr)
	if err := transaction.Model(&llm.Material{}).
		Where("id = ? AND owner_user_id = ?", *activeRequest.MaterialID, activeRequest.OwnerUserID).
		Updates(map[string]any{"status": models.MaterialFailed, "error_code": errorCode}).Error; err != nil {
		return err
	}
	return updatePreparationRequestWithActiveLease(transaction, activeRequest, map[string]any{
		"status": models.LLMFailed, "error_code": errorCode,
		"finished_at": gorm.Expr("clock_timestamp()"), "lease_token": nil, "lease_expires_at": nil,
	})
}

func updatePreparationRequestWithActiveLease(transaction *gorm.DB, activeRequest *llm.LLMRequest, requestUpdates map[string]any) error {
	// Обновление материала могло ждать чужую транзакцию. Ещё раз проверяем срок
	// аренды перед последней записью; её потеря откатывает оба изменения.
	updateResult := transaction.Model(&llm.LLMRequest{}).
		Where("id = ? AND status = ? AND lease_token = ? AND lease_expires_at > clock_timestamp()",
			activeRequest.ID, models.LLMRunning, activeRequest.LeaseToken).Updates(requestUpdates)
	if updateResult.Error != nil {
		return updateResult.Error
	}
	if updateResult.RowsAffected != 1 {
		return ErrPreparationLeaseLost
	}
	return nil
}

// Пауза растёт как 2, 4, 8, 16, 32 секунды и дальше остаётся равной 32 секундам.
func calculatePreparationRetryDelay(attemptCount int) time.Duration {
	if attemptCount > 5 {
		attemptCount = 5
	}
	if attemptCount < 1 {
		attemptCount = 1
	}
	return time.Duration(1<<attemptCount) * time.Second
}

// Неизвестный текст ошибки не сохраняется: в БД попадают только известные коды.
func safePreparationFailureCode(err error) string {
	var providerError *generation.LLMProviderError
	if errors.As(err, &providerError) {
		switch providerError.Code {
		case "LLM_NOT_CONFIGURED", "LLM_TIMEOUT", "LLM_UNAVAILABLE", "LLM_RATE_LIMITED", "LLM_PROVIDER_ERROR", "LLM_INVALID_RESPONSE", "LLM_RETRIES_EXHAUSTED", "LLM_PROMPT_UNSUPPORTED", "MATERIAL_UNSUPPORTED":
			return providerError.Code
		}
	}
	return "LLM_UNAVAILABLE"
}

// generateRandomUUID создаёт случайный UUID версии 4. Новый токен отличает
// текущий захват работы от старого после восстановления упавшего процесса.
func generateRandomUUID() (string, error) {
	var uuidBytes [16]byte
	if _, err := rand.Read(uuidBytes[:]); err != nil {
		return "", err
	}
	uuidBytes[6] = uuidBytes[6]&0x0f | 0x40
	uuidBytes[8] = uuidBytes[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", uuidBytes[0:4], uuidBytes[4:6], uuidBytes[6:8], uuidBytes[8:10], uuidBytes[10:16]), nil
}
