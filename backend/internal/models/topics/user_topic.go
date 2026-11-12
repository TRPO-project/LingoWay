package topics

import "time"

// UserTopic — тема, выбранная пользователем. Position (с 1) задаёт приоритет: меньше
// значит важнее. Составной первичный ключ не даёт выбрать одну тему дважды, а
// уникальность (user_id, position) в SQL запрещает одинаковые приоритеты.
// Отметки удаляются каскадом вместе с темой или пользователем.
type UserTopic struct {
	UserID    int64     `gorm:"column:user_id;type:bigint;primaryKey;autoIncrement:false" json:"-"`
	TopicID   int64     `gorm:"column:topic_id;type:bigint;primaryKey;autoIncrement:false" json:"topic_id"`
	Position  int       `gorm:"column:position;type:integer;not null" json:"position"`
	CreatedAt time.Time `gorm:"column:created_at;type:timestamptz;not null;default:now();autoCreateTime:false" json:"created_at"`
}

func (UserTopic) TableName() string { return "user_topics" }
