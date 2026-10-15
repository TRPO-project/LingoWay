package topics

import (
	"time"

	"lingoway/internal/models"
)

// Exercise — одно задание внутри модуля; Position задаёт порядок начиная с 1.
// Content содержит публичные варианты/пары/слова, AnswerKey — приватный эталон.
// Difficulty и Topic не дублируются: они принадлежат модулю.
type Exercise struct {
	ID           int64               `gorm:"column:id;type:bigint;primaryKey;autoIncrement" json:"id"`
	ModuleID     int64               `gorm:"column:module_id;type:bigint;not null" json:"module_id"`
	Position     int                 `gorm:"column:position;type:integer;not null" json:"position"`
	Type         models.ExerciseType `gorm:"column:type;type:text;not null" json:"type"`
	Instruction  string              `gorm:"column:instruction;type:text;not null" json:"instruction"`
	QuestionText string              `gorm:"column:question_text;type:text;not null" json:"question_text"`
	Content      models.JSON         `gorm:"column:content;type:jsonb;not null" json:"content"`
	AnswerKey    models.JSON         `gorm:"column:answer_key;type:jsonb;not null" json:"-"`
	// Explanation выдаётся через DTO результата проверки, а не вместе с заданием.
	Explanation string    `gorm:"column:explanation;type:text;not null;default:''" json:"-"`
	CreatedAt   time.Time `gorm:"column:created_at;type:timestamptz;not null;default:now();autoCreateTime:false" json:"created_at"`

	Module *Module `gorm:"foreignKey:ModuleID;references:ID" json:"-"`
}

func (Exercise) TableName() string { return "exercises" }
