package materials

import (
	"context"
	"encoding/json"

	"lingoway/internal/generation"
	"lingoway/internal/models"
	"lingoway/internal/models/llm"

	"gorm.io/gorm"
)

// savePreparationOutcome выбирает один итог: повтор, успех или окончательную ошибку.
// Материал и запрос меняются в общей транзакции с проверкой действующей аренды.
func (worker *PreparationWorker) savePreparationOutcome(ctx context.Context, claimedRequest *llm.LLMRequest, detectedLevel models.CEFRLevel, detectionErr error) error {
	return worker.materialService.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		activeRequest, err := lockLLMRequestWithActiveLease(transaction, claimedRequest)
		if err != nil {
			return err
		}
		if detectionErr == nil {
			return markMaterialPreparationSucceeded(transaction, activeRequest, detectedLevel)
		}
		if canRetryLLMRequest(activeRequest, detectionErr, worker.options.MaxAttempts) {
			return requeueLLMRequest(transaction, activeRequest, generation.ProviderRetryDelay(detectionErr))
		}
		return markMaterialPreparationFailed(transaction, activeRequest, detectionErr)
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
	return completeLLMRequest(transaction, activeRequest, models.JSON(resultSummary))
}

func markMaterialPreparationFailed(transaction *gorm.DB, activeRequest *llm.LLMRequest, detectionErr error) error {
	errorCode := safeLLMFailureCode(detectionErr)
	if err := transaction.Model(&llm.Material{}).
		Where("id = ? AND owner_user_id = ?", *activeRequest.MaterialID, activeRequest.OwnerUserID).
		Updates(map[string]any{"status": models.MaterialFailed, "error_code": errorCode}).Error; err != nil {
		return err
	}
	return failLLMRequest(transaction, activeRequest, errorCode)
}
