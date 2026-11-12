package database

import (
	"fmt"

	"lingoway/internal/models/topics"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// seedTopics добавляет стартовые темы только в пустую таблицу. Дальше темами управляют
// администраторы: иначе удалённая стартовая тема возвращалась бы при каждом запуске.
// Slug — стабильный ключ, после запуска его менять нельзя: на него могут ссылаться
// другие части проекта.
func seedTopics(tx *gorm.DB) error {
	var count int64
	if err := tx.Model(&topics.Topic{}).Count(&count).Error; err != nil {
		return fmt.Errorf("count topics: %w", err)
	}
	if count > 0 {
		return nil
	}
	initial := []topics.Topic{
		{Slug: "it", Name: "IT"},
		{Slug: "aviation", Name: "Авиация"},
		{Slug: "travel", Name: "Путешествия"},
		{Slug: "business", Name: "Бизнес"},
		{Slug: "medicine", Name: "Медицина"},
		{Slug: "science", Name: "Наука"},
		{Slug: "music", Name: "Музыка"},
		{Slug: "cinema", Name: "Кино"},
		{Slug: "sport", Name: "Спорт"},
		{Slug: "games", Name: "Игры"},
	}
	err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "slug"}},
		DoNothing: true,
	}).Create(&initial).Error
	if err != nil {
		return fmt.Errorf("seed topics: %w", err)
	}
	return nil
}
