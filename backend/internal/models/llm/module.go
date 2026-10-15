package llm

import (
	"time"

	"lingoway/internal/models/topics"
	"lingoway/internal/models/users"
)

// Module объединяет несколько упражнений с общей темой и сложностью.
// CreatedByUserID может быть NULL у публичного системного модуля. Автор — служебное
// поле, поэтому он скрыт из JSON даже при наличии. Для личных модулей и модулей
// по материалу владелец обязателен; связь владельца материала проверяется в SQL.
type Module struct {
	ID              int64            `gorm:"column:id;type:bigint;primaryKey;autoIncrement" json:"id"`
	CreatedByUserID *int64           `gorm:"column:created_by_user_id;type:bigint" json:"-"`
	MaterialID      *int64           `gorm:"column:material_id;type:bigint" json:"material_id"`
	TopicID         int64            `gorm:"column:topic_id;type:bigint;not null" json:"topic_id"`
	Title           string           `gorm:"column:title;type:text;not null" json:"title"`
	Difficulty      CEFRLevel        `gorm:"column:difficulty;type:text;not null" json:"difficulty"`
	Visibility      ModuleVisibility `gorm:"column:visibility;type:text;not null;default:private" json:"visibility"`
	Status          ModuleStatus     `gorm:"column:status;type:text;not null;default:draft" json:"status"`
	CreatedAt       time.Time        `gorm:"column:created_at;type:timestamptz;not null;default:now();autoCreateTime:false" json:"created_at"`
	PublishedAt     *time.Time       `gorm:"column:published_at;type:timestamptz" json:"published_at"`

	Creator   *users.User   `gorm:"foreignKey:CreatedByUserID;references:ID" json:"-"`
	Topic     *topics.Topic `gorm:"foreignKey:TopicID;references:ID" json:"-"`
	Exercises []Exercise    `gorm:"foreignKey:ModuleID;references:ID" json:"-"`
	// Составная связь (MaterialID, CreatedByUserID) задана SQL-миграцией.
	// Для загрузки исходного материала используйте MaterialID и проверку владельца.
}

func (Module) TableName() string { return "modules" }
