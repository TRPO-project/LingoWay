package materials

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// MaxTextImportFileBytes ограничивает TXT, Markdown или сжатый DOCX до 2 МиБ.
const MaxTextImportFileBytes int64 = 2 << 20

// ExtractTextFromFile читает ограниченный по размеру TXT/Markdown или DOCX и возвращает текст
// после NormalizeAndValidateText. Расширение выбирает способ разбора, а для DOCX дополнительно
// проверяется структура ZIP и XML. Содержимое архива на диск не распаковывается.
// Внешние связи DOCX и команды полей не исполняются; исходный файл не сохраняется.
func ExtractTextFromFile(originalFilename string, uploadedFileReader io.Reader) (string, error) {
	fileExtension := strings.ToLower(filepath.Ext(originalFilename))
	if fileExtension != ".txt" && fileExtension != ".md" && fileExtension != ".docx" {
		return "", ErrUnsupportedTextFileFormat
	}
	if uploadedFileReader == nil {
		return "", ErrInvalidTextFile
	}
	// Один дополнительный байт отличает файл ровно на лимите от слишком большого,
	// не требуя читать весь потенциально неограниченный входной поток.
	uploadedFileBytes, err := io.ReadAll(io.LimitReader(uploadedFileReader, MaxTextImportFileBytes+1))
	if err != nil {
		return "", fmt.Errorf("read import: %w", ErrInvalidTextFile)
	}
	if int64(len(uploadedFileBytes)) > MaxTextImportFileBytes {
		return "", ErrMaterialSizeLimitExceeded
	}
	if fileExtension != ".docx" {
		return NormalizeAndValidateText(string(uploadedFileBytes))
	}
	return extractTextFromDOCX(uploadedFileBytes)
}
