package httpapi

import (
	"lingoway/internal/middleware"
	"lingoway/internal/models/users"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func NewRouter(db *gorm.DB) *gin.Engine {
	r := gin.Default()

	authHandler := NewAuthHandler(db)

	api := r.Group("/api")
	{
		api.GET("/health", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		authGroup := api.Group("/auth")
		{
			authGroup.POST("/register", authHandler.Register)
			authGroup.POST("/login", authHandler.Login)
		}

		protected := api.Group("/")
		protected.Use(middleware.Auth())
		{
			protected.GET("/me", func(c *gin.Context) {
				claims, _ := middleware.GetClaims(c)
				c.JSON(http.StatusOK, gin.H{
					"user_id": claims.UserID,
					"role":    claims.Role,
				})
			})

			admin := protected.Group("/admin")
			admin.Use(middleware.RequireRole(users.RoleAdmin))
			{
				admin.GET("/dashboard", func(c *gin.Context) {
					c.JSON(http.StatusOK, gin.H{"message": "Admin panel"})
				})
			}
		}
	}

	return r
}
