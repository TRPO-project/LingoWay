package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

// LLMConfig задаёт подключение к OpenAI-совместимому API и работу очереди.
// Пустой список ключей выключает вызовы LLM, но позволяет запустить приложение.
// BaseURL — базовый адрес API, обычно с путём /v1, без /chat/completions.
// Model — имя модели, которое провайдер ожидает в поле model запроса.
// APIKeys — секретные ключи из LLM_API_KEYS через запятую; выбираются по кругу.
// Timeout — предельная длительность одного вызова; в окружении, например, 45s.
// PollInterval — пауза между проверками очереди без готовой работы, например 1s.
// MaxAttempts — общее число попыток запроса, включая первую; допустимо от 1 до 10.
type LLMConfig struct {
	BaseURL      string
	Model        string
	APIKeys      []string
	Timeout      time.Duration
	PollInterval time.Duration
	MaxAttempts  int
}

// loadLLMConfig читает настройки из окружения. Пустые элементы списка ключей
// отбрасываются, а неверные адреса и пределы возвращают ошибку до запуска сервера.
func loadLLMConfig() (LLMConfig, error) {
	cfg := LLMConfig{
		BaseURL: strings.TrimSpace(getenv("LLM_BASE_URL", "https://api.openai.com/v1")),
		Model:   strings.TrimSpace(getenv("LLM_MODEL", "gpt-4o-mini")),
	}
	for _, key := range strings.Split(os.Getenv("LLM_API_KEYS"), ",") {
		if key = strings.TrimSpace(key); key != "" {
			cfg.APIKeys = append(cfg.APIKeys, key)
		}
	}
	var err error
	if cfg.Timeout, err = envDuration("LLM_TIMEOUT", 45*time.Second); err != nil {
		return LLMConfig{}, err
	}
	if cfg.PollInterval, err = envDuration("LLM_POLL_INTERVAL", time.Second); err != nil {
		return LLMConfig{}, err
	}
	if cfg.MaxAttempts, err = envInt("LLM_MAX_ATTEMPTS", 3, 1); err != nil {
		return LLMConfig{}, err
	}
	if err := cfg.Validate(); err != nil {
		return LLMConfig{}, err
	}
	return cfg, nil
}

// Validate проверяет настройки, не раскрывая адрес провайдера и секреты в ошибках.
// HTTP без шифрования разрешён только для localhost и loopback-адресов:
// это позволяет работать с локальной моделью, не отправляя ключи по внешнему HTTP.
// Логин, пароль, параметры запроса и фрагмент URL запрещены; авторизация задаётся
// отдельно через APIKeys и заголовок Authorization.
func (cfg LLMConfig) Validate() error {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Host == "" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(cfg.BaseURL, "#") || u.Opaque != "" {
		return fmt.Errorf("LLM_BASE_URL must be an HTTP(S) URL without credentials, query or fragment")
	}
	switch u.Scheme {
	case "https":
	case "http":
		host := u.Hostname()
		ip := net.ParseIP(host)
		if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
			return fmt.Errorf("LLM_BASE_URL requires HTTPS except for localhost or a loopback address")
		}
	default:
		return fmt.Errorf("LLM_BASE_URL must use HTTP or HTTPS")
	}
	if strings.TrimSpace(cfg.Model) == "" || strings.ContainsAny(cfg.Model, "\r\n") {
		return fmt.Errorf("LLM_MODEL must be a nonempty single-line model name")
	}
	for _, key := range cfg.APIKeys {
		if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "\r\n") {
			return fmt.Errorf("LLM_API_KEYS must contain nonempty single-line keys")
		}
	}
	if cfg.Timeout <= 0 {
		return fmt.Errorf("LLM_TIMEOUT must be a positive duration")
	}
	if cfg.PollInterval <= 0 {
		return fmt.Errorf("LLM_POLL_INTERVAL must be a positive duration")
	}
	if cfg.MaxAttempts < 1 || cfg.MaxAttempts > 10 {
		return fmt.Errorf("LLM_MAX_ATTEMPTS must be an integer between 1 and 10")
	}
	return nil
}
