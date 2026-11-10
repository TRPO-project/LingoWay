package httpapi

import (
	"errors"
	"net/http"
	"time"

	"lingoway/internal/materials"
	"lingoway/internal/models"
	"lingoway/internal/models/llm"

	"github.com/gin-gonic/gin"
)

// materialResponse — явный публичный DTO, то есть разрешённый набор полей ответа.
// Всю GORM-модель сериализовать нельзя: в ней есть владелец, исходный текст,
// адрес источника и служебные метаданные, которых нет в HTTP-контракте.
// На текущем этапе подготовленный ExtractedText доступен по ID без проверки пользователя.
type materialResponse struct {
	ID            int64                 `json:"id"`
	Title         string                `json:"title"`
	TopicID       *int64                `json:"topic_id"`
	Kind          models.MaterialKind   `json:"kind"`
	Status        models.MaterialStatus `json:"status"`
	Level         *models.CEFRLevel     `json:"level"`
	ExtractedText *string               `json:"extracted_text"`
	CreatedAt     time.Time             `json:"created_at"`
}

func newMaterialResponse(material *llm.Material) materialResponse {
	return materialResponse{
		ID:            material.ID,
		Title:         material.Title,
		TopicID:       material.TopicID,
		Kind:          material.Kind,
		Status:        material.Status,
		Level:         material.Level,
		ExtractedText: material.ExtractedText,
		CreatedAt:     material.CreatedAt,
	}
}

// llmRequestFailureResponse описывает результат неудачной фоновой обработки. Retryable
// отличает временный сбой провайдера от ошибки, которую простой повтор не исправит.
type llmRequestFailureResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// llmRequestResponse раскрывает состояние очереди и ID связанных ресурсов.
// Промпт, параметры, модель, результат провайдера и токен аренды остаются на сервере.
type llmRequestResponse struct {
	ID         int64                      `json:"id"`
	Operation  models.LLMOperation        `json:"operation"`
	Status     models.LLMStatus           `json:"status"`
	MaterialID *int64                     `json:"material_id"`
	ModuleID   *int64                     `json:"module_id"`
	AnswerID   *int64                     `json:"answer_id"`
	Error      *llmRequestFailureResponse `json:"error"`
	CreatedAt  time.Time                  `json:"created_at"`
	FinishedAt *time.Time                 `json:"finished_at"`
}

func newLLMRequestResponse(llmRequest *llm.LLMRequest) llmRequestResponse {
	response := llmRequestResponse{
		ID:         llmRequest.ID,
		Operation:  llmRequest.Operation,
		Status:     llmRequest.Status,
		MaterialID: llmRequest.MaterialID,
		ModuleID:   llmRequest.ModuleID,
		AnswerID:   llmRequest.AnswerID,
		CreatedAt:  llmRequest.CreatedAt,
		FinishedAt: llmRequest.FinishedAt,
	}
	// Сообщение из ErrorMessage не используем: оно может содержать внутренние
	// сведения. Только известные коды получают заранее заданное безопасное описание.
	if llmRequest.Status == models.LLMFailed {
		safeFailure := llmRequestFailureResponse{Code: "PROCESSING_FAILED", Message: "Не удалось подготовить материал."}
		if llmRequest.ErrorCode != nil {
			switch *llmRequest.ErrorCode {
			case "LLM_TIMEOUT", "LLM_RATE_LIMITED", "LLM_UNAVAILABLE":
				safeFailure = llmRequestFailureResponse{Code: *llmRequest.ErrorCode, Message: "Сервис модели временно недоступен.", Retryable: true}
			case "LLM_NOT_CONFIGURED":
				safeFailure = llmRequestFailureResponse{Code: *llmRequest.ErrorCode, Message: "Подключение к модели не настроено."}
			case "LLM_INVALID_RESPONSE":
				safeFailure = llmRequestFailureResponse{Code: *llmRequest.ErrorCode, Message: "Модель вернула некорректный ответ."}
			case "LLM_PROVIDER_ERROR":
				safeFailure = llmRequestFailureResponse{Code: *llmRequest.ErrorCode, Message: "Сервис модели отклонил запрос."}
			case "LLM_INVALID_INPUT":
				safeFailure = llmRequestFailureResponse{Code: *llmRequest.ErrorCode, Message: "Материал не подходит для обработки."}
			case "LLM_CANCELLED":
				safeFailure = llmRequestFailureResponse{Code: *llmRequest.ErrorCode, Message: "Обработка материала отменена."}
			}
		}
		response.Error = &safeFailure
	}
	return response
}

func writeAPIError(requestContext *gin.Context, status int, code, message string) {
	requestContext.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}

func writeInvalidJSONError(requestContext *gin.Context, err error) {
	var bodySizeLimitError *http.MaxBytesError
	if errors.As(err, &bodySizeLimitError) {
		writeTextImportError(requestContext, materials.ErrMaterialSizeLimitExceeded)
		return
	}
	writeAPIError(requestContext, http.StatusBadRequest, "INVALID_JSON", "Проверьте JSON и набор полей запроса.")
}

func writeTextImportError(requestContext *gin.Context, err error) {
	switch {
	case errors.Is(err, materials.ErrMaterialSizeLimitExceeded):
		writeAPIError(requestContext, http.StatusRequestEntityTooLarge, "MATERIAL_TOO_LARGE", "Материал превышает допустимый размер.")
	case errors.Is(err, materials.ErrUnsupportedTextFileFormat):
		writeAPIError(requestContext, http.StatusUnsupportedMediaType, "UNSUPPORTED_FILE_FORMAT", "Поддерживаются файлы TXT, MD и DOCX.")
	case errors.Is(err, materials.ErrInvalidTextFile):
		writeAPIError(requestContext, http.StatusUnprocessableEntity, "INVALID_FILE", "Файл повреждён или имеет неверный формат.")
	default:
		writeAPIError(requestContext, http.StatusUnprocessableEntity, "INVALID_TEXT", "Укажите непустой текст в UTF-8 без управляющих символов.")
	}
}

// writeMaterialServiceError переводит бизнес-ошибки в стабильный HTTP-контракт. Неизвестная
// ошибка БД или другого внутреннего слоя не должна попасть к клиенту через err.Error().
func writeMaterialServiceError(requestContext *gin.Context, err error) {
	switch {
	case errors.Is(err, materials.ErrInvalidMaterialInput):
		writeAPIError(requestContext, http.StatusUnprocessableEntity, "INVALID_INPUT", "Проверьте данные материала и тему.")
	case errors.Is(err, materials.ErrMaterialIdempotencyConflict):
		writeAPIError(requestContext, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Этот Idempotency-Key уже использован с другими данными.")
	case errors.Is(err, materials.ErrResourceNotFound):
		writeAPIError(requestContext, http.StatusNotFound, "NOT_FOUND", "Ресурс не найден.")
	case errors.Is(err, materials.ErrMaterialPreparationUnavailable):
		writeAPIError(requestContext, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Подготовка материалов временно недоступна.")
	default:
		writeAPIError(requestContext, http.StatusInternalServerError, "INTERNAL_ERROR", "Не удалось выполнить запрос.")
	}
}
