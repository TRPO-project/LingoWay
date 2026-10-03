package config

import (
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port        string
	DatabaseURL string
	JWTSecret   string
}

func Load() Config {
	// .env нужен только при локальном запуске без Docker.
	// Если файла нет, это не ошибка. Уже заданные переменные окружения не перезаписываются.
	for _, path := range []string{".env", "../.env"} {
		_ = godotenv.Load(path)
	}

	return Config{
		Port:        getenv("PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		JWTSecret:   os.Getenv("JWT_SECRET"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
