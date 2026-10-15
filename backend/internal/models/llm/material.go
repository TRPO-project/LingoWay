package llm

import (
	"time"

	"lingoway/internal/models"
	"lingoway/internal/models/topics"
	"lingoway/internal/models/users"
)

// Material — пользовательский источник, из которого извлекается текст для LLM.
// Файлы и аудио находятся в приватном S3/MinIO, в БД хранится только ObjectKey.
// Для text задаётся SourceText, для url — SourceURL, для file/audio — ObjectKey
// и метаданные файла. Эти варианты взаимно исключаются SQL-ограничением.
type Material struct {
	ID               int64                 `gorm:"column:id;type:bigint;primaryKey;autoIncrement" json:"id"`
	OwnerUserID      int64                 `gorm:"column:owner_user_id;type:bigint;not null" json:"owner_user_id"`
	RequestKey       string                `gorm:"column:request_key;type:uuid;not null;default:gen_random_uuid()" json:"request_key"`
	TopicID          *int64                `gorm:"column:topic_id;type:bigint" json:"topic_id"`
	Title            string                `gorm:"column:title;type:text;not null" json:"title"`
	Kind             models.MaterialKind   `gorm:"column:kind;type:text;not null" json:"kind"`
	Status           models.MaterialStatus `gorm:"column:status;type:text;not null;default:pending" json:"status"`
	SourceText       *string               `gorm:"column:source_text;type:text" json:"source_text,omitempty"`
	SourceURL        *string               `gorm:"column:source_url;type:text" json:"source_url,omitempty"`
	ObjectKey        *string               `gorm:"column:object_key;type:text" json:"-"`
	OriginalFilename *string               `gorm:"column:original_filename;type:text" json:"original_filename,omitempty"`
	ContentType      *string               `gorm:"column:content_type;type:text" json:"content_type,omitempty"`
	SizeBytes        *int64                `gorm:"column:size_bytes;type:bigint" json:"size_bytes,omitempty"`
	ContentSHA256    *string               `gorm:"column:content_sha256;type:text" json:"-"`
	ExtractedText    *string               `gorm:"column:extracted_text;type:text" json:"extracted_text,omitempty"`
	Level            *models.CEFRLevel     `gorm:"column:level;type:text" json:"level,omitempty"`
	ErrorCode        *string               `gorm:"column:error_code;type:text" json:"error_code,omitempty"`
	CreatedAt        time.Time             `gorm:"column:created_at;type:timestamptz;not null;default:now();autoCreateTime:false" json:"created_at"`

	// Связи загружаются явно через Preload; не включаем учётные записи в JSON.
	OwnerUser *users.User   `gorm:"foreignKey:OwnerUserID;references:ID" json:"-"`
	Topic     *topics.Topic `gorm:"foreignKey:TopicID;references:ID" json:"-"`
}

func (Material) TableName() string { return "materials" }
