package generation

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"lingoway/internal/config"
	"lingoway/internal/models"
)

// CEFRDetectionPromptVersion связывает запрос очереди с конкретной версией
// инструкции и формата результата. Эти файлы встраиваются в приложение.
const CEFRDetectionPromptVersion = "prepare-material-v1"

// Это верхний предел текста в общем слое интеграции и в БД. Приём материалов
// устанавливает более строгий лимит для текущего этапа приложения.
const maxCEFRMaterialCharacters = 200000

//go:embed prompts/prepare-material-v1.txt
var cefrDetectionPrompt string

//go:embed schemas/prepare-material-v1.json
var cefrDetectionSchema string

// LLMProviderError передаёт безопасный код ошибки и возможность повтора.
// В ней нет адреса провайдера, ключа, исходного материала или тела ответа модели.
type LLMProviderError struct {
	// Code — стабильный служебный код для очереди и публичного описания ошибки.
	Code string
	// Retryable разрешает воркеру повторить временно неудавшийся вызов.
	Retryable bool
}

// Error возвращает только код: сообщение провайдера может содержать приватные данные.
func (providerError *LLMProviderError) Error() string { return providerError.Code }

// IsRetryableProviderError распознаёт LLMProviderError, в том числе внутри
// обёрнутой ошибки. Неизвестные ошибки не разрешают автоматический повтор.
func IsRetryableProviderError(err error) bool {
	var providerError *LLMProviderError
	return errors.As(err, &providerError) && providerError.Retryable
}

// CEFRLevelClient определяет сложность английского текста через OpenAI-совместимый
// API. Один экземпляр можно использовать из нескольких горутин: выбор следующего
// ключа синхронизирован. Очередью, повторами и сохранением управляет пакет materials.
type CEFRLevelClient struct {
	apiBaseURL      string
	modelName       string
	apiKeys         []string
	nextAPIKeyIndex atomic.Uint64
	requestTimeout  time.Duration
	httpClient      *http.Client
}

// NewCEFRLevelClient проверяет настройки без обращения к провайдеру.
// Переданный HTTP-клиент копируется: запрет редиректов не меняет настройки
// вызывающего кода и не даёт переслать ключ по адресу из ответа провайдера.
func NewCEFRLevelClient(llmConfig config.LLMConfig, httpClient *http.Client) (*CEFRLevelClient, error) {
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
	return &CEFRLevelClient{
		apiBaseURL:     strings.TrimRight(llmConfig.BaseURL, "/"),
		modelName:      llmConfig.Model,
		apiKeys:        apiKeys,
		requestTimeout: llmConfig.Timeout,
		httpClient:     &providerHTTPClient,
	}, nil
}

// HasAPIKeys сообщает, заданы ли ключи для вызовов модели. Доступность API
// и действительность ключа здесь не проверяются; сетевой запрос не выполняется.
func (client *CEFRLevelClient) HasAPIKeys() bool { return client != nil && len(client.apiKeys) > 0 }

// ProviderName задаёт название протокола интеграции для журнала запросов.
// Оно одинаково для внешнего сервиса и локальной совместимой модели.
func (client *CEFRLevelClient) ProviderName() string { return "openai-compatible" }

// ModelName возвращает имя модели из настроек для сохранения в журнале LLM.
func (client *CEFRLevelClient) ModelName() string {
	if client == nil {
		return ""
	}
	return client.modelName
}

// DetectCEFRLevel определяет сложность английского текста по шкале CEFR (A1–C2).
// Исходный текст не изменяется. Метод делает один HTTP-вызов; решение о повторе
// принимает воркер очереди. Разбор и проверка ответа находятся в отдельном файле.
func (client *CEFRLevelClient) DetectCEFRLevel(ctx context.Context, sourceText string) (models.CEFRLevel, error) {
	if !client.HasAPIKeys() {
		return "", &LLMProviderError{Code: "LLM_NOT_CONFIGURED"}
	}
	if !utf8.ValidString(sourceText) || strings.TrimSpace(sourceText) == "" || utf8.RuneCountInString(sourceText) > maxCEFRMaterialCharacters {
		return "", &LLMProviderError{Code: "LLM_INVALID_INPUT"}
	}
	// Ограничение действует и с HTTP-клиентом без собственного Timeout.
	// Более ранний срок или отмена родительского контекста также сохраняются.
	requestContext, cancel := context.WithTimeout(ctx, client.requestTimeout)
	defer cancel()
	httpRequest, err := client.buildCEFRDetectionRequest(requestContext, sourceText)
	if err != nil {
		return "", err
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return "", classifyProviderTransportError(requestContext, err)
	}
	defer httpResponse.Body.Close()
	return readCEFRLevelResponse(requestContext, httpResponse)
}

// buildCEFRDetectionRequest отделяет недоверенный пользовательский текст
// от инструкции приложения и схемы результата. Здесь же задаётся авторизация.
func (client *CEFRLevelClient) buildCEFRDetectionRequest(ctx context.Context, sourceText string) (*http.Request, error) {
	payload := chatCompletionRequest{
		Model: client.modelName,
		Messages: []chatCompletionMessage{
			{Role: "system", Content: cefrDetectionPrompt + "\nExpected JSON schema:\n" + cefrDetectionSchema},
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
