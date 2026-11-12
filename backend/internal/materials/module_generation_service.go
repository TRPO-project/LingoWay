package materials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"lingoway/internal/generation"
	"lingoway/internal/models"
	"lingoway/internal/models/llm"
	"lingoway/internal/models/topics"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	// ErrMaterialNotReady означает, что источник ещё нельзя передать на генерацию.
	ErrMaterialNotReady = errors.New("material is not ready")
	// ErrGeneratedModuleNotReady означает, что опубликованные задания пока недоступны.
	ErrGeneratedModuleNotReady = errors.New("generated module is not ready")
)

// ExerciseGenerator создаёт и проверяет упражнения по подготовленному тексту.
type ExerciseGenerator interface {
	HasAPIKeys() bool
	ProviderName() string
	ModelName() string
	GenerateExercises(context.Context, generation.ExerciseGenerationInput, string) ([]generation.GeneratedExercise, error)
}

// CreateGeneratedModuleInput задаёт тему, уровень и состав нового модуля.
type CreateGeneratedModuleInput struct {
	Title      string                `json:"title"`
	TopicID    int64                 `json:"topic_id"`
	Difficulty models.CEFRLevel      `json:"difficulty"`
	Count      int                   `json:"count"`
	Types      []models.ExerciseType `json:"types"`
}

type moduleGenerationParameters struct {
	Count int                   `json:"count"`
	Types []models.ExerciseType `json:"types"`
}

// ModuleGenerationService принимает запросы генерации и выдаёт готовые упражнения.
type ModuleGenerationService struct {
	database  *gorm.DB
	generator ExerciseGenerator
}

func NewModuleGenerationService(database *gorm.DB, exerciseGenerator ExerciseGenerator) *ModuleGenerationService {
	return &ModuleGenerationService{database: database, generator: exerciseGenerator}
}

// CreateGeneratedModule сохраняет модуль и работу очереди в одной транзакции.
// Повтор прежнего ключа с теми же данными возвращает уже созданную пару.
func (service *ModuleGenerationService) CreateGeneratedModule(ctx context.Context, materialID int64, idempotencyKey string, input CreateGeneratedModuleInput) (*topics.Module, *llm.LLMRequest, error) {
	if materialID <= 0 {
		return nil, nil, ErrResourceNotFound
	}
	input.Title = strings.TrimSpace(input.Title)
	if !idempotencyKeyPattern.MatchString(idempotencyKey) || input.Title == "" ||
		!utf8.ValidString(input.Title) || strings.ContainsRune(input.Title, 0) ||
		utf8.RuneCountInString(input.Title) > 200 || input.TopicID <= 0 || !isSupportedCEFRLevel(input.Difficulty) {
		return nil, nil, ErrInvalidMaterialInput
	}
	if _, err := generation.PlanExerciseCounts(input.Count, input.Types); err != nil {
		return nil, nil, ErrInvalidMaterialInput
	}
	if service.database == nil {
		return nil, nil, ErrMaterialPreparationUnavailable
	}
	idempotencyKey = strings.ToLower(idempotencyKey)
	parameters := moduleGenerationParameters{Count: input.Count, Types: input.Types}
	parametersJSON, err := json.Marshal(parameters)
	if err != nil {
		return nil, nil, err
	}

	var module topics.Module
	var generationRequest llm.LLMRequest
	err = service.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		var material llm.Material
		// Готовность источника и владелец должны сохраняться до записи связанных строк.
		if err := transaction.Clauses(clause.Locking{Strength: "SHARE"}).
			Select("id", "owner_user_id", "status").First(&material, materialID).Error; err != nil {
			return mapDatabaseNotFoundError(err)
		}
		lockKey := fmt.Sprintf("generate-module:%d:%s", material.OwnerUserID, idempotencyKey)
		if err := transaction.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Error; err != nil {
			return err
		}
		// Воркер завершает модуль и работу очереди вместе. Блокировка запроса
		// удерживает согласованное состояние пары до чтения модуля.
		lookupErr := transaction.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("owner_user_id = ? AND operation = ? AND request_key = ?",
				material.OwnerUserID, models.LLMGenerateModule, idempotencyKey).First(&generationRequest).Error
		if lookupErr == nil {
			return loadGeneratedModuleReplay(transaction, materialID, input, &generationRequest, &module)
		}
		if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return lookupErr
		}
		if material.Status != models.MaterialReady {
			return ErrMaterialNotReady
		}
		if service.generator == nil || !service.generator.HasAPIKeys() {
			return ErrMaterialPreparationUnavailable
		}
		if err := validateMaterialTopicExists(transaction, &input.TopicID); err != nil {
			return err
		}
		// В общем режиме владелец нужен для составных внешних ключей БД.
		module = topics.Module{
			CreatedByUserID: &material.OwnerUserID, MaterialID: &material.ID,
			TopicID: input.TopicID, Title: input.Title, Difficulty: input.Difficulty,
			Visibility: models.VisibilityPrivate, Status: models.ModuleGenerating,
		}
		if err := transaction.Create(&module).Error; err != nil {
			return err
		}
		generationRequest = llm.LLMRequest{
			OwnerUserID: material.OwnerUserID, RequestKey: idempotencyKey, Operation: models.LLMGenerateModule,
			ModuleID: &module.ID, Status: models.LLMQueued, Parameters: models.JSON(parametersJSON),
			PromptVersion: generation.ExerciseGenerationPromptVersion,
		}
		return transaction.Create(&generationRequest).Error
	})
	if err != nil {
		return nil, nil, err
	}
	return &module, &generationRequest, nil
}

func loadGeneratedModuleReplay(transaction *gorm.DB, materialID int64, input CreateGeneratedModuleInput, request *llm.LLMRequest, module *topics.Module) error {
	if err := transaction.First(module, *request.ModuleID).Error; err != nil {
		return mapDatabaseNotFoundError(err)
	}
	var parameters moduleGenerationParameters
	if err := json.Unmarshal(request.Parameters, &parameters); err != nil {
		return err
	}
	if module.MaterialID == nil || *module.MaterialID != materialID || module.Title != input.Title ||
		module.TopicID != input.TopicID || module.Difficulty != input.Difficulty ||
		parameters.Count != input.Count || !slices.Equal(parameters.Types, input.Types) {
		return ErrMaterialIdempotencyConflict
	}
	if module.Status == models.ModuleReady {
		// Ответ на повторный POST использует только число опубликованных заданий.
		return transaction.Select("id").Where("module_id = ?", module.ID).Find(&module.Exercises).Error
	}
	return nil
}

// ListGeneratedModuleExercises возвращает готовые задания только из этого потока.
// По ID нельзя получить упражнения модулей, созданных в других частях приложения.
func (service *ModuleGenerationService) ListGeneratedModuleExercises(ctx context.Context, moduleID int64, limit, offset int) (*topics.Module, []topics.Exercise, error) {
	if moduleID <= 0 {
		return nil, nil, ErrResourceNotFound
	}
	if limit < 1 || limit > 101 || offset < 0 {
		return nil, nil, ErrInvalidMaterialInput
	}
	if service.database == nil {
		return nil, nil, ErrMaterialPreparationUnavailable
	}
	var module topics.Module
	exercises := make([]topics.Exercise, 0)
	err := service.database.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		// При смене версии промпта сохраняем доступ к модулям прежних общих версий.
		generatedInThisFlow := transaction.Model(&llm.LLMRequest{}).Select("1").
			Where("module_id = modules.id AND operation = ? AND prompt_version = ?", models.LLMGenerateModule, generation.ExerciseGenerationPromptV1)
		if err := transaction.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("material_id IS NOT NULL").Where("EXISTS (?)", generatedInThisFlow).
			First(&module, moduleID).Error; err != nil {
			return mapDatabaseNotFoundError(err)
		}
		if module.Status != models.ModuleReady {
			return ErrGeneratedModuleNotReady
		}
		return transaction.Select("id", "module_id", "position", "type", "instruction", "question_text", "content").
			Where("module_id = ?", module.ID).Order("position ASC").Limit(limit).Offset(offset).Find(&exercises).Error
	})
	if err != nil {
		return nil, nil, err
	}
	return &module, exercises, nil
}
