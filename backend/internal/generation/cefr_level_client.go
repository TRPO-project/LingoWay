package generation

import (
	"context"
	_ "embed"

	"lingoway/internal/models"
)

const (
	cefrDetectionPromptVersionV1 = "prepare-material-v1"
	cefrDetectionPromptVersionV2 = "prepare-material-v2"
	// CEFRDetectionPromptVersion записывается в новые запросы очереди.
	CEFRDetectionPromptVersion = cefrDetectionPromptVersionV2
	// Для одного уровня CEFR достаточно короткого ответа.
	maxCEFRResponseBytes = 64 << 10
)

//go:embed prompts/prepare-material-v1.txt
var cefrDetectionPromptV1 string

//go:embed prompts/prepare-material-v2.txt
var cefrDetectionPromptV2 string

//go:embed schemas/prepare-material-v1.json
var cefrDetectionSchema string

// DetectCEFRLevel определяет сложность английского текста по шкале CEFR.
// Версия из запроса очереди задаёт инструкцию, включая ранее поставленные работы.
func (client *ChatCompletionClient) DetectCEFRLevel(ctx context.Context, sourceText, promptVersion string) (models.CEFRLevel, error) {
	var detectionPrompt string
	switch promptVersion {
	case cefrDetectionPromptVersionV1:
		detectionPrompt = cefrDetectionPromptV1
	case cefrDetectionPromptVersionV2:
		detectionPrompt = cefrDetectionPromptV2
	default:
		return "", &LLMProviderError{Code: "LLM_PROMPT_UNSUPPORTED"}
	}
	instruction := detectionPrompt + "\nExpected JSON schema:\n" + cefrDetectionSchema
	completionContent, err := client.requestJSONCompletion(ctx, instruction, sourceText, maxCEFRResponseBytes)
	if err != nil {
		return "", err
	}
	return parseCEFRLevelJSON(completionContent)
}
