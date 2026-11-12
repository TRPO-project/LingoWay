package materials

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"lingoway/internal/generation"
	"lingoway/internal/models"
	"lingoway/internal/models/llm"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Запас аренды даёт воркеру время прочитать источник и сохранить результат.
const llmLeaseCompletionGracePeriod = 30 * time.Second

// Ошибка означает, что запрос уже взят другим воркером или срок аренды истёк.
var errLLMRequestLeaseLost = errors.New("request lease is no longer current")

// claimNextLLMRequest берёт доступную работу нужного типа или восстанавливает
// running с истёкшей арендой. При пустой очереди возвращает nil без ошибки.
func claimNextLLMRequest(ctx context.Context, database *gorm.DB, operation models.LLMOperation, timeout time.Duration, providerName, modelName string) (*llm.LLMRequest, error) {
	var claimedRequest llm.LLMRequest
	err := database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		err := transaction.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("operation = ? AND ((status = ? AND available_at <= clock_timestamp()) OR (status = ? AND lease_expires_at <= clock_timestamp()))",
				operation, models.LLMQueued, models.LLMRunning).
			Order("available_at, id").First(&claimedRequest).Error
		if err != nil {
			return err
		}
		leaseToken, err := generateRandomUUID()
		if err != nil {
			return err
		}
		leaseDuration := timeout + llmLeaseCompletionGracePeriod
		// Одинаковое время PostgreSQL для захвата и завершения исключает влияние
		// расхождения часов между серверами. Запас нужен на чтение и запись БД.
		err = transaction.Model(&claimedRequest).Updates(map[string]any{
			"status": models.LLMRunning, "lease_token": leaseToken,
			"lease_expires_at": gorm.Expr("clock_timestamp() + (? * interval '1 second')", leaseDuration.Seconds()),
			"attempt_count":    claimedRequest.AttemptCount + 1,
			"provider":         providerName, "model": modelName,
		}).Error
		if err != nil {
			return err
		}
		// Получаем значения SQL-выражений, которых ещё нет в структуре в памяти.
		return transaction.First(&claimedRequest, claimedRequest.ID).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &claimedRequest, nil
}

func lockLLMRequestWithActiveLease(transaction *gorm.DB, claimedRequest *llm.LLMRequest) (*llm.LLMRequest, error) {
	var activeRequest llm.LLMRequest
	err := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND status = ? AND lease_token = ? AND lease_expires_at > clock_timestamp()",
			claimedRequest.ID, models.LLMRunning, claimedRequest.LeaseToken).First(&activeRequest).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errLLMRequestLeaseLost
	}
	if err != nil {
		return nil, err
	}
	return &activeRequest, nil
}

func requeueLLMRequest(transaction *gorm.DB, activeRequest *llm.LLMRequest, providerDelay time.Duration) error {
	retryDelay := max(calculateLLMRetryDelay(activeRequest.AttemptCount), providerDelay)
	return updateLLMRequestWithActiveLease(transaction, activeRequest, map[string]any{
		"status": models.LLMQueued, "lease_token": nil, "lease_expires_at": nil,
		"available_at": gorm.Expr("clock_timestamp() + (? * interval '1 second')", retryDelay.Seconds()),
	})
}

func canRetryLLMRequest(request *llm.LLMRequest, operationErr error, maxAttempts int) bool {
	return request.AttemptCount < maxAttempts && generation.IsRetryableProviderError(operationErr)
}

func completeLLMRequest(transaction *gorm.DB, request *llm.LLMRequest, result models.JSON) error {
	return updateLLMRequestWithActiveLease(transaction, request, map[string]any{
		"status": models.LLMSucceeded, "result": result,
		"finished_at": gorm.Expr("clock_timestamp()"), "lease_token": nil, "lease_expires_at": nil,
	})
}

func failLLMRequest(transaction *gorm.DB, request *llm.LLMRequest, errorCode string) error {
	return updateLLMRequestWithActiveLease(transaction, request, map[string]any{
		"status": models.LLMFailed, "error_code": errorCode,
		"finished_at": gorm.Expr("clock_timestamp()"), "lease_token": nil, "lease_expires_at": nil,
	})
}

func updateLLMRequestWithActiveLease(transaction *gorm.DB, activeRequest *llm.LLMRequest, requestUpdates map[string]any) error {
	// Запись результата могла ждать другую транзакцию. Ещё раз проверяем срок
	// аренды перед последней записью; её потеря откатывает оба изменения.
	updateResult := transaction.Model(&llm.LLMRequest{}).
		Where("id = ? AND status = ? AND lease_token = ? AND lease_expires_at > clock_timestamp()",
			activeRequest.ID, models.LLMRunning, activeRequest.LeaseToken).Updates(requestUpdates)
	if updateResult.Error != nil {
		return updateResult.Error
	}
	if updateResult.RowsAffected != 1 {
		return errLLMRequestLeaseLost
	}
	return nil
}

// Пауза растёт как 2, 4, 8, 16, 32 секунды и дальше остаётся равной 32 секундам.
func calculateLLMRetryDelay(attemptCount int) time.Duration {
	if attemptCount > 5 {
		attemptCount = 5
	}
	if attemptCount < 1 {
		attemptCount = 1
	}
	return time.Duration(1<<attemptCount) * time.Second
}

// Неизвестный текст ошибки не сохраняется: в БД попадают только известные коды.
func safeLLMFailureCode(err error) string {
	var providerError *generation.LLMProviderError
	if errors.As(err, &providerError) && providerError != nil {
		switch providerError.Code {
		case "LLM_NOT_CONFIGURED", "LLM_TIMEOUT", "LLM_UNAVAILABLE", "LLM_RATE_LIMITED", "LLM_PROVIDER_ERROR", "LLM_INVALID_RESPONSE", "LLM_INVALID_INPUT", "LLM_RETRIES_EXHAUSTED", "LLM_PROMPT_UNSUPPORTED", "MATERIAL_UNSUPPORTED":
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
