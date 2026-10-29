package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"lingoway/internal/auth"
	"lingoway/internal/models/users"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type AuthHandler struct {
	db *gorm.DB
}

func NewAuthHandler(db *gorm.DB) *AuthHandler {
	return &AuthHandler{db: db}
}

type RegisterRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Name     string `json:"name" binding:"required"`
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type AuthResponse struct {
	Token string     `json:"token"`
	User  users.User `json:"user"`
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req RegisterRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid data: check email and password"})
		return
	}

	cleanEmail := strings.ToLower(strings.TrimSpace(req.Email))
	cleanName := strings.TrimSpace(req.Name)

	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Bcrypt error"})
		return
	}

	newUser := users.User{
		Email:        cleanEmail,
		PasswordHash: string(hashedBytes),
		Name:         cleanName,
		Role:         users.RoleStudent,
	}

	if err := h.db.WithContext(c.Request.Context()).Create(&newUser).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "User with this email already exists"})
		return
	}

	token, err := auth.GenerateToken(newUser)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Can not generate token"})
		return
	}

	c.JSON(http.StatusCreated, AuthResponse{
		Token: token,
		User:  newUser,
	})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Enter correct email and password"})
		return
	}

	cleanEmail := strings.ToLower(strings.TrimSpace(req.Email))

	var user users.User
	err := h.db.WithContext(c.Request.Context()).
		Where("lower(trim(email)) = ?", cleanEmail).
		First(&user).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "DB error"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
		return
	}

	token, err := auth.GenerateToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Can not generate token"})
		return
	}

	c.JSON(http.StatusOK, AuthResponse{
		Token: token,
		User:  user,
	})
}
