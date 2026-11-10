package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config объединяет настройки приложения. Значения загружаются один раз при старте;
// после изменения окружения сервер нужно перезапустить.
type Config struct {
	Port        string
	DatabaseURL string
	Database    DatabaseConfig
	LLM         LLMConfig // Подключение к модели и ограничения фоновой подготовки материала.
}

// DatabaseConfig настраивает пул PostgreSQL и ограничивает время старта сервера.
// Сам адрес подключения остаётся в Config.DatabaseURL и не должен попадать в логи.
type DatabaseConfig struct {
	AutoMigrate      bool
	MaxOpenConns     int
	MaxIdleConns     int
	ConnMaxLifetime  time.Duration
	ConnectTimeout   time.Duration
	MigrationTimeout time.Duration
}

// Load читает конфигурацию и возвращает ошибку до открытия порта при неверных настройках.
func Load() (Config, error) {
	// .env нужен только при локальном запуске без Docker.
	// Если файла нет, это не ошибка. Уже заданные переменные окружения не перезаписываются.
	for _, path := range []string{".env", "../.env"} {
		_ = godotenv.Load(path)
	}

	return fromEnvironment()
}

func fromEnvironment() (Config, error) {
	cfg := Config{
		Port:        getenv("PORT", "8080"),
		DatabaseURL: strings.TrimSpace(os.Getenv("DATABASE_URL")),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	port, err := strconv.Atoi(cfg.Port)
	if err != nil || port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("PORT must be an integer between 1 and 65535")
	}
	if cfg.Database.AutoMigrate, err = envBool("DB_AUTO_MIGRATE", true); err != nil {
		return Config{}, err
	}
	if cfg.Database.MaxOpenConns, err = envInt("DB_MAX_OPEN_CONNS", 10, 1); err != nil {
		return Config{}, err
	}
	if cfg.Database.MaxIdleConns, err = envInt("DB_MAX_IDLE_CONNS", 5, 0); err != nil {
		return Config{}, err
	}
	if cfg.Database.MaxIdleConns > cfg.Database.MaxOpenConns {
		return Config{}, fmt.Errorf("DB_MAX_IDLE_CONNS must not exceed DB_MAX_OPEN_CONNS")
	}
	if cfg.Database.ConnMaxLifetime, err = envDuration("DB_CONN_MAX_LIFETIME", 30*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.Database.ConnectTimeout, err = envDuration("DB_CONNECT_TIMEOUT", 5*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.Database.MigrationTimeout, err = envDuration("DB_MIGRATION_TIMEOUT", time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.LLM, err = loadLLMConfig(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) (bool, error) {
	raw, exists := os.LookupEnv(key)
	if !exists {
		return fallback, nil
	}
	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean (true or false)", key)
	}
	return value, nil
}

func envInt(key string, fallback, minimum int) (int, error) {
	raw, exists := os.LookupEnv(key)
	if !exists {
		return fallback, nil
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < minimum {
		return 0, fmt.Errorf("%s must be an integer greater than or equal to %d", key, minimum)
	}
	return value, nil
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw, exists := os.LookupEnv(key)
	if !exists {
		return fallback, nil
	}
	value, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration (for example, 5s or 30m)", key)
	}
	return value, nil
}
