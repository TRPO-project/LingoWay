package middleware

import (
	"errors"
	"log"
	"net/http"

	"lingoway/internal/models/users"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ActiveUser выполняется после Auth. JWT нельзя отозвать (сервер его не хранит), поэтому
// состояние пользователя проверяется в БД при каждом запросе: удалённый получает 401,
// заблокированный 403. Роль берётся из БД, а не из токена, поэтому её смена
// действует сразу, без повторного входа.
func ActiveUser(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := GetClaims(c)
		if !ok {
			abortWithError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Требуется вход в систему.")
			return
		}

		var user users.User
		err := db.WithContext(c.Request.Context()).
			Select("id", "role", "is_blocked").
			Where("id = ?", claims.UserID).
			Take(&user).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			abortWithError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Пользователь не найден. Войдите заново.")
			return
		case err != nil:
			log.Printf("active user check: %v", err)
			abortWithError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Не удалось выполнить запрос.")
			return
		}
		if user.IsBlocked {
			abortWithError(c, http.StatusForbidden, "ACCOUNT_BLOCKED", "Аккаунт заблокирован.")
			return
		}

		// claims создаётся заново на каждый запрос, поэтому изменение безопасно;
		// RequireRole и обработчики увидят актуальную роль.
		claims.Role = user.Role
		c.Next()
	}
}

func abortWithError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
