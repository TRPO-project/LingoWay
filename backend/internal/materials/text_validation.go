package materials

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxMaterialTextCharacters ограничивает нормализованный материал в символах Unicode.
// Весь принятый текст передаётся модели; усечения до этого предела не происходит.
const MaxMaterialTextCharacters = 20000

var (
	// ErrInvalidMaterialText означает пустой текст, неверный UTF-8 или запрещённые управляющие символы.
	ErrInvalidMaterialText = errors.New("material must contain nonempty valid UTF-8 text without control characters")
	// ErrMaterialSizeLimitExceeded означает превышение любого лимита: файла, распакованного XML или текста.
	ErrMaterialSizeLimitExceeded = errors.New("material exceeds the import limit")
	// ErrUnsupportedTextFileFormat возвращается до чтения файла с неподдерживаемым расширением.
	ErrUnsupportedTextFileFormat = errors.New("supported import formats are TXT, MD and DOCX")
	// ErrInvalidTextFile означает ошибку чтения либо повреждённую структуру ZIP/XML.
	// Все эти ошибки можно распознавать через errors.Is, не разбирая их текст.
	ErrInvalidTextFile = errors.New("invalid material file")
)

// NormalizeAndValidateText удаляет начальный BOM, заменяет CRLF на LF и убирает внешние пробелы.
// Остальной текст сохраняется целиком; превышение MaxMaterialTextCharacters вызывает ошибку,
// а не усечение. Переводы строк и табуляция разрешены, прочие управляющие символы — нет.
func NormalizeAndValidateText(materialText string) (string, error) {
	if !utf8.ValidString(materialText) {
		return "", ErrInvalidMaterialText
	}
	materialText = strings.TrimPrefix(materialText, "\ufeff")
	materialText = strings.ReplaceAll(materialText, "\r\n", "\n")
	// Проверка идёт до TrimSpace, чтобы запрещённый символ на краю нельзя было
	// скрыть нормализацией. Например, отдельный CR не считается переводом строки LF.
	for _, character := range materialText {
		if unicode.IsControl(character) && character != '\n' && character != '\t' {
			return "", ErrInvalidMaterialText
		}
	}
	materialText = strings.TrimSpace(materialText)
	if materialText == "" {
		return "", ErrInvalidMaterialText
	}
	if utf8.RuneCountInString(materialText) > MaxMaterialTextCharacters {
		return "", ErrMaterialSizeLimitExceeded
	}
	return materialText, nil
}
