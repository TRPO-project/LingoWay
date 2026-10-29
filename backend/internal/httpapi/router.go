// Package httpapi связывает HTTP-запросы с сервисом подготовки материалов.
// Здесь проверяется формат входных данных; работа с БД и очередью остаётся
// в materials. На текущем этапе все маршруты работают без проверки пользователя.
package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// RouterOptions передаёт маршрутизатору зависимости обработчиков.
// Проверка /api/health работает и без сервиса материалов.
type RouterOptions struct {
	// MaterialService выполняет операции в общем режиме без проверки владельца.
	MaterialService MaterialPreparationService
}

// NewRouter создаёт общий HTTP API без авторизации. Материалы и статусы запросов
// доступны по ID. Без options работают health и предварительное извлечение текста;
// создание и чтение материала требуют переданного сервиса.
// Подключение к БД передаётся отдельно для будущих обработчиков каталога.
func NewRouter(database *gorm.DB, options ...RouterOptions) *gin.Engine {
	_ = database
	router := gin.Default()
	// Не доверяем переданному клиентом X-Forwarded-For. При появлении обратного
	// прокси его доверенные адреса нужно настроить явно.
	_ = router.SetTrustedProxies(nil)

	api := router.Group("/api")
	api.GET("/health", func(requestContext *gin.Context) {
		requestContext.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	var routerOptions RouterOptions
	if len(options) > 0 {
		routerOptions = options[0]
	}
	materialHandlers := materialHTTPHandler{materialPreparationService: routerOptions.MaterialService}
	materialAPI := api.Group("")
	// Проверка пользователя временно отключена, но ответы с текстами и состоянием
	// обработки по-прежнему не должны сохраняться HTTP-клиентами и общими кэшами.
	materialAPI.Use(func(requestContext *gin.Context) {
		requestContext.Header("Cache-Control", "private, no-store")
		requestContext.Next()
	})
	materialAPI.POST("/materials/extract", materialHandlers.previewUploadedMaterialText)
	materialAPI.POST("/materials", materialHandlers.createTextMaterial)
	materialAPI.GET("/materials/:id", materialHandlers.getMaterialByID)
	materialAPI.GET("/llm-requests/:id", materialHandlers.getLLMRequestByID)

	return router
}
