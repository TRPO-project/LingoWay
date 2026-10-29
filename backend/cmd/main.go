package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"lingoway/internal/config"
	"lingoway/internal/database"
	"lingoway/internal/httpapi"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.Open(ctx, cfg)
	if err != nil {
		return err
	}
	pool, err := db.DB()
	if err != nil {
		return fmt.Errorf("obtain database pool: %w", err)
	}
	defer pool.Close()

	// Схема готовится до приёма запросов. При неуспешной миграции сервер не
	// запускается с частично совместимыми моделями и существующей базой.
	if cfg.Database.AutoMigrate {
		migrationCtx, cancel := context.WithTimeout(ctx, cfg.Database.MigrationTimeout)
		err = database.Migrate(migrationCtx, db)
		cancel()
		if err != nil {
			return fmt.Errorf("migrate database: %w", err)
		}
	}

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           httpapi.NewRouter(db),
		ReadHeaderTimeout: 5 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	log.Printf("server started on :%s", cfg.Port)
	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		// Заканчиваем активные HTTP-запросы перед закрытием пула PostgreSQL.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("shutdown HTTP: %w", err)
		}
		return nil
	}
}
