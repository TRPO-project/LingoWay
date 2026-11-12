package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"lingoway/internal/materials"
	"lingoway/internal/models/llm"
	"lingoway/internal/models/topics"

	"github.com/gin-gonic/gin"
)

// ModuleGenerationService ставит создание модуля в очередь и выдаёт упражнения
// опубликованного модуля из материала. Проверка доступа к каталогу остаётся в сервисе.
type ModuleGenerationService interface {
	CreateGeneratedModule(ctx context.Context, materialID int64, idempotencyKey string, input materials.CreateGeneratedModuleInput) (*topics.Module, *llm.LLMRequest, error)
	ListGeneratedModuleExercises(ctx context.Context, moduleID int64, limit, offset int) (*topics.Module, []topics.Exercise, error)
}

type moduleGenerationHTTPHandler struct {
	service ModuleGenerationService
}

// В запросе передаются только название, тема, уровень, число и типы упражнений.
// Лимит учитывает JSON-представление названия и поддерживаемых типов упражнений.
const maxCreateGeneratedModuleJSONBytes int64 = 8 << 10

func (handler moduleGenerationHTTPHandler) createGeneratedModule(requestContext *gin.Context) {
	if handler.service == nil {
		writeModuleGenerationServiceError(requestContext, materials.ErrMaterialPreparationUnavailable)
		return
	}
	materialID, valid := parsePositiveResourceID(requestContext)
	if !valid {
		return
	}
	idempotencyKey := requestContext.GetHeader("Idempotency-Key")
	if !idempotencyKeyPattern.MatchString(idempotencyKey) {
		writeAPIError(requestContext, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "Укажите UUID в заголовке Idempotency-Key.")
		return
	}
	var input materials.CreateGeneratedModuleInput
	if err := decodeJSONRequest(requestContext, maxCreateGeneratedModuleJSONBytes, &input); err != nil {
		var bodySizeLimitError *http.MaxBytesError
		if errors.As(err, &bodySizeLimitError) {
			writeAPIError(requestContext, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "Тело запроса превышает допустимый размер.")
		} else {
			writeInvalidJSONError(requestContext, err)
		}
		return
	}
	module, llmRequest, err := handler.service.CreateGeneratedModule(requestContext.Request.Context(), materialID, idempotencyKey, input)
	if err != nil {
		writeModuleGenerationServiceError(requestContext, err)
		return
	}
	requestContext.JSON(http.StatusAccepted, gin.H{"module": newGeneratedModuleResponse(module), "llm_request": newLLMRequestResponse(llmRequest)})
}

func (handler moduleGenerationHTTPHandler) listGeneratedModuleExercises(requestContext *gin.Context) {
	if handler.service == nil {
		writeModuleGenerationServiceError(requestContext, materials.ErrMaterialPreparationUnavailable)
		return
	}
	moduleID, valid := parsePositiveResourceID(requestContext)
	if !valid {
		return
	}
	limit, offset, valid := parseExercisePage(requestContext)
	if !valid {
		return
	}
	// Дополнительная строка показывает, есть ли следующая страница.
	module, exercises, err := handler.service.ListGeneratedModuleExercises(requestContext.Request.Context(), moduleID, limit+1, offset)
	if err != nil {
		writeModuleGenerationServiceError(requestContext, err)
		return
	}
	hasMore := len(exercises) > limit
	if hasMore {
		exercises = exercises[:limit]
	}
	items := make([]generatedExerciseResponse, 0, len(exercises))
	for _, exercise := range exercises {
		items = append(items, newGeneratedExerciseResponse(module, exercise))
	}
	requestContext.JSON(http.StatusOK, gin.H{"items": items, "limit": limit, "offset": offset, "has_more": hasMore})
}

func parseExercisePage(requestContext *gin.Context) (limit, offset int, valid bool) {
	limit = 20
	if rawLimit, exists := requestContext.GetQuery("limit"); exists {
		parsedLimit, err := strconv.Atoi(rawLimit)
		if err != nil || parsedLimit < 1 || parsedLimit > 100 {
			writeAPIError(requestContext, http.StatusBadRequest, "INVALID_PAGINATION", "Параметр limit должен быть целым числом от 1 до 100.")
			return 0, 0, false
		}
		limit = parsedLimit
	}
	if rawOffset, exists := requestContext.GetQuery("offset"); exists {
		parsedOffset, err := strconv.Atoi(rawOffset)
		if err != nil || parsedOffset < 0 {
			writeAPIError(requestContext, http.StatusBadRequest, "INVALID_PAGINATION", "Параметр offset должен быть неотрицательным целым числом.")
			return 0, 0, false
		}
		offset = parsedOffset
	}
	return limit, offset, true
}

func writeModuleGenerationServiceError(requestContext *gin.Context, err error) {
	switch {
	case errors.Is(err, materials.ErrMaterialNotReady):
		writeAPIError(requestContext, http.StatusConflict, "MATERIAL_NOT_READY", "Материал ещё не готов для создания модуля.")
	case errors.Is(err, materials.ErrGeneratedModuleNotReady):
		writeAPIError(requestContext, http.StatusConflict, "MODULE_NOT_READY", "Упражнения модуля ещё недоступны.")
	case errors.Is(err, materials.ErrInvalidMaterialInput):
		writeAPIError(requestContext, http.StatusUnprocessableEntity, "INVALID_INPUT", "Проверьте тему, название, уровень и параметры упражнений.")
	case errors.Is(err, materials.ErrMaterialPreparationUnavailable):
		writeAPIError(requestContext, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Создание или чтение модуля временно недоступно.")
	default:
		writeMaterialServiceError(requestContext, err)
	}
}
