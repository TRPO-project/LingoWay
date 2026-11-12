package generation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"lingoway/internal/config"
)

// Это верхний предел текста в слое интеграции и в БД. Приём новых материалов
// устанавливает более строгий предел. Клиент отправляет весь текст целиком.
const maxModelSourceCharacters = 200000

// LLMProviderError передаёт безопасный код ошибки и возможность повтора.
// В ней нет адреса провайдера, ключа, исходного материала или тела ответа модели.
type LLMProviderError struct {
	// Code — стабильный служебный код для очереди и публичного описания ошибки.
	Code string
	// Retryable разрешает воркеру повторить временно неудавшийся вызов.
	Retryable bool
	// RetryAfter хранит задержку из HTTP-заголовка провайдера. Ноль означает,
	// что подходящей подсказки нет.
	RetryAfter time.Duration
}

// Error возвращает только код: сообщение провайдера может содержать приватные данные.
func (providerError *LLMProviderError) Error() string { return providerError.Code }

// IsRetryableProviderError распознаёт LLMProviderError, в том числе внутри
// обёрнутой ошибки. Неизвестные ошибки не разрешают автоматический повтор.
func IsRetryableProviderError(err error) bool {
	var providerError *LLMProviderError
	return errors.As(err, &providerError) && providerError != nil && providerError.Retryable
}

// ProviderRetryDelay извлекает подсказку о задержке из временной ошибки,
// в том числе обёрнутой. Возвращает ноль, если подходящей подсказки нет.
func ProviderRetryDelay(err error) time.Duration {
	var providerError *LLMProviderError
	if !errors.As(err, &providerError) || providerError == nil || !providerError.Retryable || providerError.RetryAfter <= 0 {
		return 0
	}
	return min(providerError.RetryAfter, maxProviderRetryDelay)
}

// ChatCompletionClient обслуживает оценку CEFR и генерацию через совместимый
// API. Воркеры делят один клиент и по очереди используют настроенные ключи.
type ChatCompletionClient struct {
	apiBaseURL      string
	modelName       string
	apiKeys         []string
	nextAPIKeyIndex atomic.Uint64
	requestTimeout  time.Duration
	httpClient      *http.Client
}

// NewChatCompletionClient проверяет настройки без обращения к провайдеру.
// Переданный HTTP-клиент копируется: запрет редиректов не меняет настройки
// вызывающего кода и не даёт переслать ключ по адресу из ответа провайдера.
func NewChatCompletionClient(llmConfig config.LLMConfig, httpClient *http.Client) (*ChatCompletionClient, error) {
	llmConfig.BaseURL = strings.TrimSpace(llmConfig.BaseURL)
	llmConfig.Model = strings.TrimSpace(llmConfig.Model)
	if err := llmConfig.Validate(); err != nil {
		return nil, err
	}
	providerHTTPClient := http.Client{}
	if httpClient != nil {
		providerHTTPClient = *httpClient
	}
	providerHTTPClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	apiKeys := make([]string, len(llmConfig.APIKeys))
	for index, apiKey := range llmConfig.APIKeys {
		apiKeys[index] = strings.TrimSpace(apiKey)
	}
	return &ChatCompletionClient{
		apiBaseURL:     strings.TrimRight(llmConfig.BaseURL, "/"),
		modelName:      llmConfig.Model,
		apiKeys:        apiKeys,
		requestTimeout: llmConfig.Timeout,
		httpClient:     &providerHTTPClient,
	}, nil
}

// HasAPIKeys сообщает, заданы ли ключи для вызовов модели. Доступность API
// и действительность ключа здесь не проверяются; сетевой запрос не выполняется.
func (client *ChatCompletionClient) HasAPIKeys() bool {
	return client != nil && len(client.apiKeys) > 0
}

// ProviderName задаёт название протокола интеграции для журнала запросов.
// Оно одинаково для внешнего сервиса и локальной совместимой модели.
func (client *ChatCompletionClient) ProviderName() string { return "openai-compatible" }

// ModelName возвращает имя модели из настроек для сохранения в журнале LLM.
func (client *ChatCompletionClient) ModelName() string {
	if client == nil {
		return ""
	}
	return client.modelName
}

// requestJSONCompletion отправляет инструкцию приложения и материал разными
// сообщениями. Каждый вызов делает один запрос. Повторами управляет очередь.
func (client *ChatCompletionClient) requestJSONCompletion(ctx context.Context, instruction, sourceText string, responseLimit int64) (string, error) {
	if !client.HasAPIKeys() {
		return "", &LLMProviderError{Code: "LLM_NOT_CONFIGURED"}
	}
	if !utf8.ValidString(sourceText) || strings.TrimSpace(sourceText) == "" || utf8.RuneCountInString(sourceText) > maxModelSourceCharacters {
		return "", &LLMProviderError{Code: "LLM_INVALID_INPUT"}
	}
	// Ограничение действует и с HTTP-клиентом без собственного Timeout.
	// Более ранний срок или отмена родительского контекста также сохраняются.
	requestContext, cancel := context.WithTimeout(ctx, client.requestTimeout)
	defer cancel()
	httpRequest, err := client.buildChatCompletionRequest(requestContext, instruction, sourceText)
	if err != nil {
		return "", err
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return "", classifyProviderTransportError(requestContext, err)
	}
	defer httpResponse.Body.Close()
	return readChatCompletionContent(requestContext, httpResponse, responseLimit)
}

func (client *ChatCompletionClient) buildChatCompletionRequest(ctx context.Context, instruction, sourceText string) (*http.Request, error) {
	payload := chatCompletionRequest{
		Model: client.modelName,
		Messages: []chatCompletionMessage{
			{Role: "system", Content: instruction},
			{Role: "user", Content: sourceText},
		},
		ResponseFormat: jsonResponseFormat{Type: "json_object"},
	}
	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, &LLMProviderError{Code: "LLM_INVALID_INPUT"}
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, client.apiBaseURL+"/chat/completions", bytes.NewReader(encodedPayload))
	if err != nil {
		return nil, &LLMProviderError{Code: "LLM_PROVIDER_ERROR"}
	}
	// Ключи выбираются по кругу без гонок. Переключение ключа не запускает
	// скрытый повтор при ошибке: один вызов остаётся одним запросом.
	apiKey := client.apiKeys[(client.nextAPIKeyIndex.Add(1)-1)%uint64(len(client.apiKeys))]
	httpRequest.Header.Set("Authorization", "Bearer "+apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	return httpRequest, nil
}

// Эти DTO содержат только поля OpenAI-совместимого запроса, которые мы отправляем.
type chatCompletionRequest struct {
	Model          string                  `json:"model"`
	Messages       []chatCompletionMessage `json:"messages"`
	ResponseFormat jsonResponseFormat      `json:"response_format"`
}

type chatCompletionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type jsonResponseFormat struct {
	Type string `json:"type"`
}

// classifyProviderTransportError заменяет сетевую ошибку безопасным кодом.
// Отмена вызывающим кодом отличается от истечения таймаута: сама по себе
// она не означает временную неисправность провайдера.
func classifyProviderTransportError(ctx context.Context, err error) *LLMProviderError {
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return &LLMProviderError{Code: "LLM_CANCELLED"}
	}
	var networkError net.Error
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkError) && networkError.Timeout()) {
		return &LLMProviderError{Code: "LLM_TIMEOUT", Retryable: true}
	}
	return &LLMProviderError{Code: "LLM_UNAVAILABLE", Retryable: true}
}
