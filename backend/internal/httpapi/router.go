// Package httpapi связывает маршруты с сервисами приложения.
package httpapi

import (
	"net/http"

	"lingoway/internal/middleware"
	"lingoway/internal/models/users"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// RouterOptions передаёт маршрутизатору зависимости обработчиков.
// Проверка /api/health работает и без сервиса материалов.
type RouterOptions struct {
	// MaterialService выполняет операции в общем режиме без проверки владельца.
	MaterialService MaterialPreparationService
	// ModuleGenerationService создаёт модули из материалов и читает готовые упражнения.
	ModuleGenerationService ModuleGenerationService
}

// NewRouter подключает авторизацию и общие маршруты работы с материалами и генерацией.
// Без options доступны health и предварительное извлечение текста из файла.
func NewRouter(database *gorm.DB, options ...RouterOptions) *gin.Engine {
	router := gin.Default()
	// Не доверяем переданному клиентом X-Forwarded-For. При появлении обратного
	// прокси его доверенные адреса нужно настроить явно.
	_ = router.SetTrustedProxies(nil)

	api := router.Group("/api")
	api.GET("/health", func(requestContext *gin.Context) {
		requestContext.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	authHandler := NewAuthHandler(database)

	authGroup := api.Group("/auth")
	{
		authGroup.POST("/register", authHandler.Register)
		authGroup.POST("/login", authHandler.Login)
	}

	protected := api.Group("")
	protected.Use(middleware.Auth(), middleware.ActiveUser(database))
	{
		protected.GET("/me", func(c *gin.Context) {
			claims, _ := middleware.GetClaims(c)
			c.JSON(http.StatusOK, gin.H{
				"user_id": claims.UserID,
				"role":    claims.Role,
			})
		})

		registerCatalogRoutes(protected, database)
		registerUserTopicRoutes(protected, database)

		admin := protected.Group("/admin")
		admin.Use(middleware.RequireRole(users.RoleAdmin))
		{
			admin.GET("/dashboard", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"message": "Admin panel"})
			})
			registerAdminCatalogRoutes(admin, database)
			registerAdminUserRoutes(admin, database)
			registerAdminTopicRoutes(admin, database)
		}
	}
	var routerOptions RouterOptions
	if len(options) > 0 {
		routerOptions = options[0]
	}
	materialHandlers := materialHTTPHandler{materialPreparationService: routerOptions.MaterialService}
	moduleHandlers := moduleGenerationHTTPHandler{service: routerOptions.ModuleGenerationService}
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
	materialAPI.POST("/materials/:id/modules", moduleHandlers.createGeneratedModule)
	materialAPI.GET("/modules/:id/exercises", moduleHandlers.listGeneratedModuleExercises)

	return router
}
