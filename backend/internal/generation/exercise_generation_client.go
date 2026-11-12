package generation

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"lingoway/internal/models"
)

const (
	// ExerciseGenerationPromptV1 отмечает модули, созданные в общем режиме по первой инструкции.
	ExerciseGenerationPromptV1 = "generate-module-v1"
	// ExerciseGenerationPromptVersion записывается в новые запросы очереди.
	ExerciseGenerationPromptVersion = ExerciseGenerationPromptV1
)

// Предел относится к полному HTTP-ответу с несколькими заданиями.
const maxExerciseGenerationResponseBytes = 512 << 10

//go:embed prompts/generate-module-v1.txt
var exerciseGenerationPromptV1 string

// ExerciseGenerationInput описывает подготовленный материал, уровень и план заданий.
type ExerciseGenerationInput struct {
	SourceText string
	Difficulty models.CEFRLevel
	Count      int
	Types      []models.ExerciseType
}

// GenerateExercises возвращает проверенный набор закрытых заданий.
// Все типы и количества передаются в инструкции приложения, отдельно от материала.
func (client *ChatCompletionClient) GenerateExercises(ctx context.Context, input ExerciseGenerationInput, promptVersion string) ([]GeneratedExercise, error) {
	if promptVersion != ExerciseGenerationPromptV1 {
		return nil, &LLMProviderError{Code: "LLM_PROMPT_UNSUPPORTED"}
	}
	counts, err := PlanExerciseCounts(input.Count, input.Types)
	if err != nil || !isCEFRLevel(input.Difficulty) {
		return nil, &LLMProviderError{Code: "LLM_INVALID_INPUT"}
	}

	var instruction strings.Builder
	instruction.WriteString(exerciseGenerationPromptV1)
	fmt.Fprintf(&instruction, "\nCEFR level: %s\nTotal exercises: %d\nTypes and counts, in order:\n", input.Difficulty, input.Count)
	for index, exerciseType := range input.Types {
		fmt.Fprintf(&instruction, "- %s: %d\n", exerciseType, counts[index])
	}
	completionContent, err := client.requestJSONCompletion(ctx, instruction.String(), input.SourceText, maxExerciseGenerationResponseBytes)
	if err != nil {
		return nil, err
	}
	return parseGeneratedExercisesJSON(completionContent, input.Types, counts, input.Count)
}
