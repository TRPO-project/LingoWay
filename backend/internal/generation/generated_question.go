package generation

import (
	"encoding/json"
	"strings"
)

// Ученик видит вопрос, цитату из материала и, при наличии, слова для ответа.
// Критерии и пример ответа хранятся отдельно в приватном ключе.
func parseGeneratedQuestion(rawContent, rawKey json.RawMessage, sourceText string) (content, answerKey map[string]any, ok bool) {
	fields, ok := generatedObjectFields(rawContent, []string{"format", "context"}, "vocabulary")
	if !ok {
		return nil, nil, false
	}
	format, _ := generatedString(fields["format"])
	excerpt, ok := generatedString(fields["context"])
	if format != "free_text" || !ok || !strings.Contains(sourceText, excerpt) {
		return nil, nil, false
	}
	content = map[string]any{"format": format, "context": excerpt}
	if rawVocabulary, present := fields["vocabulary"]; present {
		vocabulary, ok := parseQuestionStrings(rawVocabulary, 5, 5)
		if !ok {
			return nil, nil, false
		}
		seen := make(map[string]bool, len(vocabulary))
		for _, word := range vocabulary {
			normalized := strings.ToLower(strings.Join(strings.Fields(word), " "))
			if seen[normalized] {
				return nil, nil, false
			}
			seen[normalized] = true
		}
		content["vocabulary"] = vocabulary
	}
	keyFields, ok := generatedObjectFields(rawKey, []string{"rubric"})
	if !ok {
		return nil, nil, false
	}
	rubricFields, ok := generatedObjectFields(keyFields["rubric"], []string{"criteria"}, "reference_answer")
	if !ok {
		return nil, nil, false
	}
	criteria, ok := parseQuestionStrings(rubricFields["criteria"], 1, 10)
	if !ok {
		return nil, nil, false
	}
	rubric := map[string]any{"criteria": criteria}
	if rawReference, present := rubricFields["reference_answer"]; present {
		reference, ok := generatedString(rawReference)
		if !ok {
			return nil, nil, false
		}
		rubric["reference_answer"] = reference
	}
	return content, map[string]any{"rubric": rubric}, true
}

// Проверяет количество элементов и применяет к каждому те же правила,
// что к остальным текстовым полям упражнений.
func parseQuestionStrings(raw json.RawMessage, minCount, maxCount int) ([]string, bool) {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || len(items) < minCount || len(items) > maxCount {
		return nil, false
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		value, ok := generatedString(item)
		if !ok {
			return nil, false
		}
		values = append(values, value)
	}
	return values, true
}
