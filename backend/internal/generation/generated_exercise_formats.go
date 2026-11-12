package generation

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

type generatedExerciseItem struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type generatedMatchingPair struct {
	LeftID  string `json:"left_id"`
	RightID string `json:"right_id"`
}

func parseGeneratedChoice(rawContent, rawKey json.RawMessage) (content, answerKey map[string]any, ok bool) {
	contentFields, ok := generatedObjectFields(rawContent, []string{"format", "options"})
	format, _ := generatedString(contentFields["format"])
	if !ok || format != "single_choice" {
		return nil, nil, false
	}
	options, optionTexts, ok := parseGeneratedItems(contentFields["options"], 4, 4, true)
	if !ok {
		return nil, nil, false
	}
	keyFields, ok := generatedObjectFields(rawKey, []string{"correct_choice_id"}, "distractor_types")
	if !ok {
		return nil, nil, false
	}
	correctChoiceID, ok := generatedString(keyFields["correct_choice_id"])
	if !ok || optionTexts[correctChoiceID] == "" {
		return nil, nil, false
	}
	answerKey = map[string]any{"correct_choice_id": correctChoiceID}
	if rawDistractors, present := keyFields["distractor_types"]; present {
		distractorFields, ok := readGeneratedJSONObject(rawDistractors)
		if !ok {
			return nil, nil, false
		}
		distractors := make(map[string]string, len(distractorFields))
		for optionID, rawDescription := range distractorFields {
			description, ok := generatedString(rawDescription)
			if !ok || optionTexts[optionID] == "" || optionID == correctChoiceID {
				return nil, nil, false
			}
			distractors[optionID] = description
		}
		answerKey["distractor_types"] = distractors
	}
	return map[string]any{"format": format, "options": options}, answerKey, true
}

func parseGeneratedMatching(rawContent, rawKey json.RawMessage) (content, answerKey map[string]any, ok bool) {
	contentFields, ok := generatedObjectFields(rawContent, []string{"format", "left_items", "right_items"})
	format, _ := generatedString(contentFields["format"])
	if !ok || format != "matching" {
		return nil, nil, false
	}
	leftItems, leftTexts, ok := parseGeneratedItems(contentFields["left_items"], 4, 6, true)
	if !ok {
		return nil, nil, false
	}
	rightItems, rightTexts, ok := parseGeneratedItems(contentFields["right_items"], 4, 6, true)
	if !ok || len(leftItems) != len(rightItems) {
		return nil, nil, false
	}
	for _, rightItem := range rightItems {
		if leftTexts[rightItem.ID] != "" {
			return nil, nil, false
		}
	}
	keyFields, ok := generatedObjectFields(rawKey, []string{"pairs"})
	if !ok {
		return nil, nil, false
	}
	var rawPairs []json.RawMessage
	if json.Unmarshal(keyFields["pairs"], &rawPairs) != nil || len(rawPairs) != len(leftItems) {
		return nil, nil, false
	}
	pairs := make([]generatedMatchingPair, 0, len(rawPairs))
	used := make(map[string]bool, len(leftItems)+len(rightItems))
	for _, rawPair := range rawPairs {
		pairFields, ok := generatedObjectFields(rawPair, []string{"left_id", "right_id"})
		if !ok {
			return nil, nil, false
		}
		leftID, leftOK := generatedString(pairFields["left_id"])
		rightID, rightOK := generatedString(pairFields["right_id"])
		if !leftOK || !rightOK || leftTexts[leftID] == "" || rightTexts[rightID] == "" || used[leftID] || used[rightID] {
			return nil, nil, false
		}
		used[leftID] = true
		used[rightID] = true
		pairs = append(pairs, generatedMatchingPair{LeftID: leftID, RightID: rightID})
	}
	return map[string]any{"format": format, "left_items": leftItems, "right_items": rightItems},
		map[string]any{"pairs": pairs}, true
}

func parseGeneratedWordOrder(rawContent, rawKey json.RawMessage) (content, answerKey map[string]any, ok bool) {
	contentFields, ok := generatedObjectFields(rawContent, []string{"format", "source_text", "target_language", "word_bank"})
	format, _ := generatedString(contentFields["format"])
	targetLanguage, _ := generatedString(contentFields["target_language"])
	sourceText, sourceOK := generatedString(contentFields["source_text"])
	if !ok || !sourceOK || format != "word_order" || targetLanguage != "en" {
		return nil, nil, false
	}
	// В предложении слова могут повторяться. Разные токены такого слова
	// должны иметь разные ID, но их текст совпадает.
	wordBank, words, ok := parseGeneratedItems(contentFields["word_bank"], 2, 40, false)
	if !ok {
		return nil, nil, false
	}
	keyFields, ok := generatedObjectFields(rawKey, []string{"token_ids"}, "correct_translation")
	if !ok {
		return nil, nil, false
	}
	var tokenIDs []string
	if json.Unmarshal(keyFields["token_ids"], &tokenIDs) != nil || len(tokenIDs) < 2 || len(tokenIDs) > len(wordBank) {
		return nil, nil, false
	}
	used := make(map[string]bool, len(tokenIDs))
	orderedWords := make([]string, 0, len(tokenIDs))
	for _, tokenID := range tokenIDs {
		word := words[tokenID]
		if word == "" || used[tokenID] {
			return nil, nil, false
		}
		used[tokenID] = true
		orderedWords = append(orderedWords, word)
	}
	answerKey = map[string]any{"token_ids": tokenIDs}
	if rawTranslation, present := keyFields["correct_translation"]; present {
		translation, ok := generatedString(rawTranslation)
		if !ok || !generatedTranslationMatches(translation, orderedWords) {
			return nil, nil, false
		}
		answerKey["correct_translation"] = translation
	}
	return map[string]any{"format": format, "source_text": sourceText, "target_language": targetLanguage, "word_bank": wordBank}, answerKey, true
}

// parseGeneratedItems проверяет ограничения массива и возвращает текст по ID.
// Совпадающий видимый текст допустим для разных токенов word_order.
func parseGeneratedItems(raw json.RawMessage, minCount, maxCount int, uniqueText bool) ([]generatedExerciseItem, map[string]string, bool) {
	var rawItems []json.RawMessage
	if json.Unmarshal(raw, &rawItems) != nil || len(rawItems) < minCount || len(rawItems) > maxCount {
		return nil, nil, false
	}
	items := make([]generatedExerciseItem, 0, len(rawItems))
	textsByID := make(map[string]string, len(rawItems))
	seenTexts := make(map[string]bool, len(rawItems))
	for _, rawItem := range rawItems {
		itemFields, ok := generatedObjectFields(rawItem, []string{"id", "text"})
		if !ok {
			return nil, nil, false
		}
		id, idOK := generatedString(itemFields["id"])
		text, textOK := generatedString(itemFields["text"])
		if !idOK || !textOK || strings.TrimSpace(id) != id || utf8.RuneCountInString(id) > 64 || textsByID[id] != "" {
			return nil, nil, false
		}
		if uniqueText {
			visibleText := strings.ToLower(strings.Join(strings.Fields(text), " "))
			if seenTexts[visibleText] {
				return nil, nil, false
			}
			seenTexts[visibleText] = true
		}
		textsByID[id] = text
		items = append(items, generatedExerciseItem{ID: id, Text: text})
	}
	return items, textsByID, true
}

var generatedPunctuationSpacing = strings.NewReplacer(
	" ,", ",", " .", ".", " !", "!", " ?", "?", " :", ":", " ;", ";", " )", ")", "( ", "(",
)

func generatedTranslationMatches(translation string, orderedWords []string) bool {
	translation = generatedPunctuationSpacing.Replace(strings.Join(strings.Fields(translation), " "))
	ordered := generatedPunctuationSpacing.Replace(strings.Join(strings.Fields(strings.Join(orderedWords, " ")), " "))
	if translation == ordered {
		return true
	}
	// В примерах схемы завершающий знак не обязательно входит в набор токенов.
	return !strings.HasSuffix(ordered, ".") && !strings.HasSuffix(ordered, "!") && !strings.HasSuffix(ordered, "?") &&
		(translation == ordered+"." || translation == ordered+"!" || translation == ordered+"?")
}
