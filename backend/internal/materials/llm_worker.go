package materials

import (
	"context"
	"log"
	"time"
)

// LLMWorkerOptions задаёт время вызова модели, интервал чтения очереди и число попыток.
type LLMWorkerOptions struct {
	LLMRequestTimeout time.Duration
	QueuePollInterval time.Duration
	MaxAttempts       int
}

func (options LLMWorkerOptions) withDefaults() LLMWorkerOptions {
	if options.LLMRequestTimeout <= 0 {
		options.LLMRequestTimeout = 45 * time.Second
	}
	if options.QueuePollInterval <= 0 {
		options.QueuePollInterval = time.Second
	}
	if options.MaxAttempts <= 0 {
		options.MaxAttempts = 3
	}
	return options
}

// runLLMWorker сразу берёт следующую работу после успешной обработки.
// При пустой очереди или ошибке БД ждёт следующего интервала.
func runLLMWorker(ctx context.Context, pollInterval time.Duration, workerName string, processNextRequest func(context.Context) (bool, error)) {
	pollTicker := time.NewTicker(pollInterval)
	defer pollTicker.Stop()
	for ctx.Err() == nil {
		requestWasClaimed, err := processNextRequest(ctx)
		if err != nil && ctx.Err() == nil {
			// Ошибка драйвера может содержать текст материала или данные подключения.
			log.Printf("%s: queue operation failed", workerName)
		}
		if requestWasClaimed && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-pollTicker.C:
		}
	}
}
