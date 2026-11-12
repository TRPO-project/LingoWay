package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"lingoway/internal/admin"
	"lingoway/internal/models/users"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const adminUsersMaxBodyBytes = 1 << 16

type adminUserHTTPHandler struct {
	service *admin.UserService
}

// registerAdminUserRoutes подключает управление пользователями к группе администратора
// (Auth, ActiveUser и RequireRole уже применены в router.go).
func registerAdminUserRoutes(adminGroup *gin.RouterGroup, database *gorm.DB) {
	handler := adminUserHTTPHandler{service: admin.NewUserService(database)}

	group := adminGroup.Group("/users", privateNoStore())
	{
		group.GET("", handler.list)
		group.GET("/:id", handler.get)
		group.PATCH("/:id", handler.patch)
	}
}

type adminUserResponse struct {
	ID        int64          `json:"id"`
	Email     string         `json:"email"`
	Name      string         `json:"name"`
	Role      users.UserRole `json:"role"`
	IsBlocked bool           `json:"is_blocked"`
	CreatedAt time.Time      `json:"created_at"`
}

func newAdminUserResponse(user users.User) adminUserResponse {
	return adminUserResponse{
		ID: user.ID, Email: user.Email, Name: user.Name,
		Role: user.Role, IsBlocked: user.IsBlocked, CreatedAt: user.CreatedAt,
	}
}

// adminUserPatchRequest — тело PATCH; неизвестные поля запрещены.
type adminUserPatchRequest struct {
	Role      *string `json:"role"`
	IsBlocked *bool   `json:"is_blocked"`
}

func (h adminUserHTTPHandler) list(c *gin.Context) {
	limit, offset, ok := parsePagination(c)
	if !ok {
		return
	}
	filter := admin.UserFilter{Limit: limit, Offset: offset}

	if raw := c.Query("role"); raw != "" {
		role := users.UserRole(raw)
		switch role {
		case users.RoleStudent, users.RoleTeacher, users.RoleAdmin:
			filter.Role = &role
		default:
			writeAPIError(c, http.StatusBadRequest, "INVALID_QUERY", "role допускает значения student, teacher, admin.")
			return
		}
	}
	switch c.Query("blocked") {
	case "":
	case "true":
		value := true
		filter.Blocked = &value
	case "false":
		value := false
		filter.Blocked = &value
	default:
		writeAPIError(c, http.StatusBadRequest, "INVALID_QUERY", "blocked допускает значения true и false.")
		return
	}
	filter.Search = strings.TrimSpace(c.Query("q"))
	if utf8.RuneCountInString(filter.Search) > catalogMaxSearchLength {
		writeAPIError(c, http.StatusBadRequest, "INVALID_QUERY", "Поисковый запрос не длиннее 100 символов.")
		return
	}

	items, hasMore, err := h.service.List(c.Request.Context(), filter)
	if err != nil {
		writeAdminUserError(c, err)
		return
	}
	response := make([]adminUserResponse, 0, len(items))
	for _, user := range items {
		response = append(response, newAdminUserResponse(user))
	}
	c.JSON(http.StatusOK, catalogPage[adminUserResponse]{Items: response, Limit: limit, Offset: offset, HasMore: hasMore})
}

func (h adminUserHTTPHandler) get(c *gin.Context) {
	userID, ok := parseCatalogID(c)
	if !ok {
		return
	}
	user, err := h.service.Get(c.Request.Context(), userID)
	if err != nil {
		writeAdminUserError(c, err)
		return
	}
	c.JSON(http.StatusOK, newAdminUserResponse(*user))
}

func (h adminUserHTTPHandler) patch(c *gin.Context) {
	actorID, ok := currentUserID(c)
	if !ok {
		return
	}
	targetID, ok := parseCatalogID(c)
	if !ok {
		return
	}
	var request adminUserPatchRequest
	if !decodeStrictJSON(c, &request, adminUsersMaxBodyBytes) {
		return
	}
	patch, err := admin.NewUserPatch(request.Role, request.IsBlocked)
	if err != nil {
		writeAdminUserError(c, err)
		return
	}
	user, err := h.service.Update(c.Request.Context(), actorID, targetID, patch)
	if err != nil {
		writeAdminUserError(c, err)
		return
	}
	c.JSON(http.StatusOK, newAdminUserResponse(*user))
}

// decodeStrictJSON читает ровно один JSON-объект без неизвестных полей и лишних данных.
func decodeStrictJSON(c *gin.Context, destination any, limit int64) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()

	err := decoder.Decode(destination)
	if err == nil {
		if extra := decoder.Decode(&struct{}{}); !errors.Is(extra, io.EOF) {
			err = errors.New("unexpected data after JSON object")
		}
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeAPIError(c, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "Тело запроса слишком большое.")
		} else {
			writeAPIError(c, http.StatusBadRequest, "INVALID_JSON", "Проверьте JSON и набор полей запроса.")
		}
		return false
	}
	return true
}

// writeAdminUserError не отдаёт клиенту внутренние ошибки, только пишет их в лог.
func writeAdminUserError(c *gin.Context, err error) {
	var validation *admin.PatchValidationError
	switch {
	case errors.As(err, &validation):
		writeAPIError(c, http.StatusUnprocessableEntity, "INVALID_INPUT", validation.Message)
	case errors.Is(err, admin.ErrUserNotFound):
		writeAPIError(c, http.StatusNotFound, "NOT_FOUND", "Пользователь не найден.")
	case errors.Is(err, admin.ErrCannotModifySelf):
		writeAPIError(c, http.StatusConflict, "CANNOT_MODIFY_SELF", "Нельзя менять собственную роль или блокировать себя.")
	case errors.Is(err, admin.ErrLastAdmin):
		writeAPIError(c, http.StatusConflict, "LAST_ADMIN", "Нельзя оставить систему без активного администратора.")
	case errors.Is(err, admin.ErrActorNotAdmin):
		writeAPIError(c, http.StatusForbidden, "FORBIDDEN", "Недостаточно прав.")
	default:
		log.Printf("admin users: %v", err)
		writeAPIError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Не удалось выполнить запрос.")
	}
}
