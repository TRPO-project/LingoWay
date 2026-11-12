package admin

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"lingoway/internal/models/topics"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

const (
	maxTopicSlugLength = 50
	maxTopicNameRunes  = 100

	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
)

var (
	ErrTopicNotFound   = errors.New("topic not found")
	ErrTopicSlugExists = errors.New("topic slug already exists")
	// ErrTopicInUse означает, что на тему ссылаются материалы, модули или другие записи.
	ErrTopicInUse = errors.New("topic is in use")

	// Правило совпадает с ограничением topics_slug_check в БД.
	topicSlugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

// TopicValidationError описывает неверные данные темы; Message безопасно показать клиенту.
type TopicValidationError struct{ Message string }

func (e *TopicValidationError) Error() string { return e.Message }

func invalidTopic(message string) error { return &TopicValidationError{Message: message} }

func normalizeTopicSlug(raw string) (string, error) {
	slug := strings.TrimSpace(raw)
	if len(slug) < 1 || len(slug) > maxTopicSlugLength || !topicSlugPattern.MatchString(slug) {
		return "", invalidTopic("slug: строчные латинские буквы, цифры и дефисы (например, business-english), до 50 символов.")
	}
	return slug, nil
}

func normalizeTopicName(raw string) (string, error) {
	if strings.ContainsRune(raw, 0) {
		return "", invalidTopic("name: не должно содержать нулевых символов.")
	}
	name := strings.TrimSpace(raw)
	if count := utf8.RuneCountInString(name); count < 1 || count > maxTopicNameRunes {
		return "", invalidTopic("name: от 1 до 100 символов.")
	}
	return name, nil
}

// pgErrorCode возвращает код ошибки PostgreSQL или пустую строку.
func pgErrorCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// TopicService выполняет запросы управления темами.
type TopicService struct{ db *gorm.DB }

func NewTopicService(db *gorm.DB) *TopicService { return &TopicService{db: db} }

// Create создаёт тему. Повторный slug даёт ErrTopicSlugExists.
func (s *TopicService) Create(ctx context.Context, slug, name string) (*topics.Topic, error) {
	cleanSlug, err := normalizeTopicSlug(slug)
	if err != nil {
		return nil, err
	}
	cleanName, err := normalizeTopicName(name)
	if err != nil {
		return nil, err
	}
	topic := topics.Topic{Slug: cleanSlug, Name: cleanName}
	if err := s.db.WithContext(ctx).Create(&topic).Error; err != nil {
		if pgErrorCode(err) == pgUniqueViolation {
			return nil, ErrTopicSlugExists
		}
		return nil, fmt.Errorf("create topic: %w", err)
	}
	return &topic, nil
}

// Rename меняет название. Slug остаётся прежним: на него могут ссылаться другие части проекта.
func (s *TopicService) Rename(ctx context.Context, id int64, name string) (*topics.Topic, error) {
	cleanName, err := normalizeTopicName(name)
	if err != nil {
		return nil, err
	}
	result := s.db.WithContext(ctx).Model(&topics.Topic{}).Where("id = ?", id).Update("name", cleanName)
	if result.Error != nil {
		return nil, fmt.Errorf("rename topic: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, ErrTopicNotFound
	}
	var topic topics.Topic
	if err := s.db.WithContext(ctx).Where("id = ?", id).Take(&topic).Error; err != nil {
		return nil, fmt.Errorf("load topic: %w", err)
	}
	return &topic, nil
}

// Delete удаляет тему. Если на неё ссылаются материалы или модули, внешний ключ
// БД не даёт удалить её, и возвращается ErrTopicInUse (без предварительных проверок,
// поэтому гонки между проверкой и удалением нет).
func (s *TopicService) Delete(ctx context.Context, id int64) error {
	result := s.db.WithContext(ctx).Delete(&topics.Topic{}, id)
	if result.Error != nil {
		if pgErrorCode(result.Error) == pgForeignKeyViolation {
			return ErrTopicInUse
		}
		return fmt.Errorf("delete topic: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrTopicNotFound
	}
	return nil
}
