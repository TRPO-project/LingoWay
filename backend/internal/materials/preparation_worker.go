package materials

import (
	"context"
	"log"
	"time"

	"lingoway/internal/generation"
	"lingoway/internal/models"
	"lingoway/internal/models/llm"
)

// PreparationWorkerOptions ограничивает время вызова LLM и число попыток.
// QueuePollInterval относится к опросу очереди в БД, а не к запросам фронтенда.
type PreparationWorkerOptions struct {
	LLMRequestTimeout      time.Duration
	QueuePollInterval      time.Duration
	MaxPreparationAttempts int
}

// PreparationWorker выполняет только prepare_material. Один экземпляр работает
// последовательно; несколько процессов разделяют общую очередь через PostgreSQL.
type PreparationWorker struct {
	materialService *MaterialService
	options         PreparationWorkerOptions
}

// NewPreparationWorker задаёт стандартные ограничения, но не запускает обработку.
func NewPreparationWorker(materialService *MaterialService, options PreparationWorkerOptions) *PreparationWorker {
	if options.LLMRequestTimeout <= 0 {
		options.LLMRequestTimeout = 45 * time.Second
	}
	if options.QueuePollInterval <= 0 {
		options.QueuePollInterval = time.Second
	}
	if options.MaxPreparationAttempts <= 0 {
		options.MaxPreparationAttempts = 3
	}
	return &PreparationWorker{materialService: materialService, options: options}
}

// RunUntilStopped обрабатывает очередь до отмены контекста приложения.
// При свободной работе следующий запрос берётся сразу; пустая очередь и ошибки
// БД вызывают паузу, чтобы не нагружать базу непрерывным опросом.
func (worker *PreparationWorker) RunUntilStopped(ctx context.Context) {
	pollTicker := time.NewTicker(worker.options.QueuePollInterval)
	defer pollTicker.Stop()
	for ctx.Err() == nil {
		requestWasClaimed, err := worker.ProcessNextPreparationRequest(ctx)
		if err != nil && ctx.Err() == nil {
			// Исходная ошибка драйвера может содержать текст материала или пароль.
			log.Print("material worker: queue operation failed")
		}
		if requestWasClaimed && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-pollTicker.C:
		}
	}
}

// ProcessNextPreparationRequest проводит одну работу через три явных этапа:
// захват запроса, определение CEFR и сохранение итога. false без ошибки означает,
// что работы нет либо клиент LLM отключён. Вызов модели проходит без блокировки БД.
func (worker *PreparationWorker) ProcessNextPreparationRequest(ctx context.Context) (bool, error) {
	service := worker.materialService
	if service == nil || service.database == nil || service.levelDetector == nil || !service.levelDetector.HasAPIKeys() {
		return false, nil
	}
	preparationRequest, err := worker.claimNextPreparationRequest(ctx)
	if err != nil || preparationRequest == nil {
		return false, err
	}
	if preparationRequest.AttemptCount > worker.options.MaxPreparationAttempts {
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
	if request.PromptVersion != generation.CEFRDetectionPromptVersion {
		return "", &generation.LLMProviderError{Code: "LLM_PROMPT_UNSUPPORTED"}
	}
	if material.SourceText == nil || material.Kind != models.MaterialText {
		return "", &generation.LLMProviderError{Code: "MATERIAL_UNSUPPORTED"}
	}
	llmCallContext, cancelLLMCall := context.WithTimeout(ctx, worker.options.LLMRequestTimeout)
	defer cancelLLMCall()
	detectedLevel, err := worker.materialService.levelDetector.DetectCEFRLevel(llmCallContext, *material.SourceText)
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
