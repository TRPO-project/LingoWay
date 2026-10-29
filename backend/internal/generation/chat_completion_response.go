package generation

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"lingoway/internal/models"
)

// Для одного уровня CEFR достаточно короткого ответа. Лимит 64 КиБ относится
// ко всему HTTP-ответу провайдера и ограничивает объём чтения в память.
const maxProviderResponseBytes = 64 << 10

// readCEFRLevelResponse проверяет HTTP-статус и объём ответа, затем последовательно
// разбирает оболочку Chat Completions и строгий JSON с результатом определения CEFR.
func readCEFRLevelResponse(ctx context.Context, httpResponse *http.Response) (models.CEFRLevel, error) {
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		// Тело ошибки не читаем: оно может повторять материал или служебные данные.
		return "", classifyProviderHTTPStatus(httpResponse.StatusCode)
	}
	// Дополнительный байт отличает ответ ровно на границе лимита от слишком
	// большого ответа, который иначе выглядел бы просто обрезанным.
	responseBody, err := io.ReadAll(io.LimitReader(httpResponse.Body, maxProviderResponseBytes+1))
	if err != nil {
		return "", classifyProviderTransportError(ctx, err)
	}
	if len(responseBody) > maxProviderResponseBytes {
		return "", &LLMProviderError{Code: "LLM_INVALID_RESPONSE"}
	}
	completionContent, err := parseChatCompletionContent(responseBody)
	if err != nil {
		return "", err
	}
	return parseCEFRLevelJSON(completionContent)
}

func classifyProviderHTTPStatus(statusCode int) *LLMProviderError {
	switch {
	case statusCode == http.StatusTooManyRequests:
		return &LLMProviderError{Code: "LLM_RATE_LIMITED", Retryable: true}
	case statusCode == http.StatusRequestTimeout:
		return &LLMProviderError{Code: "LLM_TIMEOUT", Retryable: true}
	case statusCode >= 500:
		return &LLMProviderError{Code: "LLM_UNAVAILABLE", Retryable: true}
	default:
		return &LLMProviderError{Code: "LLM_PROVIDER_ERROR"}
	}
}

// parseChatCompletionContent принимает один ответ модели без отказа или признака
// незавершённой генерации. Неиспользуемые поля протокола провайдера разрешены.
func parseChatCompletionContent(responseBody []byte) (string, error) {
	var completionResponse chatCompletionResponse
	if json.Unmarshal(responseBody, &completionResponse) != nil || len(completionResponse.Choices) != 1 {
		return "", &LLMProviderError{Code: "LLM_INVALID_RESPONSE"}
	}
	completionChoice := completionResponse.Choices[0]
	if completionChoice.Message.Refusal != "" || (completionChoice.FinishReason != "" && completionChoice.FinishReason != "stop") {
		return "", &LLMProviderError{Code: "LLM_INVALID_RESPONSE"}
	}
	return completionChoice.Message.Content, nil
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
			Refusal string `json:"refusal"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// parseCEFRLevelJSON разбирает ответ по токенам: обычный разбор в структуру
// допускает повтор ключа и другой регистр его имени. Здесь нужен ровно один ключ
// "level", без дополнительных полей, второго объекта и Markdown-обрамления.
func parseCEFRLevelJSON(completionContent string) (models.CEFRLevel, error) {
	decoder := json.NewDecoder(strings.NewReader(completionContent))
	openingToken, err := decoder.Token()
	if err != nil || openingToken != json.Delim('{') || !decoder.More() {
		return "", &LLMProviderError{Code: "LLM_INVALID_RESPONSE"}
	}
	fieldName, err := decoder.Token()
	var detectedLevel models.CEFRLevel
	if err != nil || fieldName != "level" || decoder.Decode(&detectedLevel) != nil || decoder.More() {
		return "", &LLMProviderError{Code: "LLM_INVALID_RESPONSE"}
	}
	closingToken, err := decoder.Token()
	if err != nil || closingToken != json.Delim('}') || decoder.Decode(new(any)) != io.EOF {
		return "", &LLMProviderError{Code: "LLM_INVALID_RESPONSE"}
	}
	// Схема в инструкции помогает модели, но не заменяет проверку ответа кодом.
	switch detectedLevel {
	case models.LevelA1, models.LevelA2, models.LevelB1, models.LevelB2, models.LevelC1, models.LevelC2:
		return detectedLevel, nil
	default:
		return "", &LLMProviderError{Code: "LLM_INVALID_RESPONSE"}
	}
}
