package auth

import (
	"errors"
	"lingoway/internal/models/users"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var jwtSecret = os.Getenv("JWT_SECRET")

type Claims struct {
	UserID int64          `json:"user_id"`
	Role   users.UserRole `json:"role"`
	jwt.RegisteredClaims
}

func getSecretKey() []byte {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return []byte("dev-secret-key")
	}
	return []byte(secret)
}

func GenerateToken(user users.User) (string, error) {
	claims := Claims{
		UserID: user.ID,
		Role:   user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(getSecretKey())
}

func ParseToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return getSecretKey(), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token")
}
