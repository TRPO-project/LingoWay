package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"lingoway/internal/config"
	"lingoway/internal/database"
	"lingoway/internal/generation"
	"lingoway/internal/httpapi"
	"lingoway/internal/materials"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

// run связывает настройки, PostgreSQL, клиент LLM, фоновую очередь и HTTP-сервер.
// Общий контекст отменяется при остановке приложения и завершает сетевую работу.
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

	// Отсутствие ключей LLM не мешает запуску сервера: чтение существующих
	// материалов и извлечение текста остаются доступны в общем режиме.
	llmClient, err := generation.NewChatCompletionClient(cfg.LLM, nil)
	if err != nil {
		return fmt.Errorf("initialize LLM client: %w", err)
	}
	materialService := materials.NewMaterialService(db, llmClient)
	moduleGenerationService := materials.NewModuleGenerationService(db, llmClient)
	workerContext, stopWorkers := context.WithCancel(ctx)
	var workers sync.WaitGroup
	if llmClient.HasAPIKeys() {
		workerOptions := materials.LLMWorkerOptions{
			LLMRequestTimeout: cfg.LLM.Timeout, QueuePollInterval: cfg.LLM.PollInterval,
			MaxAttempts: cfg.LLM.MaxAttempts,
		}
		preparationWorker := materials.NewPreparationWorker(materialService, workerOptions)
		moduleGenerationWorker := materials.NewModuleGenerationWorker(moduleGenerationService, workerOptions)
		workers.Go(func() { preparationWorker.RunUntilStopped(workerContext) })
		workers.Go(func() { moduleGenerationWorker.RunUntilStopped(workerContext) })
	} else {
		log.Print("LLM workers disabled: configure LLM_API_KEYS")
	}
	// Останавливаем оба воркера до закрытия пула БД. Незавершённые запросы
	// остаются в очереди и восстанавливаются после истечения аренды.
	defer func() {
		stopWorkers()
		workers.Wait()
	}()

	server := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: httpapi.NewRouter(db, httpapi.RouterOptions{
			MaterialService: materialService, ModuleGenerationService: moduleGenerationService,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
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
