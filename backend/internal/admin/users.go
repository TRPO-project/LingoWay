// Package admin содержит правила и запросы админ-панели. Формат HTTP задаёт пакет httpapi.
package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"lingoway/internal/models/users"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrUserNotFound     = errors.New("user not found")
	ErrCannotModifySelf = errors.New("administrator cannot modify own account")
	// ErrLastAdmin защищает инвариант «всегда есть активный администратор». При
	// запрете менять себя и проверке прав внутри транзакции до него не дойти: это
	// страховка на случай изменения этих правил.
	ErrLastAdmin     = errors.New("operation would leave no active administrator")
	ErrActorNotAdmin = errors.New("acting user is no longer an active administrator")
)

// PatchValidationError описывает неверное тело PATCH; Message безопасно показать клиенту.
type PatchValidationError struct{ Message string }

func (e *PatchValidationError) Error() string { return e.Message }

// UserPatch — частичное изменение пользователя; nil означает «не менять».
type UserPatch struct {
	Role      *users.UserRole
	IsBlocked *bool
}

// NewUserPatch проверяет входные значения. Через админку можно назначать только student и admin.
func NewUserPatch(role *string, isBlocked *bool) (UserPatch, error) {
	var patch UserPatch
	if role == nil && isBlocked == nil {
		return patch, &PatchValidationError{Message: "Укажите role или is_blocked."}
	}
	if role != nil {
		switch users.UserRole(*role) {
		case users.RoleStudent, users.RoleAdmin:
			value := users.UserRole(*role)
			patch.Role = &value
		default:
			return patch, &PatchValidationError{Message: "role: допустимы student и admin."}
		}
	}
	patch.IsBlocked = isBlocked
	return patch, nil
}

// UserFilter задаёт условия списка. nil и пустая строка означают «не фильтровать».
type UserFilter struct {
	Role    *users.UserRole
	Blocked *bool
	Search  string
	Limit   int
	Offset  int
}

// publicColumns исключает password_hash: хэш не должен даже загружаться.
const publicColumns = "id, email, name, role, is_blocked, created_at"

// UserService выполняет запросы к пользователям для админки.
type UserService struct{ db *gorm.DB }

func NewUserService(db *gorm.DB) *UserService { return &UserService{db: db} }

// List возвращает пользователей, сначала новые, и признак «есть ещё».
func (s *UserService) List(ctx context.Context, filter UserFilter) ([]users.User, bool, error) {
	query := s.db.WithContext(ctx).Model(&users.User{})
	if filter.Role != nil {
		query = query.Where("role = ?", string(*filter.Role))
	}
	if filter.Blocked != nil {
		query = query.Where("is_blocked = ?", *filter.Blocked)
	}
	if filter.Search != "" {
		pattern := "%" + escapeLike(filter.Search) + "%"
		query = query.Where(`(email ILIKE ? ESCAPE '\' OR name ILIKE ? ESCAPE '\')`, pattern, pattern)
	}

	var items []users.User
	err := query.Select(publicColumns).
		Order("id DESC").
		Limit(filter.Limit + 1).Offset(filter.Offset).
		Find(&items).Error
	if err != nil {
		return nil, false, fmt.Errorf("list users: %w", err)
	}
	hasMore := len(items) > filter.Limit
	if hasMore {
		items = items[:filter.Limit]
	}
	return items, hasMore, nil
}

// Get возвращает пользователя или ErrUserNotFound.
func (s *UserService) Get(ctx context.Context, id int64) (*users.User, error) {
	var user users.User
	err := s.db.WithContext(ctx).Select(publicColumns).Where("id = ?", id).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return &user, nil
}

// Update применяет изменение. Администратор не может менять свой аккаунт. В транзакции
// блокируются строки активных администраторов (по возрастанию ID, чтобы не было
// взаимной блокировки): параллельные изменения выстраиваются в очередь, а права
// действующего администратора проверяются ещё раз уже после получения блокировки.
func (s *UserService) Update(ctx context.Context, actorID, targetID int64, patch UserPatch) (*users.User, error) {
	if actorID == targetID {
		return nil, ErrCannotModifySelf
	}

	var updated users.User
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var admins []users.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id").
			Where("role = ? AND is_blocked = ?", string(users.RoleAdmin), false).
			Order("id").
			Find(&admins).Error; err != nil {
			return fmt.Errorf("lock administrators: %w", err)
		}
		actorIsAdmin := false
		for _, admin := range admins {
			if admin.ID == actorID {
				actorIsAdmin = true
			}
		}
		if !actorIsAdmin {
			return ErrActorNotAdmin
		}

		var target users.User
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", targetID).Take(&target).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUserNotFound
		}
		if err != nil {
			return fmt.Errorf("load user: %w", err)
		}

		newRole := target.Role
		if patch.Role != nil {
			newRole = *patch.Role
		}
		newBlocked := target.IsBlocked
		if patch.IsBlocked != nil {
			newBlocked = *patch.IsBlocked
		}

		wasActiveAdmin := target.Role == users.RoleAdmin && !target.IsBlocked
		willBeActiveAdmin := newRole == users.RoleAdmin && !newBlocked
		if wasActiveAdmin && !willBeActiveAdmin && len(admins) <= 1 {
			return ErrLastAdmin
		}

		if err := tx.Model(&users.User{}).Where("id = ?", targetID).Updates(map[string]any{
			"role":       string(newRole),
			"is_blocked": newBlocked,
		}).Error; err != nil {
			return fmt.Errorf("update user: %w", err)
		}
		return tx.Select(publicColumns).Where("id = ?", targetID).Take(&updated).Error
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

// escapeLike экранирует служебные символы LIKE, чтобы поиск по «50%» не стал маской.
func escapeLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}
