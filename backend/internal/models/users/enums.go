package users

// UserRole определяет базовые права пользователя; прогресс к роли не относится.
type UserRole string

const (
	RoleStudent UserRole = "student"
	RoleTeacher UserRole = "teacher"
	RoleAdmin   UserRole = "admin"
)
