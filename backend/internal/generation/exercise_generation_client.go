package generation

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"lingoway/internal/models"
)

const (
	// ExerciseGenerationPromptV1 поддерживает семь лексических и грамматических типов.
	ExerciseGenerationPromptV1 = "generate-module-v1"
	// ExerciseGenerationPromptV2 добавляет вопросы на понимание и рассуждение.
	ExerciseGenerationPromptV2 = "generate-module-v2"
	// ExerciseGenerationPromptVersion записывается в новые запросы очереди.
	ExerciseGenerationPromptVersion = ExerciseGenerationPromptV2
)

// Предел относится к полному HTTP-ответу с несколькими заданиями.
const maxExerciseGenerationResponseBytes = 512 << 10

//go:embed prompts/generate-module-v1.txt
var exerciseGenerationPromptV1 string

//go:embed prompts/generate-module-v2.txt
var exerciseGenerationPromptV2 string

// SupportsExerciseGenerationPrompt сообщает, может ли этот клиент обработать
// версию инструкции, сохранённую при создании запроса.
func SupportsExerciseGenerationPrompt(version string) bool {
	return exerciseGenerationPrompt(version) != ""
}

func exerciseGenerationPrompt(version string) string {
	switch version {
	case ExerciseGenerationPromptV1:
		return exerciseGenerationPromptV1
	case ExerciseGenerationPromptV2:
		return exerciseGenerationPromptV2
	default:
		return ""
	}
}

// ExerciseGenerationInput описывает подготовленный материал, уровень и план заданий.
type ExerciseGenerationInput struct {
	SourceText string
	Difficulty models.CEFRLevel
	Count      int
	Types      []models.ExerciseType
}

// GenerateExercises возвращает проверенный набор заданий.
// Все типы и количества передаются в инструкции приложения, отдельно от материала.
func (client *ChatCompletionClient) GenerateExercises(ctx context.Context, input ExerciseGenerationInput, promptVersion string) ([]GeneratedExercise, error) {
	prompt := exerciseGenerationPrompt(promptVersion)
	if prompt == "" {
		return nil, &LLMProviderError{Code: "LLM_PROMPT_UNSUPPORTED"}
	}
	counts, err := PlanExerciseCounts(input.Count, input.Types)
	if err != nil || !isCEFRLevel(input.Difficulty) {
		return nil, &LLMProviderError{Code: "LLM_INVALID_INPUT"}
	}
	if promptVersion == ExerciseGenerationPromptV1 {
		for _, exerciseType := range input.Types {
			if generatedExerciseFormat(exerciseType) == "free_text" {
				return nil, &LLMProviderError{Code: "LLM_INVALID_INPUT"}
			}
		}
	}

	var instruction strings.Builder
	instruction.WriteString(prompt)
	fmt.Fprintf(&instruction, "\nCEFR level: %s\nTotal exercises: %d\nTypes and counts, in order:\n", input.Difficulty, input.Count)
	for index, exerciseType := range input.Types {
		fmt.Fprintf(&instruction, "- %s: %d\n", exerciseType, counts[index])
	}
	completionContent, err := client.requestJSONCompletion(ctx, instruction.String(), input.SourceText, maxExerciseGenerationResponseBytes)
	if err != nil {
		return nil, err
	}
	return parseGeneratedExercisesJSON(completionContent, input, counts)
}
