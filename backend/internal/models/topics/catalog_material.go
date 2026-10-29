package topics

import (
	"time"

	"lingoway/internal/models"
)

// CatalogMaterial — готовый материал каталога. Его добавляет и правит только админ.
// Для type=text заполнено Body, для type=link заполнен URL (только http/https);
// согласованность полей проверяет SQL-ограничение. Это отдельная сущность от
// llm.Material: там лежат материалы пользователей для генерации заданий.
type CatalogMaterial struct {
	ID          int64                      `gorm:"column:id;type:bigint;primaryKey;autoIncrement" json:"id"`
	TopicID     int64                      `gorm:"column:topic_id;type:bigint;not null" json:"topic_id"`
	Level       models.CEFRLevel           `gorm:"column:level;type:text;not null" json:"level"`
	Type        models.CatalogMaterialType `gorm:"column:type;type:text;not null" json:"type"`
	Title       string                     `gorm:"column:title;type:text;not null" json:"title"`
	Description *string                    `gorm:"column:description;type:text" json:"description"`
	Body        *string                    `gorm:"column:body;type:text" json:"body"`
	URL         *string                    `gorm:"column:url;type:text" json:"url"`
	CreatedAt   time.Time                  `gorm:"column:created_at;type:timestamptz;not null;default:now();autoCreateTime:false" json:"created_at"`
	UpdatedAt   time.Time                  `gorm:"column:updated_at;type:timestamptz;not null;default:now();autoUpdateTime" json:"updated_at"`

	Topic *Topic `gorm:"foreignKey:TopicID;references:ID" json:"-"`
}

func (CatalogMaterial) TableName() string { return "catalog_materials" }

// CatalogMaterialFavorite — отметка «в избранном» у конкретного пользователя.
// Составной первичный ключ не даёт добавить один материал дважды.
type CatalogMaterialFavorite struct {
	UserID     int64     `gorm:"column:user_id;type:bigint;primaryKey;autoIncrement:false" json:"-"`
	MaterialID int64     `gorm:"column:material_id;type:bigint;primaryKey;autoIncrement:false" json:"material_id"`
	CreatedAt  time.Time `gorm:"column:created_at;type:timestamptz;not null;default:now();autoCreateTime:false" json:"created_at"`
}

func (CatalogMaterialFavorite) TableName() string { return "catalog_material_favorites" }
