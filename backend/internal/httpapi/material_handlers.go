package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"lingoway/internal/materials"
	"lingoway/internal/models"
	"lingoway/internal/models/llm"

	"github.com/gin-gonic/gin"
)

// MaterialPreparationService описывает только нужные HTTP-слою операции подготовки материала.
// В текущем общем режиме методы не получают пользователя и не проверяют владельца.
type MaterialPreparationService interface {
	// CreateTextMaterialAndQueuePreparation атомарно сохраняет источник и запрос очереди либо возвращает
	// ранее созданную пару для того же ключа идемпотентности и того же ввода.
	CreateTextMaterialAndQueuePreparation(ctx context.Context, idempotencyKey string, input materials.CreateTextMaterialInput) (*llm.Material, *llm.LLMRequest, error)
	// GetMaterialByID читает материал по ID без ограничения по владельцу.
	GetMaterialByID(ctx context.Context, materialID int64) (*llm.Material, error)
	// GetLLMRequestByID возвращает состояние фонового запроса по ID.
	GetLLMRequestByID(ctx context.Context, requestID int64) (*llm.LLMRequest, error)
}

type materialHTTPHandler struct{ materialPreparationService MaterialPreparationService }

// Символ за пределами основной плоскости Unicode может занимать в JSON 12 байт
// как пара escape-последовательностей. Добавочные 4096 байт оставлены для названия
// и служебных полей. Это лимит HTTP-тела, отдельный от лимита символов в самом тексте.
const maxCreateMaterialJSONBytes int64 = materials.MaxMaterialTextCharacters*12 + 4096

var idempotencyKeyPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// createTextMaterial принимает только текстовый источник. Ответ 202 означает сохранение
// материала и задания очереди; обращение к LLM выполняет фоновый воркер.
func (handler materialHTTPHandler) createTextMaterial(requestContext *gin.Context) {
	if handler.materialPreparationService == nil {
		writeMaterialServiceError(requestContext, materials.ErrMaterialPreparationUnavailable)
		return
	}
	idempotencyKey := requestContext.GetHeader("Idempotency-Key")
	if !idempotencyKeyPattern.MatchString(idempotencyKey) {
		writeAPIError(requestContext, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "Укажите UUID в заголовке Idempotency-Key.")
		return
	}
	mediaType, _, err := mime.ParseMediaType(requestContext.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeAPIError(requestContext, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Ожидается application/json.")
		return
	}
	requestContext.Request.Body = http.MaxBytesReader(requestContext.Writer, requestContext.Request.Body, maxCreateMaterialJSONBytes)
	requestBodyBytes, err := io.ReadAll(requestContext.Request.Body)
	if err != nil {
		writeInvalidJSONError(requestContext, err)
		return
	}
	if !utf8.Valid(requestBodyBytes) {
		writeAPIError(requestContext, http.StatusBadRequest, "INVALID_JSON", "JSON должен быть в кодировке UTF-8.")
		return
	}
	requestJSONDecoder := json.NewDecoder(bytes.NewReader(requestBodyBytes))
	// Неизвестные поля и второй JSON после объекта считаем ошибкой, чтобы опечатки
	// и попытки передать служебные поля не превращались в молча проигнорированный ввод.
	requestJSONDecoder.DisallowUnknownFields()
	var materialInput materials.CreateTextMaterialInput
	if err := requestJSONDecoder.Decode(&materialInput); err != nil {
		writeInvalidJSONError(requestContext, err)
		return
	}
	var trailingJSONValue any
	if err := requestJSONDecoder.Decode(&trailingJSONValue); err != io.EOF {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		writeInvalidJSONError(requestContext, err)
		return
	}
	materialInput, inputIsValid := normalizeAndValidateMaterialHTTPInput(requestContext, materialInput)
	if !inputIsValid {
		return
	}
	// Ключ приводится к одной форме: регистр шестнадцатеричных символов не должен
	// создавать разные запросы. Технического владельца для БД назначает сервис;
	// HTTP-запрос не принимает owner_user_id или другие служебные поля.
	material, llmRequest, err := handler.materialPreparationService.CreateTextMaterialAndQueuePreparation(requestContext.Request.Context(), strings.ToLower(idempotencyKey), materialInput)
	if err != nil {
		writeMaterialServiceError(requestContext, err)
		return
	}
	requestContext.JSON(http.StatusAccepted, gin.H{"material": newMaterialResponse(material), "llm_request": newLLMRequestResponse(llmRequest)})
}

// normalizeAndValidateMaterialHTTPInput сохраняет конкретные HTTP-коды проверки:
// неподдерживаемый вид источника, слишком большой текст и неверное название различаются.
// Сервис повторно проверяет ввод, поскольку его могут вызывать и без HTTP-обработчика.
func normalizeAndValidateMaterialHTTPInput(requestContext *gin.Context, materialInput materials.CreateTextMaterialInput) (materials.CreateTextMaterialInput, bool) {
	if materialInput.Kind != models.MaterialText {
		writeAPIError(requestContext, http.StatusUnprocessableEntity, "UNSUPPORTED_MATERIAL_KIND", "Пока поддерживается материал kind=text.")
		return materialInput, false
	}
	normalizedMaterialText, err := materials.NormalizeAndValidateText(materialInput.SourceText)
	if err != nil {
		writeTextImportError(requestContext, err)
		return materialInput, false
	}
	materialInput.SourceText = normalizedMaterialText
	materialInput.Title = strings.TrimSpace(materialInput.Title)
	if materialInput.Title == "" || !utf8.ValidString(materialInput.Title) || utf8.RuneCountInString(materialInput.Title) > 200 || (materialInput.TopicID != nil && *materialInput.TopicID <= 0) {
		writeAPIError(requestContext, http.StatusUnprocessableEntity, "INVALID_INPUT", "Проверьте название и идентификатор темы.")
		return materialInput, false
	}
	return materialInput, true
}

// getMaterialByID выдаёт материал в общем режиме: проверка пользователя отключена.
func (handler materialHTTPHandler) getMaterialByID(requestContext *gin.Context) {
	if handler.materialPreparationService == nil {
		writeMaterialServiceError(requestContext, materials.ErrMaterialPreparationUnavailable)
		return
	}
	materialID, materialIDIsValid := parsePositiveResourceID(requestContext)
	if !materialIDIsValid {
		return
	}
	material, err := handler.materialPreparationService.GetMaterialByID(requestContext.Request.Context(), materialID)
	if err != nil {
		writeMaterialServiceError(requestContext, err)
		return
	}
	requestContext.JSON(http.StatusOK, newMaterialResponse(material))
}

// getLLMRequestByID выдаёт состояние обработки по ID без проверки пользователя.
func (handler materialHTTPHandler) getLLMRequestByID(requestContext *gin.Context) {
	if handler.materialPreparationService == nil {
		writeMaterialServiceError(requestContext, materials.ErrMaterialPreparationUnavailable)
		return
	}
	llmRequestID, requestIDIsValid := parsePositiveResourceID(requestContext)
	if !requestIDIsValid {
		return
	}
	llmRequest, err := handler.materialPreparationService.GetLLMRequestByID(requestContext.Request.Context(), llmRequestID)
	if err != nil {
		writeMaterialServiceError(requestContext, err)
		return
	}
	requestContext.JSON(http.StatusOK, newLLMRequestResponse(llmRequest))
}

// previewUploadedMaterialText — предварительный импорт для просмотра и проверки текста пользователем.
// Он не сохраняет бинарный файл, не создаёт Material и не вызывает LLM. Полученный
// текст нужно отдельно передать в createTextMaterial; в БД это будет источник kind=text.
func (handler materialHTTPHandler) previewUploadedMaterialText(requestContext *gin.Context) {
	mediaType, _, err := mime.ParseMediaType(requestContext.GetHeader("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		writeAPIError(requestContext, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Ожидается multipart/form-data с полем file.")
		return
	}
	// Ограничиваем всё multipart-тело, включая заголовки частей и разделители.
	// Сам файл ниже проверяется отдельно: ему разрешено не более MaxTextImportFileBytes.
	requestContext.Request.Body = http.MaxBytesReader(requestContext.Writer, requestContext.Request.Body, materials.MaxTextImportFileBytes+(64<<10))
	multipartReader, err := requestContext.Request.MultipartReader()
	if err != nil {
		writeAPIError(requestContext, http.StatusBadRequest, "INVALID_MULTIPART", "Не удалось прочитать загруженный файл.")
		return
	}
	uploadedFilePart, err := multipartReader.NextPart()
	if err != nil {
		writeInvalidMultipartError(requestContext, err)
		return
	}
	defer uploadedFilePart.Close()
	if uploadedFilePart.FormName() != "file" || uploadedFilePart.FileName() == "" {
		writeAPIError(requestContext, http.StatusBadRequest, "INVALID_MULTIPART", "Загрузите ровно один файл в поле file.")
		return
	}
	// Читаем в память с жёстким пределом и одним проверочным байтом. Потоковый
	// MultipartReader не создаёт временные файлы, в отличие от ParseMultipartForm.
	uploadedFileBytes, err := io.ReadAll(io.LimitReader(uploadedFilePart, materials.MaxTextImportFileBytes+1))
	if err != nil {
		writeInvalidMultipartError(requestContext, err)
		return
	}
	if int64(len(uploadedFileBytes)) > materials.MaxTextImportFileBytes {
		writeTextImportError(requestContext, materials.ErrMaterialSizeLimitExceeded)
		return
	}
	// В этом контракте разрешён ровно один файл без дополнительных полей.
	// Проверяем окончание multipart, чтобы случайно не принять только первый источник.
	if _, err := multipartReader.NextPart(); err != io.EOF {
		if err != nil {
			writeInvalidMultipartError(requestContext, err)
		} else {
			writeAPIError(requestContext, http.StatusBadRequest, "INVALID_MULTIPART", "Загрузите ровно один файл в поле file.")
		}
		return
	}
	normalizedMaterialText, err := materials.ExtractTextFromFile(uploadedFilePart.FileName(), bytes.NewReader(uploadedFileBytes))
	if err != nil {
		writeTextImportError(requestContext, err)
		return
	}
	requestContext.JSON(http.StatusOK, gin.H{"text": normalizedMaterialText, "characters": utf8.RuneCountInString(normalizedMaterialText)})
}

func writeInvalidMultipartError(requestContext *gin.Context, err error) {
	var bodySizeLimitError *http.MaxBytesError
	if errors.As(err, &bodySizeLimitError) {
		writeTextImportError(requestContext, materials.ErrMaterialSizeLimitExceeded)
		return
	}
	writeAPIError(requestContext, http.StatusBadRequest, "INVALID_MULTIPART", "Не удалось прочитать загруженный файл.")
}

func parsePositiveResourceID(requestContext *gin.Context) (int64, bool) {
	resourceID, err := strconv.ParseInt(requestContext.Param("id"), 10, 64)
	if err != nil || resourceID <= 0 {
		writeAPIError(requestContext, http.StatusBadRequest, "INVALID_ID", "Идентификатор должен быть положительным целым числом.")
		return 0, false
	}
	return resourceID, true
}
