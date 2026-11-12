package catalog

import (
	"context"
	"errors"
	"fmt"

	"lingoway/internal/models/topics"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// MaxUserTopics — сколько тем может выбрать пользователь. Лимит держит программу
	// сфокусированной; при изменении обновите контракт в docs/CONTRACTS.md.
	MaxUserTopics = 5
	// maxTopicIDsInRequest защищает от огромных списков: проверка выполняется до запросов к БД.
	maxTopicIDsInRequest = 100
)

// ErrUserNotFound означает, что пользователя удалили во время запроса.
var ErrUserNotFound = errors.New("user not found")

// SelectedTopic — выбранная тема. Position всегда идёт подряд с 1, даже если после
// удаления темы администратором в хранилище остались пропуски.
type SelectedTopic struct {
	TopicID  int64  `gorm:"column:topic_id"`
	Slug     string `gorm:"column:slug"`
	Name     string `gorm:"column:name"`
	Position int    `gorm:"column:position"`
}

// normalizeTopicSelection убирает повторы (приоритет у первого вхождения) и проверяет размер.
func normalizeTopicSelection(ids []int64) ([]int64, error) {
	if len(ids) > maxTopicIDsInRequest {
		return nil, invalid("topic_ids: слишком много значений.")
	}
	seen := make(map[int64]bool, len(ids))
	ordered := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id < 1 {
			return nil, invalid("topic_ids: только положительные числа.")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		ordered = append(ordered, id)
	}
	if len(ordered) == 0 {
		return nil, invalid("topic_ids: выберите хотя бы одну тему.")
	}
	if len(ordered) > MaxUserTopics {
		return nil, invalid(fmt.Sprintf("topic_ids: не больше %d тем.", MaxUserTopics))
	}
	return ordered, nil
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

func selectedTopics(db *gorm.DB, userID int64) ([]SelectedTopic, error) {
	items := []SelectedTopic{}
	err := db.Table("user_topics AS ut").
		Select(`ut.topic_id AS topic_id, t.slug AS slug, t.name AS name, ut."position" AS "position"`).
		Joins("JOIN topics AS t ON t.id = ut.topic_id").
		Where("ut.user_id = ?", userID).
		Order(`ut."position" ASC`).
		Scan(&items).Error
	if err != nil {
		return nil, fmt.Errorf("list selected topics: %w", err)
	}
	for index := range items {
		items[index].Position = index + 1
	}
	return items, nil
}

// GetSelectedTopics возвращает темы пользователя по приоритету. Пустой список допустим.
func (s *Service) GetSelectedTopics(ctx context.Context, userID int64) ([]SelectedTopic, error) {
	return selectedTopics(s.db.WithContext(ctx), userID)
}

// SetSelectedTopics заменяет выбор целиком. Порядок topic_ids задаёт приоритет.
// В транзакции блокируется строка пользователя: два одновременных запроса одного
// пользователя выстраиваются в очередь, а не ломаются о ограничения уникальности.
func (s *Service) SetSelectedTopics(ctx context.Context, userID int64, topicIDs []int64) ([]SelectedTopic, error) {
	ordered, err := normalizeTopicSelection(topicIDs)
	if err != nil {
		return nil, err
	}

	var result []SelectedTopic
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked []int64
		if err := tx.Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).
			Table("users").Where("id = ?", userID).Pluck("id", &locked).Error; err != nil {
			return fmt.Errorf("lock user: %w", err)
		}
		if len(locked) == 0 {
			return ErrUserNotFound
		}

		var count int64
		if err := tx.Model(&topics.Topic{}).Where("id IN ?", ordered).Count(&count).Error; err != nil {
			return fmt.Errorf("check topics: %w", err)
		}
		if count != int64(len(ordered)) {
			return ErrTopicNotFound
		}

		if err := tx.Where("user_id = ?", userID).Delete(&topics.UserTopic{}).Error; err != nil {
			return fmt.Errorf("clear selected topics: %w", err)
		}
		rows := make([]topics.UserTopic, len(ordered))
		for index, topicID := range ordered {
			rows[index] = topics.UserTopic{UserID: userID, TopicID: topicID, Position: index + 1}
		}
		if err := tx.Create(&rows).Error; err != nil {
			if isForeignKeyViolation(err) {
				// Тему удалили между проверкой и записью.
				return ErrTopicNotFound
			}
			return fmt.Errorf("save selected topics: %w", err)
		}

		var loadErr error
		result, loadErr = selectedTopics(tx, userID)
		return loadErr
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
