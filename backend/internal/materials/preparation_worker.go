package materials

import (
	"context"

	"lingoway/internal/generation"
	"lingoway/internal/models"
	"lingoway/internal/models/llm"
)

// PreparationWorker выполняет только prepare_material. Один экземпляр работает
// последовательно. Несколько процессов разделяют общую очередь через PostgreSQL.
type PreparationWorker struct {
	materialService *MaterialService
	options         LLMWorkerOptions
}

// NewPreparationWorker задаёт стандартные ограничения, но не запускает обработку.
func NewPreparationWorker(materialService *MaterialService, options LLMWorkerOptions) *PreparationWorker {
	return &PreparationWorker{materialService: materialService, options: options.withDefaults()}
}

// RunUntilStopped обрабатывает очередь до отмены контекста приложения.
func (worker *PreparationWorker) RunUntilStopped(ctx context.Context) {
	runLLMWorker(ctx, worker.options.QueuePollInterval, "material worker", worker.ProcessNextPreparationRequest)
}

// ProcessNextPreparationRequest проводит одну работу через три явных этапа:
// захват запроса, определение CEFR и сохранение итога. false без ошибки означает,
// что работы нет либо клиент LLM отключён. Вызов модели проходит без блокировки БД.
func (worker *PreparationWorker) ProcessNextPreparationRequest(ctx context.Context) (bool, error) {
	service := worker.materialService
	if service == nil || service.database == nil || service.levelDetector == nil || !service.levelDetector.HasAPIKeys() {
		return false, nil
	}
	preparationRequest, err := claimNextLLMRequest(ctx, service.database, models.LLMPrepareMaterial,
		worker.options.LLMRequestTimeout, service.levelDetector.ProviderName(), service.levelDetector.ModelName())
	if err != nil || preparationRequest == nil {
		return false, err
	}
	if preparationRequest.AttemptCount > worker.options.MaxAttempts {
		// Последний процесс мог упасть после захвата работы. Завершаем запрос
		// без ещё одного платного вызова, если бюджет попыток уже исчерпан.
		return true, worker.savePreparationOutcome(ctx, preparationRequest, "",
			&generation.LLMProviderError{Code: "LLM_RETRIES_EXHAUSTED"})
	}
	material, err := service.GetMaterialByID(ctx, *preparationRequest.MaterialID)
	if err != nil {
		return true, err // При временной ошибке БД оставляем аренду для восстановления.
	}
	detectedLevel, detectionErr := worker.detectMaterialCEFRLevel(ctx, preparationRequest, material)
	if ctx.Err() != nil {
		// Штатная остановка не означает окончательный failed: новый процесс
		// сможет продолжить работу после истечения текущей аренды.
		return true, ctx.Err()
	}
	return true, worker.savePreparationOutcome(ctx, preparationRequest, detectedLevel, detectionErr)
}

func (worker *PreparationWorker) detectMaterialCEFRLevel(ctx context.Context, request *llm.LLMRequest, material *llm.Material) (models.CEFRLevel, error) {
	if material.SourceText == nil || material.Kind != models.MaterialText {
		return "", &generation.LLMProviderError{Code: "MATERIAL_UNSUPPORTED"}
	}
	llmCallContext, cancelLLMCall := context.WithTimeout(ctx, worker.options.LLMRequestTimeout)
	defer cancelLLMCall()
	detectedLevel, err := worker.materialService.levelDetector.DetectCEFRLevel(llmCallContext, *material.SourceText, request.PromptVersion)
	if err != nil {
		return "", err
	}
	if !isSupportedCEFRLevel(detectedLevel) {
		return "", &generation.LLMProviderError{Code: "LLM_INVALID_RESPONSE"}
	}
	return detectedLevel, nil
}

// Повторная проверка на границе сохранения нужна и при замене клиента LLM
// другой реализацией CEFRLevelDetector.
func isSupportedCEFRLevel(level models.CEFRLevel) bool {
	switch level {
	case models.LevelA1, models.LevelA2, models.LevelB1, models.LevelB2, models.LevelC1, models.LevelC2:
		return true
	}
	return false
}
