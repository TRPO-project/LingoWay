package catalog

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"lingoway/internal/models"
	"lingoway/internal/models/topics"
)

const (
	maxTitleRunes       = 200
	maxDescriptionRunes = 500
	maxBodyRunes        = 100000
	maxURLRunes         = 2048
)

// ErrTopicNotFound означает, что темы с указанным ID нет.
var ErrTopicNotFound = errors.New("topic not found")

// ValidationError описывает неверные данные материала. Message безопасно показывать клиенту.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(message string) error { return &ValidationError{Message: message} }

var validLevels = map[models.CEFRLevel]bool{
	models.LevelA1: true, models.LevelA2: true, models.LevelB1: true,
	models.LevelB2: true, models.LevelC1: true, models.LevelC2: true,
}

// MaterialInput — данные для создания или полной замены материала каталога.
type MaterialInput struct {
	TopicID     int64
	Level       models.CEFRLevel
	Type        models.CatalogMaterialType
	Title       string
	Description *string
	Body        *string
	URL         *string
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// Normalize обрезает пробелы, очищает поле, не относящееся к типу материала,
// и проверяет данные. Правила совпадают с ограничениями таблицы в БД, но строже
// их, чтобы нарушение всегда давало понятную ошибку, а не сбой записи.
func (in *MaterialInput) Normalize() error {
	for _, value := range []string{in.Title, deref(in.Description), deref(in.Body), deref(in.URL)} {
		if strings.ContainsRune(value, 0) {
			return invalid("Текстовые поля не должны содержать нулевых символов.")
		}
	}
	in.Title = strings.TrimSpace(in.Title)
	if count := utf8.RuneCountInString(in.Title); count < 1 || count > maxTitleRunes {
		return invalid("title: от 1 до 200 символов.")
	}
	if in.TopicID < 1 {
		return invalid("topic_id: укажите тему.")
	}
	if !validLevels[in.Level] {
		return invalid("level: допустимы A1, A2, B1, B2, C1, C2.")
	}
	if in.Description != nil {
		description := strings.TrimSpace(*in.Description)
		switch {
		case description == "":
			in.Description = nil
		case utf8.RuneCountInString(description) > maxDescriptionRunes:
			return invalid("description: не длиннее 500 символов.")
		default:
			in.Description = &description
		}
	}
	switch in.Type {
	case models.CatalogText:
		if strings.TrimSpace(deref(in.URL)) != "" {
			return invalid("url: для типа text не указывается.")
		}
		if strings.TrimSpace(deref(in.Body)) == "" {
			return invalid("body: для типа text нужен непустой текст.")
		}
		if utf8.RuneCountInString(*in.Body) > maxBodyRunes {
			return invalid("body: не длиннее 100000 символов.")
		}
		in.URL = nil
	case models.CatalogLink:
		if strings.TrimSpace(deref(in.Body)) != "" {
			return invalid("body: для типа link не указывается.")
		}
		link := strings.TrimSpace(deref(in.URL))
		if !validLink(link) {
			return invalid("url: нужен адрес вида http:// или https:// без пробелов, до 2048 символов.")
		}
		in.URL = &link
		in.Body = nil
	default:
		return invalid("type: допустимы text и link.")
	}
	return nil
}

// validLink принимает только http/https без пробелов и с адресом хоста.
// Схема проверяется в нижнем регистре: так требует ограничение в БД.
func validLink(raw string) bool {
	if raw == "" || utf8.RuneCountInString(raw) > maxURLRunes {
		return false
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return false
	}
	if strings.IndexFunc(raw, unicode.IsSpace) >= 0 {
		return false
	}
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Host != ""
}

func (s *Service) ensureTopic(ctx context.Context, topicID int64) error {
	var count int64
	if err := s.db.WithContext(ctx).Model(&topics.Topic{}).Where("id = ?", topicID).Count(&count).Error; err != nil {
		return fmt.Errorf("check topic: %w", err)
	}
	if count == 0 {
		return ErrTopicNotFound
	}
	return nil
}

// nullable превращает пустой указатель в SQL NULL.
func nullable(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

// CreateMaterial создаёт материал. Данные должны пройти Normalize.
func (s *Service) CreateMaterial(ctx context.Context, input MaterialInput) (*MaterialDetail, error) {
	if err := s.ensureTopic(ctx, input.TopicID); err != nil {
		return nil, err
	}
	material := topics.CatalogMaterial{
		TopicID:     input.TopicID,
		Level:       input.Level,
		Type:        input.Type,
		Title:       input.Title,
		Description: input.Description,
		Body:        input.Body,
		URL:         input.URL,
	}
	if err := s.db.WithContext(ctx).Create(&material).Error; err != nil {
		return nil, fmt.Errorf("create catalog material: %w", err)
	}
	return s.GetMaterial(ctx, 0, material.ID)
}

// UpdateMaterial полностью заменяет поля материала (включая тип). Пустые поля
// записываются как NULL. Данные должны пройти Normalize.
func (s *Service) UpdateMaterial(ctx context.Context, materialID int64, input MaterialInput) (*MaterialDetail, error) {
	if err := s.ensureTopic(ctx, input.TopicID); err != nil {
		return nil, err
	}
	result := s.db.WithContext(ctx).Model(&topics.CatalogMaterial{}).
		Where("id = ?", materialID).
		Updates(map[string]any{
			"topic_id":    input.TopicID,
			"level":       string(input.Level),
			"type":        string(input.Type),
			"title":       input.Title,
			"description": nullable(input.Description),
			"body":        nullable(input.Body),
			"url":         nullable(input.URL),
			"updated_at":  time.Now(),
		})
	if result.Error != nil {
		return nil, fmt.Errorf("update catalog material: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return s.GetMaterial(ctx, 0, materialID)
}

// DeleteMaterial удаляет материал. Отметки избранного удаляются каскадом.
func (s *Service) DeleteMaterial(ctx context.Context, materialID int64) error {
	result := s.db.WithContext(ctx).Delete(&topics.CatalogMaterial{}, materialID)
	if result.Error != nil {
		return fmt.Errorf("delete catalog material: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
