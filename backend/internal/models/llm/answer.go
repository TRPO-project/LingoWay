package llm

import (
	"time"

	"lingoway/internal/models/users"
)

// Answer — отдельный отправленный ответ и результат его проверки; это не прогресс.
// Повтор с тем же (UserID, RequestKey) возвращает тот же ответ. После проверки
// ответ неизменяем; новую попытку оформляют новой записью с новым RequestKey.
type Answer struct {
	ID         int64        `gorm:"column:id;type:bigint;primaryKey;autoIncrement" json:"id"`
	UserID     int64        `gorm:"column:user_id;type:bigint;not null" json:"user_id"`
	ExerciseID int64        `gorm:"column:exercise_id;type:bigint;not null" json:"exercise_id"`
	RequestKey string       `gorm:"column:request_key;type:uuid;not null;default:gen_random_uuid()" json:"request_key"`
	Response   JSON         `gorm:"column:response;type:jsonb;not null" json:"response"`
	Status     AnswerStatus `gorm:"column:status;type:text;not null;default:pending" json:"status"`
	// PostgreSQL округляет Score до 5 знаков (numeric(6,5)); float64 нужен для
	// числового API. Финансовую/произвольную точность этот тип не гарантирует.
	Score *float64 `gorm:"column:score;type:numeric(6,5)" json:"score"`
	// IsCorrect вычисляет PostgreSQL как (score = 1). Поле только для чтения;
	// отдельная SQL-миграция создаёт GENERATED ALWAYS ... STORED после AutoMigrate.
	IsCorrect *bool      `gorm:"column:is_correct;->;-:migration" json:"is_correct"`
	Feedback  *string    `gorm:"column:feedback;type:text" json:"feedback"`
	ErrorCode *string    `gorm:"column:error_code;type:text" json:"error_code,omitempty"`
	CreatedAt time.Time  `gorm:"column:created_at;type:timestamptz;not null;default:now();autoCreateTime:false" json:"created_at"`
	CheckedAt *time.Time `gorm:"column:checked_at;type:timestamptz" json:"checked_at"`

	User     *users.User `gorm:"foreignKey:UserID;references:ID" json:"-"`
	Exercise *Exercise   `gorm:"foreignKey:ExerciseID;references:ID" json:"-"`
}

func (Answer) TableName() string { return "answers" }
