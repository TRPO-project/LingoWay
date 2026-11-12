package generation

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"lingoway/internal/models"
)

const (
	minGeneratedExerciseCount  = 2
	maxGeneratedExerciseCount  = 30
	generatedExerciseTypeCount = 7
)

// GeneratedExercise содержит проверенное задание для записи в БД.
// AnswerKey и Explanation относятся к внутреннему формату задания.
type GeneratedExercise struct {
	Type         models.ExerciseType `json:"type"`
	Instruction  string              `json:"instruction"`
	QuestionText string              `json:"question_text"`
	Content      models.JSON         `json:"content"`
	AnswerKey    models.JSON         `json:"answer_key"`
	Explanation  string              `json:"explanation"`
}

// PlanExerciseCounts распределяет задания по типам в порядке запроса.
// Первые типы получают по одному заданию из остатка от деления.
func PlanExerciseCounts(count int, types []models.ExerciseType) ([]int, error) {
	if count < minGeneratedExerciseCount || count > maxGeneratedExerciseCount {
		return nil, errors.New("exercise count must be between 2 and 30")
	}
	if len(types) == 0 || len(types) > generatedExerciseTypeCount || len(types) > count {
		return nil, errors.New("exercise types must contain 1 to 7 supported types and fit the exercise count")
	}
	counts := make([]int, len(types))
	seen := make(map[models.ExerciseType]bool, len(types))
	for index, exerciseType := range types {
		if generatedExerciseFormat(exerciseType) == "" || seen[exerciseType] {
			return nil, errors.New("exercise types must be supported and unique")
		}
		seen[exerciseType] = true
		counts[index] = count / len(types)
		if index < count%len(types) {
			counts[index]++
		}
	}
	return counts, nil
}

func generatedExerciseFormat(exerciseType models.ExerciseType) string {
	switch exerciseType {
	case models.ExerciseVocabularyGap, models.ExerciseVocabularyContextChoice,
		models.ExerciseGrammarBrackets, models.ExerciseGrammarTense:
		return "single_choice"
	case models.ExerciseVocabularyMatchingTranslation, models.ExerciseVocabularyMatchingDefinition:
		return "matching"
	case models.ExerciseGrammarWordOrder:
		return "word_order"
	default:
		return ""
	}
}

// parseGeneratedExercisesJSON сверяет результат с планом, проверенным до вызова модели.
// Группы идут в порядке types, порядок внутри каждого типа задаёт модель.
func parseGeneratedExercisesJSON(completionContent string, types []models.ExerciseType, counts []int, total int) ([]GeneratedExercise, error) {
	invalidResponse := &LLMProviderError{Code: "LLM_INVALID_RESPONSE"}
	if !utf8.ValidString(completionContent) {
		return nil, invalidResponse
	}
	root, ok := generatedObjectFields([]byte(completionContent), []string{"exercises"})
	if !ok {
		return nil, invalidResponse
	}
	var rawExercises []json.RawMessage
	if json.Unmarshal(root["exercises"], &rawExercises) != nil || len(rawExercises) != total {
		return nil, invalidResponse
	}

	groupIndices := make(map[models.ExerciseType]int, len(types))
	groups := make([][]GeneratedExercise, len(types))
	for index, exerciseType := range types {
		groupIndices[exerciseType] = index
		groups[index] = make([]GeneratedExercise, 0, counts[index])
	}
	for _, rawExercise := range rawExercises {
		exercise, ok := parseGeneratedExercise(rawExercise)
		if !ok {
			return nil, invalidResponse
		}
		index, requested := groupIndices[exercise.Type]
		if !requested || len(groups[index]) >= counts[index] {
			return nil, invalidResponse
		}
		groups[index] = append(groups[index], exercise)
	}
	result := make([]GeneratedExercise, 0, total)
	for _, group := range groups {
		result = append(result, group...)
	}
	return result, nil
}

func parseGeneratedExercise(raw json.RawMessage) (GeneratedExercise, bool) {
	fields, ok := generatedObjectFields(raw, []string{"type", "instruction", "question_text", "content", "answer_key", "explanation"})
	if !ok {
		return GeneratedExercise{}, false
	}
	exerciseType, typeOK := generatedString(fields["type"])
	instruction, instructionOK := generatedString(fields["instruction"])
	questionText, questionOK := generatedString(fields["question_text"])
	explanation, explanationOK := generatedString(fields["explanation"])
	if !typeOK || !instructionOK || !questionOK || !explanationOK {
		return GeneratedExercise{}, false
	}

	var content, answerKey map[string]any
	switch generatedExerciseFormat(models.ExerciseType(exerciseType)) {
	case "single_choice":
		content, answerKey, ok = parseGeneratedChoice(fields["content"], fields["answer_key"])
	case "matching":
		content, answerKey, ok = parseGeneratedMatching(fields["content"], fields["answer_key"])
	case "word_order":
		content, answerKey, ok = parseGeneratedWordOrder(fields["content"], fields["answer_key"])
	default:
		return GeneratedExercise{}, false
	}
	if !ok {
		return GeneratedExercise{}, false
	}
	// Пересобираем JSON из проверенных полей. Публичное content содержит
	// только поля выбранного формата, включая вложенные элементы.
	contentJSON, err := json.Marshal(content)
	if err != nil {
		return GeneratedExercise{}, false
	}
	answerKeyJSON, err := json.Marshal(answerKey)
	if err != nil {
		return GeneratedExercise{}, false
	}
	return GeneratedExercise{
		Type:         models.ExerciseType(exerciseType),
		Instruction:  instruction,
		QuestionText: questionText,
		Content:      models.JSON(contentJSON),
		AnswerKey:    models.JSON(answerKeyJSON),
		Explanation:  explanation,
	}, true
}

// generatedObjectFields проверяет точные имена обязательных и разрешённых полей.
func generatedObjectFields(raw []byte, required []string, optional ...string) (map[string]json.RawMessage, bool) {
	fields, ok := readGeneratedJSONObject(raw)
	if !ok {
		return nil, false
	}
	for _, field := range required {
		if _, exists := fields[field]; !exists {
			return nil, false
		}
	}
	for field := range fields {
		if !slices.Contains(required, field) && !slices.Contains(optional, field) {
			return nil, false
		}
	}
	return fields, true
}

// readGeneratedJSONObject читает ровно один объект и замечает повтор ключа.
// Обычный json.Unmarshal в map оставляет только последнее значение такого ключа.
func readGeneratedJSONObject(raw []byte) (map[string]json.RawMessage, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, false
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		name, err := decoder.Token()
		field, ok := name.(string)
		if err != nil || !ok {
			return nil, false
		}
		if _, duplicate := fields[field]; duplicate {
			return nil, false
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, false
		}
		fields[field] = value
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || decoder.Decode(new(any)) != io.EOF {
		return nil, false
	}
	return fields, true
}

func generatedString(raw json.RawMessage) (string, bool) {
	var value string
	// PostgreSQL отклоняет нулевой символ как в тексте, так и внутри JSONB.
	if json.Unmarshal(raw, &value) != nil || strings.TrimSpace(value) == "" || strings.ContainsRune(value, 0) {
		return "", false
	}
	return value, true
}
