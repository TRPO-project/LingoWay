package models

// CEFRLevel — уровень английского языка для материала и сложности модуля.
type CEFRLevel string

const (
	LevelA1 CEFRLevel = "A1"
	LevelA2 CEFRLevel = "A2"
	LevelB1 CEFRLevel = "B1"
	LevelB2 CEFRLevel = "B2"
	LevelC1 CEFRLevel = "C1"
	LevelC2 CEFRLevel = "C2"
)

// MaterialKind определяет источник; набор допустимых source-полей зависит от него.
type MaterialKind string

const (
	MaterialText  MaterialKind = "text"
	MaterialFile  MaterialKind = "file"
	MaterialURL   MaterialKind = "url"
	MaterialAudio MaterialKind = "audio"
)

// MaterialStatus относится к извлечению и подготовке текста исходного материала.
type MaterialStatus string

const (
	MaterialPending MaterialStatus = "pending"
	MaterialReady   MaterialStatus = "ready"
	MaterialFailed  MaterialStatus = "failed"
)

// ModuleVisibility определяет доступ: личный модуль или общий каталог.
type ModuleVisibility string

const (
	VisibilityPrivate ModuleVisibility = "private"
	VisibilityPublic  ModuleVisibility = "public"
)

// ModuleStatus описывает подготовку модуля; ready означает опубликованный модуль.
type ModuleStatus string

const (
	ModuleDraft      ModuleStatus = "draft"
	ModuleGenerating ModuleStatus = "generating"
	ModuleReady      ModuleStatus = "ready"
	ModuleFailed     ModuleStatus = "failed"
)

// ExerciseType выбирает схему content/answer_key из exercise-templates.json.
type ExerciseType string

const (
	ExerciseVocabularyGap                 ExerciseType = "vocabulary_gap"
	ExerciseVocabularyMatchingTranslation ExerciseType = "vocabulary_matching_translation"
	ExerciseVocabularyMatchingDefinition  ExerciseType = "vocabulary_matching_definition"
	ExerciseVocabularyContextChoice       ExerciseType = "vocabulary_context_choice"
	ExerciseGrammarBrackets               ExerciseType = "grammar_brackets"
	ExerciseGrammarTense                  ExerciseType = "grammar_tense"
	ExerciseGrammarWordOrder              ExerciseType = "grammar_word_order"
	ExerciseComprehensionQuestion         ExerciseType = "comprehension_question"
	ExerciseReasoningQuestion             ExerciseType = "reasoning_question"
)

// AnswerStatus отделяет отправленный ответ от результата проверки.
type AnswerStatus string

const (
	AnswerPending AnswerStatus = "pending"
	AnswerChecked AnswerStatus = "checked"
	AnswerFailed  AnswerStatus = "failed"
)

// LLMOperation задаёт единственную целевую сущность каждого запроса к LLM.
type LLMOperation string

const (
	LLMPrepareMaterial LLMOperation = "prepare_material"
	LLMGenerateModule  LLMOperation = "generate_module"
	LLMCheckAnswer     LLMOperation = "check_answer"
)

// LLMStatus — состояние задания в общей очереди LLM.
type LLMStatus string

const (
	LLMQueued    LLMStatus = "queued"
	LLMRunning   LLMStatus = "running"
	LLMSucceeded LLMStatus = "succeeded"
	LLMFailed    LLMStatus = "failed"
)

// CatalogMaterialType — вид материала каталога: текст на сайте или ссылка на внешний сайт.
type CatalogMaterialType string

const (
	CatalogText CatalogMaterialType = "text"
	CatalogLink CatalogMaterialType = "link"
)
