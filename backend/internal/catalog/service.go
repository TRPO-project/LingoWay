// Package catalog содержит запросы к каталогу готовых материалов: чтение, фильтры и избранное. Формат HTTP-запросов и ответов задаёт пакет httpapi.
package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"lingoway/internal/models"
	"lingoway/internal/models/topics"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrNotFound означает, что материала каталога с таким ID нет.
var ErrNotFound = errors.New("catalog material not found")

// isFavoriteSQL проверяет, что материал в избранном у пользователя (один параметр: user_id).
const isFavoriteSQL = "EXISTS (SELECT 1 FROM catalog_material_favorites f " +
	"WHERE f.material_id = catalog_materials.id AND f.user_id = ?)"

const itemColumns = "catalog_materials.id, catalog_materials.topic_id, catalog_materials.level, " +
	"catalog_materials.type, catalog_materials.title, catalog_materials.description, " +
	"catalog_materials.url, catalog_materials.created_at"

const detailColumns = itemColumns + ", catalog_materials.body, catalog_materials.updated_at"

// Service выполняет запросы к каталогу. Он не знает про HTTP и авторизацию: ID пользователя приходит параметром.
type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

// ListFilter задаёт условия выбора. Пустой список или nil означает «не фильтровать».
type ListFilter struct {
	TopicIDs []int64
	Levels   []string
	Favorite *bool
	Search   string
	Limit    int
	Offset   int
}

// MaterialItem — строка списка, без полного текста.
type MaterialItem struct {
	ID          int64                      `gorm:"column:id"`
	TopicID     int64                      `gorm:"column:topic_id"`
	Level       models.CEFRLevel           `gorm:"column:level"`
	Type        models.CatalogMaterialType `gorm:"column:type"`
	Title       string                     `gorm:"column:title"`
	Description *string                    `gorm:"column:description"`
	URL         *string                    `gorm:"column:url"`
	CreatedAt   time.Time                  `gorm:"column:created_at"`
	IsFavorite  bool                       `gorm:"column:is_favorite"`
}

// MaterialDetail — полный материал.
type MaterialDetail struct {
	ID          int64                      `gorm:"column:id"`
	TopicID     int64                      `gorm:"column:topic_id"`
	Level       models.CEFRLevel           `gorm:"column:level"`
	Type        models.CatalogMaterialType `gorm:"column:type"`
	Title       string                     `gorm:"column:title"`
	Description *string                    `gorm:"column:description"`
	URL         *string                    `gorm:"column:url"`
	Body        *string                    `gorm:"column:body"`
	CreatedAt   time.Time                  `gorm:"column:created_at"`
	UpdatedAt   time.Time                  `gorm:"column:updated_at"`
	IsFavorite  bool                       `gorm:"column:is_favorite"`
}

// ListTopics возвращает темы по порядку ID и признак «есть ещё».
func (s *Service) ListTopics(ctx context.Context, limit, offset int) ([]topics.Topic, bool, error) {
	var items []topics.Topic
	err := s.db.WithContext(ctx).Order("id ASC").Limit(limit + 1).Offset(offset).Find(&items).Error
	if err != nil {
		return nil, false, fmt.Errorf("list topics: %w", err)
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}

// ListMaterials возвращает материалы, сначала новые. Условия объединяются через «и», значения внутри одного условия (несколько тем или уровней) через «или».
func (s *Service) ListMaterials(ctx context.Context, userID int64, filter ListFilter) ([]MaterialItem, bool, error) {
	query := s.db.WithContext(ctx).Model(&topics.CatalogMaterial{})
	if len(filter.TopicIDs) > 0 {
		query = query.Where("catalog_materials.topic_id IN ?", filter.TopicIDs)
	}
	if len(filter.Levels) > 0 {
		query = query.Where("catalog_materials.level IN ?", filter.Levels)
	}
	if filter.Search != "" {
		query = query.Where(`catalog_materials.title ILIKE ? ESCAPE '\'`, "%"+escapeLike(filter.Search)+"%")
	}
	if filter.Favorite != nil {
		if *filter.Favorite {
			query = query.Where(isFavoriteSQL, userID)
		} else {
			query = query.Where("NOT "+isFavoriteSQL, userID)
		}
	}

	var items []MaterialItem
	err := query.
		Select(itemColumns+", "+isFavoriteSQL+" AS is_favorite", userID).
		Order("catalog_materials.id DESC").
		Limit(filter.Limit + 1).Offset(filter.Offset).
		Scan(&items).Error
	if err != nil {
		return nil, false, fmt.Errorf("list catalog materials: %w", err)
	}
	hasMore := len(items) > filter.Limit
	if hasMore {
		items = items[:filter.Limit]
	}
	return items, hasMore, nil
}

// GetMaterial возвращает полный материал или ErrNotFound.
func (s *Service) GetMaterial(ctx context.Context, userID, materialID int64) (*MaterialDetail, error) {
	var detail MaterialDetail
	result := s.db.WithContext(ctx).Model(&topics.CatalogMaterial{}).
		Select(detailColumns+", "+isFavoriteSQL+" AS is_favorite", userID).
		Where("catalog_materials.id = ?", materialID).
		Limit(1).Scan(&detail)
	if result.Error != nil {
		return nil, fmt.Errorf("get catalog material: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return &detail, nil
}

// AddFavorite добавляет материал в избранное. Повторный вызов ничего не меняет.
func (s *Service) AddFavorite(ctx context.Context, userID, materialID int64) error {
	var count int64
	if err := s.db.WithContext(ctx).Model(&topics.CatalogMaterial{}).
		Where("id = ?", materialID).Count(&count).Error; err != nil {
		return fmt.Errorf("check catalog material: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	favorite := topics.CatalogMaterialFavorite{UserID: userID, MaterialID: materialID}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&favorite).Error; err != nil {
		return fmt.Errorf("add favorite: %w", err)
	}
	return nil
}

// RemoveFavorite убирает материал из избранного. Если его там не было, это не ошибка.
func (s *Service) RemoveFavorite(ctx context.Context, userID, materialID int64) error {
	err := s.db.WithContext(ctx).
		Where("user_id = ? AND material_id = ?", userID, materialID).
		Delete(&topics.CatalogMaterialFavorite{}).Error
	if err != nil {
		return fmt.Errorf("remove favorite: %w", err)
	}
	return nil
}

// escapeLike экранирует символы, которые в LIKE имеют особый смысл, чтобы поиск по строке «50%» не превращался в маску.
func escapeLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}
