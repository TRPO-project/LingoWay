package materials

import (
	"context"
	"encoding/json"

	"lingoway/internal/generation"
	"lingoway/internal/models"
	"lingoway/internal/models/llm"
	"lingoway/internal/models/topics"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (worker *ModuleGenerationWorker) saveModuleGenerationOutcome(ctx context.Context, claimedRequest *llm.LLMRequest, exercises []generation.GeneratedExercise, generationErr error) error {
	return worker.service.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		activeRequest, err := lockLLMRequestWithActiveLease(transaction, claimedRequest)
		if err != nil {
			return err
		}
		if generationErr == nil {
			return publishGeneratedModule(transaction, activeRequest, exercises)
		}
		if canRetryLLMRequest(activeRequest, generationErr, worker.options.MaxAttempts) {
			return requeueLLMRequest(transaction, activeRequest, generation.ProviderRetryDelay(generationErr))
		}
		return markModuleGenerationFailed(transaction, activeRequest, generationErr)
	})
}

func publishGeneratedModule(transaction *gorm.DB, activeRequest *llm.LLMRequest, generatedExercises []generation.GeneratedExercise) error {
	var module topics.Module
	if err := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("created_by_user_id = ? AND status = ?", activeRequest.OwnerUserID, models.ModuleGenerating).
		First(&module, *activeRequest.ModuleID).Error; err != nil {
		return err
	}
	exercises := make([]topics.Exercise, len(generatedExercises))
	for index, generated := range generatedExercises {
		exercises[index] = topics.Exercise{
			ModuleID: module.ID, Position: index + 1, Type: generated.Type,
			Instruction: generated.Instruction, QuestionText: generated.QuestionText,
			Content: generated.Content, AnswerKey: generated.AnswerKey, Explanation: generated.Explanation,
		}
	}
	// Триггер БД разрешает вставку заданий до публикации. Смена статуса,
	// результат запроса и все задания фиксируются общей транзакцией.
	if err := transaction.Create(&exercises).Error; err != nil {
		return err
	}
	if err := transaction.Model(&module).Updates(map[string]any{
		"status": models.ModuleReady, "published_at": gorm.Expr("clock_timestamp()"),
	}).Error; err != nil {
		return err
	}
	resultSummary, err := json.Marshal(map[string]any{"module_id": module.ID, "exercise_count": len(exercises)})
	if err != nil {
		return err
	}
	return completeLLMRequest(transaction, activeRequest, models.JSON(resultSummary))
}

func markModuleGenerationFailed(transaction *gorm.DB, activeRequest *llm.LLMRequest, generationErr error) error {
	if err := transaction.Model(&topics.Module{}).
		Where("id = ? AND created_by_user_id = ? AND status = ?", *activeRequest.ModuleID, activeRequest.OwnerUserID, models.ModuleGenerating).
		Update("status", models.ModuleFailed).Error; err != nil {
		return err
	}
	errorCode := safeLLMFailureCode(generationErr)
	return failLLMRequest(transaction, activeRequest, errorCode)
}
