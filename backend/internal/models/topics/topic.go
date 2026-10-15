package topics

// Topic — общая тема материалов и модулей (например, travel или business).
// Slug — стабильный машинный ключ, Name — название для пользователя.
type Topic struct {
	ID   int64  `gorm:"column:id;type:bigint;primaryKey;autoIncrement" json:"id"`
	Slug string `gorm:"column:slug;type:text;not null;unique" json:"slug"`
	Name string `gorm:"column:name;type:text;not null" json:"name"`
}

func (Topic) TableName() string { return "topics" }
