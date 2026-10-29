package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// NewRouter собирает маршруты. Подключение к БД пока не используется:
// его получат обработчики следующих задач (каталог материалов и другие).
func NewRouter(db *gorm.DB) *gin.Engine {
	_ = db
	r := gin.Default()

	api := r.Group("/api")
	api.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	return r
}
