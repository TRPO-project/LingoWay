package materials

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"lingoway/internal/generation"
	"lingoway/internal/models"
	"lingoway/internal/models/llm"
	"lingoway/internal/models/topics"

	"gorm.io/gorm"
)

var (
	// ErrInvalidMaterialInput означает, что поля материала или UUID ключа неверны.
	ErrInvalidMaterialInput = errors.New("invalid material input")
	// ErrMaterialIdempotencyConflict возвращается при повторе ключа с другим содержимым.
	ErrMaterialIdempotencyConflict = errors.New("idempotency key already used for different input")
	// ErrResourceNotFound означает отсутствие материала, темы или запроса с указанным ID.
	ErrResourceNotFound = errors.New("resource not found")
	// ErrMaterialPreparationUnavailable означает отсутствие БД или ключей для новой работы.
	ErrMaterialPreparationUnavailable = errors.New("material preparation is not configured")
	// ErrPreparationLeaseLost запрещает сохранять результат после истечения или смены аренды.
	ErrPreparationLeaseLost = errors.New("request lease is no longer current")
)

var idempotencyKeyPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// CEFRLevelDetector определяет уровень английского текста, не изменяя сам текст.
// Интерфейс отделяет подготовку материала от протокола конкретного провайдера.
type CEFRLevelDetector interface {
	HasAPIKeys() bool
	DetectCEFRLevel(context.Context, string) (models.CEFRLevel, error)
	ProviderName() string
	ModelName() string
}

// CreateTextMaterialInput содержит поля POST /api/materials. Пользователь не передаётся:
// модуль работает в общем режиме. Поле SourceText в HTTP-контракте называется text.
type CreateTextMaterialInput struct {
	Title      string              `json:"title"`
	Kind       models.MaterialKind `json:"kind"`
	SourceText string              `json:"text"`
	TopicID    *int64              `json:"topic_id,omitempty"`
}

// MaterialService сохраняет общие материалы и защищает от повторного создания.
// Чтение выполняется по ID без проверки пользователя. Уровень определяет PreparationWorker.
type MaterialService struct {
	database      *gorm.DB
	levelDetector CEFRLevelDetector
}

// NewMaterialService связывает БД и определитель уровня; фоновая работа здесь не запускается.
func NewMaterialService(database *gorm.DB, levelDetector CEFRLevelDetector) *MaterialService {
	return &MaterialService{database: database, levelDetector: levelDetector}
}

// CreateTextMaterialAndQueuePreparation сохраняет материал и запрос prepare_material
// одной транзакцией. Повтор того же ключа с тем же вводом возвращает прежние записи,
// а другой ввод с прежним ключом — ErrMaterialIdempotencyConflict.
func (service *MaterialService) CreateTextMaterialAndQueuePreparation(ctx context.Context, idempotencyKey string, input CreateTextMaterialInput) (*llm.Material, *llm.LLMRequest, error) {
	if !idempotencyKeyPattern.MatchString(idempotencyKey) {
		return nil, nil, ErrInvalidMaterialInput
	}
	normalizedInput, err := normalizeAndValidateTextMaterialInput(input)
	if err != nil {
		return nil, nil, err
	}
	if service.database == nil {
		return nil, nil, ErrMaterialPreparationUnavailable
	}
	idempotencyKey = strings.ToLower(idempotencyKey)
	var material llm.Material
	var preparationRequest llm.LLMRequest
	err = service.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		// Схема требует владельца для внешних ключей. Сервер назначает общую
		// служебную запись сам; это не проверка личности отправителя запроса.
		ownerUserID, err := findOrCreateSharedMaterialOwner(transaction)
		if err != nil {
			return err
		}
		if err := lockMaterialCreationByIdempotencyKey(transaction, ownerUserID, idempotencyKey); err != nil {
			return err
		}
		lookupErr := transaction.Where("owner_user_id = ? AND request_key = ?", ownerUserID, idempotencyKey).First(&material).Error
		if lookupErr == nil {
			if !materialMatchesCreationInput(material, normalizedInput) {
				return ErrMaterialIdempotencyConflict
			}
			return transaction.Where("owner_user_id = ? AND operation = ? AND request_key = ?",
				ownerUserID, models.LLMPrepareMaterial, idempotencyKey).First(&preparationRequest).Error
		}
		if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return lookupErr
		}
		// Ранее созданную пару можно прочитать повторно даже без ключей LLM.
		// Наличие настроенного клиента требуется только для новой работы.
		if service.levelDetector == nil || !service.levelDetector.HasAPIKeys() {
			return ErrMaterialPreparationUnavailable
		}
		if err := validateMaterialTopicExists(transaction, normalizedInput.TopicID); err != nil {
			return err
		}
		material = llm.Material{
			OwnerUserID: ownerUserID, RequestKey: idempotencyKey, TopicID: normalizedInput.TopicID,
			Title: normalizedInput.Title, Kind: normalizedInput.Kind,
			Status: models.MaterialPending, SourceText: &normalizedInput.SourceText,
		}
		if err := transaction.Create(&material).Error; err != nil {
			return err
		}
		preparationRequest = llm.LLMRequest{
			OwnerUserID: ownerUserID, RequestKey: idempotencyKey, Operation: models.LLMPrepareMaterial,
			MaterialID: &material.ID, Status: models.LLMQueued, Parameters: models.JSON(`{}`),
			PromptVersion: generation.CEFRDetectionPromptVersion,
		}
		return transaction.Create(&preparationRequest).Error
	})
	if err != nil {
		return nil, nil, err
	}
	return &material, &preparationRequest, nil
}

// GetMaterialByID возвращает материал по ID. В текущем общем режиме владельцы
// не ограничивают чтение, в том числе для записей, созданных до отключения авторизации.
func (service *MaterialService) GetMaterialByID(ctx context.Context, materialID int64) (*llm.Material, error) {
	if materialID <= 0 {
		return nil, ErrResourceNotFound
	}
	if service.database == nil {
		return nil, ErrMaterialPreparationUnavailable
	}
	var material llm.Material
	if err := service.database.WithContext(ctx).First(&material, materialID).Error; err != nil {
		return nil, mapDatabaseNotFoundError(err)
	}
	return &material, nil
}

// GetLLMRequestByID читает состояние запроса без проверки пользователя.
// HTTP-слой по-прежнему исключает внутренние параметры, аренду и ответ провайдера.
func (service *MaterialService) GetLLMRequestByID(ctx context.Context, requestID int64) (*llm.LLMRequest, error) {
	if requestID <= 0 {
		return nil, ErrResourceNotFound
	}
	if service.database == nil {
		return nil, ErrMaterialPreparationUnavailable
	}
	var request llm.LLMRequest
	if err := service.database.WithContext(ctx).First(&request, requestID).Error; err != nil {
		return nil, mapDatabaseNotFoundError(err)
	}
	return &request, nil
}

func normalizeAndValidateTextMaterialInput(input CreateTextMaterialInput) (CreateTextMaterialInput, error) {
	input.Title = strings.TrimSpace(input.Title)
	titleCharacters := utf8.RuneCountInString(input.Title)
	if !utf8.ValidString(input.Title) || strings.ContainsRune(input.Title, 0) ||
		titleCharacters < 1 || titleCharacters > 200 || input.Kind != models.MaterialText ||
		(input.TopicID != nil && *input.TopicID <= 0) {
		return CreateTextMaterialInput{}, ErrInvalidMaterialInput
	}
	normalizedText, err := NormalizeAndValidateText(input.SourceText)
	if err != nil {
		return CreateTextMaterialInput{}, fmt.Errorf("%w: %w", ErrInvalidMaterialInput, err)
	}
	input.SourceText = normalizedText
	return input, nil
}

// Блокировка нужна ещё до появления записей: второй одновременный запрос ждёт
// завершения первого и затем получает ту же пару вместо создания дубля.
func lockMaterialCreationByIdempotencyKey(transaction *gorm.DB, ownerUserID int64, idempotencyKey string) error {
	lockKey := fmt.Sprintf("material:%d:%s", ownerUserID, idempotencyKey)
	return transaction.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Error
}

func materialMatchesCreationInput(material llm.Material, input CreateTextMaterialInput) bool {
	return material.Kind == input.Kind && material.Title == input.Title &&
		material.SourceText != nil && *material.SourceText == input.SourceText &&
		sameOptionalTopicID(material.TopicID, input.TopicID)
}

func validateMaterialTopicExists(transaction *gorm.DB, topicID *int64) error {
	if topicID != nil {
		if err := transaction.Select("id").First(&topics.Topic{}, *topicID).Error; err != nil {
			return mapDatabaseNotFoundError(err)
		}
	}
	return nil
}

func sameOptionalTopicID(first, second *int64) bool {
	return first == nil && second == nil || first != nil && second != nil && *first == *second
}

func mapDatabaseNotFoundError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrResourceNotFound
	}
	return err
}
