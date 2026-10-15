package llm

import (
	"time"

	"lingoway/internal/models/users"
)

// LLMRequest — единственная сущность интеграции с LLM: очередь, параметры и итог.
// Тексты промптов и схемы ответов хранятся в версионируемых файлах приложения.
// В зависимости от Operation заполнен только один из MaterialID/ModuleID/AnswerID;
// составные внешние ключи в SQL не позволяют использовать чужие объекты.
type LLMRequest struct {
	ID            int64        `gorm:"column:id;type:bigint;primaryKey;autoIncrement" json:"id"`
	OwnerUserID   int64        `gorm:"column:owner_user_id;type:bigint;not null" json:"-"`
	RequestKey    string       `gorm:"column:request_key;type:uuid;not null;default:gen_random_uuid()" json:"request_key"`
	Operation     LLMOperation `gorm:"column:operation;type:text;not null" json:"operation"`
	MaterialID    *int64       `gorm:"column:material_id;type:bigint" json:"material_id"`
	ModuleID      *int64       `gorm:"column:module_id;type:bigint" json:"module_id"`
	AnswerID      *int64       `gorm:"column:answer_id;type:bigint" json:"answer_id"`
	Status        LLMStatus    `gorm:"column:status;type:text;not null;default:queued" json:"status"`
	Parameters    JSON         `gorm:"column:parameters;type:jsonb;not null;default:'{}'" json:"-"`
	PromptVersion string       `gorm:"column:prompt_version;type:text;not null" json:"-"`
	Provider      *string      `gorm:"column:provider;type:text" json:"-"`
	Model         *string      `gorm:"column:model;type:text" json:"-"`
	Result        JSON         `gorm:"column:result;type:jsonb" json:"-"`
	ErrorCode     *string      `gorm:"column:error_code;type:text" json:"error_code,omitempty"`
	ErrorMessage  *string      `gorm:"column:error_message;type:text" json:"-"`
	AttemptCount  int          `gorm:"column:attempt_count;type:integer;not null;default:0" json:"-"`
	AvailableAt   time.Time    `gorm:"column:available_at;type:timestamptz;not null;default:now()" json:"-"`
	// LeaseToken защищает запись результата от устаревшего воркера после повторного
	// захвата задания. Завершение требует актуальный token и неистёкшую аренду.
	LeaseToken     *string    `gorm:"column:lease_token;type:uuid" json:"-"`
	LeaseExpiresAt *time.Time `gorm:"column:lease_expires_at;type:timestamptz" json:"-"`
	CreatedAt      time.Time  `gorm:"column:created_at;type:timestamptz;not null;default:now();autoCreateTime:false" json:"created_at"`
	FinishedAt     *time.Time `gorm:"column:finished_at;type:timestamptz" json:"finished_at"`

	OwnerUser *users.User `gorm:"foreignKey:OwnerUserID;references:ID" json:"-"`
}

func (LLMRequest) TableName() string { return "llm_requests" }
