package users

import "time"

// User — базовая учётная запись без прогресса и личного словаря.
// Уникальность email без учёта регистра и краевых пробелов обеспечивается SQL-индексом.
type User struct {
	ID           int64     `gorm:"column:id;type:bigint;primaryKey;autoIncrement" json:"id"`
	Email        string    `gorm:"column:email;type:text;not null" json:"email"`
	PasswordHash string    `gorm:"column:password_hash;type:text;not null" json:"-"`
	Name         string    `gorm:"column:name;type:text;not null" json:"name"`
	Role         UserRole  `gorm:"column:role;type:text;not null;default:student" json:"role"`
	CreatedAt    time.Time `gorm:"column:created_at;type:timestamptz;not null;default:now();autoCreateTime:false" json:"created_at"`
}

func (User) TableName() string { return "users" }
