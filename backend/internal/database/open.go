// Package database открывает PostgreSQL и синхронизирует схему перед запуском HTTP.
package database

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"lingoway/internal/config"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open создаёт ограниченный пул и проверяет реальное подключение до старта HTTP.
// При ошибке пул закрывается. DATABASE_URL и необработанные ошибки драйвера могут
// содержать пароль, поэтому наружу возвращаются только безопасные сообщения.
func Open(ctx context.Context, cfg config.Config) (*gorm.DB, error) {
	if cfg.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	if cfg.Database.ConnectTimeout <= 0 || cfg.Database.ConnMaxLifetime <= 0 ||
		cfg.Database.MaxOpenConns <= 0 || cfg.Database.MaxIdleConns < 0 ||
		cfg.Database.MaxIdleConns > cfg.Database.MaxOpenConns {
		return nil, errors.New("invalid database pool or connection timeout configuration")
	}
	connectionConfig, err := pgx.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, errors.New("invalid DATABASE_URL: expected a PostgreSQL connection string")
	}
	// Ограничиваем и отдельную попытку драйвера, и всю проверку подключения ниже.
	connectionConfig.ConnectTimeout = cfg.Database.ConnectTimeout
	// Миграции и модели используют общую схему public; значение из DSN не должно
	// направить рабочие запросы в другую схему после завершения миграции.
	if connectionConfig.RuntimeParams == nil {
		connectionConfig.RuntimeParams = make(map[string]string)
	}
	connectionConfig.RuntimeParams["search_path"] = "public"
	pool := stdlib.OpenDB(*connectionConfig)
	pool.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	pool.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	pool.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{
		DisableAutomaticPing:                     true,
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger: logger.New(log.New(os.Stdout, "", log.LstdFlags), logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			ParameterizedQueries:      true,
			Colorful:                  false,
		}),
	})
	if err != nil {
		_ = pool.Close()
		return nil, errors.New("initialize GORM for PostgreSQL: initialization failed")
	}
	connectCtx, cancel := context.WithTimeout(ctx, cfg.Database.ConnectTimeout)
	defer cancel()
	if err := pool.PingContext(connectCtx); err != nil {
		_ = pool.Close()
		if connectCtx.Err() != nil {
			return nil, fmt.Errorf("connect to PostgreSQL: %w", connectCtx.Err())
		}
		return nil, errors.New("connect to PostgreSQL: connection failed; check DATABASE_URL and server availability")
	}
	return db, nil
}
