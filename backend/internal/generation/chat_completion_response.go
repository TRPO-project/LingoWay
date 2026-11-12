package generation

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"lingoway/internal/models"
)

// Ограничиваем подсказку провайдера сутками, чтобы ошибочный заголовок
// не отложил повтор на неопределённо долгий срок.
const maxProviderRetryDelay = 24 * time.Hour

// readChatCompletionContent ограничивает весь HTTP-ответ и извлекает текст
// модели. Форму результата проверяет конкретная операция.
func readChatCompletionContent(ctx context.Context, httpResponse *http.Response, responseLimit int64) (string, error) {
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		// Тело ошибки не читаем: оно может повторять материал или служебные данные.
		providerError := classifyProviderHTTPStatus(httpResponse.StatusCode)
		if providerError.Retryable {
			providerError.RetryAfter = parseProviderRetryAfter(httpResponse.Header.Get("Retry-After"), time.Now())
		}
		return "", providerError
	}
	// Дополнительный байт отличает ответ ровно на границе лимита от слишком
	// большого ответа, который иначе выглядел бы просто обрезанным.
	responseBody, err := io.ReadAll(io.LimitReader(httpResponse.Body, responseLimit+1))
	if err != nil {
		return "", classifyProviderTransportError(ctx, err)
	}
	if int64(len(responseBody)) > responseLimit {
		return "", &LLMProviderError{Code: "LLM_INVALID_RESPONSE"}
	}
	return parseChatCompletionContent(responseBody)
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

func parseProviderRetryAfter(headerValue string, now time.Time) time.Duration {
	headerValue = strings.Trim(headerValue, " \t")
	if headerValue == "" {
		return 0
	}
	// Допустимые HTTP-даты начинаются с дня недели, а задержка в секундах
	// начинается с цифры и состоит только из цифр ASCII.
	if headerValue[0] < '0' || headerValue[0] > '9' {
		retryTime, err := http.ParseTime(headerValue)
		if err != nil || !retryTime.After(now) {
			return 0
		}
		return min(retryTime.Sub(now), maxProviderRetryDelay)
	}
	const maxSeconds = int(maxProviderRetryDelay / time.Second)
	seconds := 0
	for index := range len(headerValue) {
		digit := headerValue[index]
		if digit < '0' || digit > '9' {
			return 0
		}
		// Ограничиваем число на каждом шаге, чтобы не переполнить его даже
		// на очень длинном значении. Оставшиеся символы всё равно проверяем.
		seconds = min(seconds*10+int(digit-'0'), maxSeconds)
	}
	return time.Duration(seconds) * time.Second
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
	if !isCEFRLevel(detectedLevel) {
		return "", &LLMProviderError{Code: "LLM_INVALID_RESPONSE"}
	}
	return detectedLevel, nil
}

func isCEFRLevel(level models.CEFRLevel) bool {
	switch level {
	case models.LevelA1, models.LevelA2, models.LevelB1, models.LevelB2, models.LevelC1, models.LevelC2:
		return true
	default:
		return false
	}
}
