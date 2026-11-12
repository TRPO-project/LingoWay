package httpapi

import (
	"time"

	"lingoway/internal/models"
	"lingoway/internal/models/topics"
)

// generatedModuleResponse содержит публичные поля модуля, включая число уже
// сохранённых упражнений. Технический владелец и сами упражнения в ответ не входят.
type generatedModuleResponse struct {
	ID            int64                   `json:"id"`
	Title         string                  `json:"title"`
	TopicID       int64                   `json:"topic_id"`
	Difficulty    models.CEFRLevel        `json:"difficulty"`
	MaterialID    *int64                  `json:"material_id"`
	Visibility    models.ModuleVisibility `json:"visibility"`
	Status        models.ModuleStatus     `json:"status"`
	ExerciseCount int                     `json:"exercise_count"`
	CreatedAt     time.Time               `json:"created_at"`
	PublishedAt   *time.Time              `json:"published_at"`
}

func newGeneratedModuleResponse(module *topics.Module) generatedModuleResponse {
	return generatedModuleResponse{
		ID:            module.ID,
		Title:         module.Title,
		TopicID:       module.TopicID,
		Difficulty:    module.Difficulty,
		MaterialID:    module.MaterialID,
		Visibility:    module.Visibility,
		Status:        module.Status,
		ExerciseCount: len(module.Exercises),
		CreatedAt:     module.CreatedAt,
		PublishedAt:   module.PublishedAt,
	}
}

// generatedExerciseResponse показывает задание ученику. Уровень и тема берутся
// из модуля. Ключ ответа и объяснение остаются в модели для проверки ответа.
type generatedExerciseResponse struct {
	ID           int64               `json:"id"`
	ModuleID     int64               `json:"module_id"`
	Position     int                 `json:"position"`
	Type         models.ExerciseType `json:"type"`
	Instruction  string              `json:"instruction"`
	QuestionText string              `json:"question_text"`
	Content      models.JSON         `json:"content"`
	Level        models.CEFRLevel    `json:"level"`
	TopicID      int64               `json:"topic_id"`
}

func newGeneratedExerciseResponse(module *topics.Module, exercise topics.Exercise) generatedExerciseResponse {
	return generatedExerciseResponse{
		ID:           exercise.ID,
		ModuleID:     exercise.ModuleID,
		Position:     exercise.Position,
		Type:         exercise.Type,
		Instruction:  exercise.Instruction,
		QuestionText: exercise.QuestionText,
		Content:      exercise.Content,
		Level:        module.Difficulty,
		TopicID:      module.TopicID,
	}
}
