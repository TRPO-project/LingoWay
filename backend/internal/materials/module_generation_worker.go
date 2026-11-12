package materials

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"lingoway/internal/generation"
	"lingoway/internal/models"
	"lingoway/internal/models/llm"
	"lingoway/internal/models/topics"
)

// ModuleGenerationWorker выполняет запросы generate_module из общей очереди.
type ModuleGenerationWorker struct {
	service *ModuleGenerationService
	options LLMWorkerOptions
}

func NewModuleGenerationWorker(service *ModuleGenerationService, options LLMWorkerOptions) *ModuleGenerationWorker {
	return &ModuleGenerationWorker{service: service, options: options.withDefaults()}
}

// RunUntilStopped обрабатывает очередь до отмены контекста приложения.
func (worker *ModuleGenerationWorker) RunUntilStopped(ctx context.Context) {
	runLLMWorker(ctx, worker.options.QueuePollInterval, "module generation worker", worker.processNextModuleGenerationRequest)
}

func (worker *ModuleGenerationWorker) processNextModuleGenerationRequest(ctx context.Context) (bool, error) {
	service := worker.service
	if service == nil || service.database == nil || service.generator == nil || !service.generator.HasAPIKeys() {
		return false, nil
	}
	request, err := claimNextLLMRequest(ctx, service.database, models.LLMGenerateModule,
		worker.options.LLMRequestTimeout, service.generator.ProviderName(), service.generator.ModelName())
	if err != nil || request == nil {
		return false, err
	}
	if request.AttemptCount > worker.options.MaxAttempts {
		return true, worker.saveModuleGenerationOutcome(ctx, request, nil,
			&generation.LLMProviderError{Code: "LLM_RETRIES_EXHAUSTED"})
	}
	input, err := worker.loadExerciseGenerationInput(ctx, request)
	if err != nil {
		// Ошибка БД оставляет работу до восстановления по аренде.
		var generationError *generation.LLMProviderError
		if !errors.As(err, &generationError) {
			return true, err
		}
		return true, worker.saveModuleGenerationOutcome(ctx, request, nil, err)
	}
	llmCallContext, cancelLLMCall := context.WithTimeout(ctx, worker.options.LLMRequestTimeout)
	generatedExercises, generationErr := service.generator.GenerateExercises(llmCallContext, input, request.PromptVersion)
	cancelLLMCall()
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	if generationErr == nil && !matchesExerciseGenerationPlan(input, generatedExercises) {
		generationErr = &generation.LLMProviderError{Code: "LLM_INVALID_RESPONSE"}
	}
	return true, worker.saveModuleGenerationOutcome(ctx, request, generatedExercises, generationErr)
}

func (worker *ModuleGenerationWorker) loadExerciseGenerationInput(ctx context.Context, request *llm.LLMRequest) (generation.ExerciseGenerationInput, error) {
	var input generation.ExerciseGenerationInput
	if request.PromptVersion != generation.ExerciseGenerationPromptV1 {
		return input, &generation.LLMProviderError{Code: "LLM_PROMPT_UNSUPPORTED"}
	}
	var parameters moduleGenerationParameters
	if err := json.Unmarshal(request.Parameters, &parameters); err != nil {
		return input, &generation.LLMProviderError{Code: "LLM_INVALID_INPUT"}
	}
	if _, err := generation.PlanExerciseCounts(parameters.Count, parameters.Types); err != nil {
		return input, &generation.LLMProviderError{Code: "LLM_INVALID_INPUT"}
	}
	database := worker.service.database.WithContext(ctx)
	var module topics.Module
	if err := database.Where("created_by_user_id = ?", request.OwnerUserID).First(&module, *request.ModuleID).Error; err != nil {
		return input, err
	}
	if module.Status != models.ModuleGenerating || module.MaterialID == nil || !isSupportedCEFRLevel(module.Difficulty) {
		return input, &generation.LLMProviderError{Code: "LLM_INVALID_INPUT"}
	}
	var material llm.Material
	if err := database.Select("status", "extracted_text").Where("owner_user_id = ?", request.OwnerUserID).
		First(&material, *module.MaterialID).Error; err != nil {
		return input, err
	}
	if material.Status != models.MaterialReady || material.ExtractedText == nil || strings.TrimSpace(*material.ExtractedText) == "" {
		return input, &generation.LLMProviderError{Code: "MATERIAL_UNSUPPORTED"}
	}
	return generation.ExerciseGenerationInput{
		SourceText: *material.ExtractedText, Difficulty: module.Difficulty,
		Count: parameters.Count, Types: parameters.Types,
	}, nil
}

// Проверяем план и на границе записи, чтобы замена клиента не позволила
// опубликовать неполный модуль или задания незапрошенных типов.
func matchesExerciseGenerationPlan(input generation.ExerciseGenerationInput, generatedExercises []generation.GeneratedExercise) bool {
	if len(generatedExercises) != input.Count {
		return false
	}
	plannedCounts, err := generation.PlanExerciseCounts(input.Count, input.Types)
	if err != nil {
		return false
	}
	exerciseIndex := 0
	for typeIndex, exerciseType := range input.Types {
		for range plannedCounts[typeIndex] {
			if generatedExercises[exerciseIndex].Type != exerciseType {
				return false
			}
			exerciseIndex++
		}
	}
	return true
}
