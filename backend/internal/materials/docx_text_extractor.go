package materials

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"unicode/utf8"
)

// Ограничения распакованного DOCX не позволяют небольшому ZIP занять чрезмерную память.
const (
	maxDOCXDocumentXMLBytes = 8 << 20
	maxDOCXArchiveEntries   = 1024
)

// extractTextFromDOCX берёт только основной текст word/document.xml. Изображения, стили,
// колонтитулы и внешние связи не нужны для первого этапа текстового импорта.
func extractTextFromDOCX(uploadedDOCXBytes []byte) (string, error) {
	docxArchive, err := zip.NewReader(bytes.NewReader(uploadedDOCXBytes), int64(len(uploadedDOCXBytes)))
	if err != nil || len(docxArchive.File) > maxDOCXArchiveEntries {
		return "", ErrInvalidTextFile
	}
	var mainDocumentEntry *zip.File
	for _, archiveEntry := range docxArchive.File {
		// Бит 0 в ZIP означает шифрование: зашифрованные записи здесь не поддерживаются.
		if archiveEntry.Flags&1 != 0 {
			return "", ErrInvalidTextFile
		}
		if archiveEntry.Name == "word/document.xml" {
			// Несколько записей с одинаковым именем сделали бы выбор источника неоднозначным.
			if mainDocumentEntry != nil {
				return "", ErrInvalidTextFile
			}
			mainDocumentEntry = archiveEntry
		}
	}
	if mainDocumentEntry == nil {
		return "", ErrInvalidTextFile
	}
	if mainDocumentEntry.UncompressedSize64 > maxDOCXDocumentXMLBytes {
		return "", ErrMaterialSizeLimitExceeded
	}
	documentXMLReader, err := mainDocumentEntry.Open()
	if err != nil {
		return "", ErrInvalidTextFile
	}
	defer documentXMLReader.Close()
	// Проверяем и заявленный размер выше, и фактически прочитанный размер здесь:
	// нельзя полагаться только на метаданные присланного архива.
	documentXMLBytes, err := io.ReadAll(io.LimitReader(documentXMLReader, maxDOCXDocumentXMLBytes+1))
	if err != nil {
		return "", ErrInvalidTextFile
	}
	if len(documentXMLBytes) > maxDOCXDocumentXMLBytes {
		return "", ErrMaterialSizeLimitExceeded
	}
	return extractTextFromWordDocumentXML(documentXMLBytes)
}

// isWordprocessingElement распознаёт элементы Word в обычном и строгом стандарте OOXML.
// Проверяем URI пространства имён, а не префикс w: — отправитель может его переименовать.
func isWordprocessingElement(elementName xml.Name, localName string) bool {
	return elementName.Local == localName && (elementName.Space == "http://schemas.openxmlformats.org/wordprocessingml/2006/main" ||
		elementName.Space == "http://purl.oclc.org/ooxml/wordprocessingml/main")
}

// extractTextFromWordDocumentXML читает XML по токенам и собирает только текст w:t внутри w:body.
// Абзацы, разрывы строк и ячейки таблиц получают явные разделители, чтобы соседние
// фрагменты не склеились. Инструкции полей w:instrText и удалённый w:delText игнорируются.
func extractTextFromWordDocumentXML(documentXMLBytes []byte) (string, error) {
	documentXMLDecoder := xml.NewDecoder(bytes.NewReader(bytes.TrimPrefix(documentXMLBytes, []byte("\xef\xbb\xbf"))))
	var extractedTextBytes []byte
	// Глубины отмечают область основного документа и текущий текстовый элемент;
	// флаги не позволяют принять несколько корней или повторное тело документа.
	elementDepth, documentBodyDepth, textElementDepth := 0, 0, 0
	documentRootFound, documentBodyFound := false, false
	for {
		token, err := documentXMLDecoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", ErrInvalidTextFile
		}
		switch token := token.(type) {
		case xml.Directive:
			// DTD и объявления сущностей не нужны для текста Word и не принимаются.
			return "", ErrInvalidTextFile
		case xml.StartElement:
			if elementDepth == 0 {
				if documentRootFound || !isWordprocessingElement(token.Name, "document") {
					return "", ErrInvalidTextFile
				}
				documentRootFound = true
			}
			elementDepth++
			if elementDepth == 2 && isWordprocessingElement(token.Name, "body") {
				if documentBodyFound {
					return "", ErrInvalidTextFile
				}
				documentBodyFound, documentBodyDepth = true, elementDepth
			}
			if documentBodyDepth > 0 {
				switch {
				case isWordprocessingElement(token.Name, "t"):
					textElementDepth = elementDepth
				case isWordprocessingElement(token.Name, "tab"):
					extractedTextBytes = append(extractedTextBytes, '\t')
				case isWordprocessingElement(token.Name, "br"), isWordprocessingElement(token.Name, "cr"):
					extractedTextBytes = append(extractedTextBytes, '\n')
				}
			}
		case xml.EndElement:
			if elementDepth == textElementDepth {
				textElementDepth = 0
			}
			if documentBodyDepth > 0 {
				// В конце ячейки и строки заменяем накопленные разделители одним
				// табом или переводом строки, сохраняя читаемую структуру таблицы.
				switch {
				case isWordprocessingElement(token.Name, "p"):
					extractedTextBytes = append(extractedTextBytes, '\n')
				case isWordprocessingElement(token.Name, "tc"):
					extractedTextBytes = bytes.TrimRight(extractedTextBytes, "\n\t")
					extractedTextBytes = append(extractedTextBytes, '\t')
				case isWordprocessingElement(token.Name, "tr"):
					extractedTextBytes = bytes.TrimRight(extractedTextBytes, "\n\t")
					extractedTextBytes = append(extractedTextBytes, '\n')
				}
			}
			if elementDepth == documentBodyDepth {
				documentBodyDepth = 0
			}
			elementDepth--
		case xml.CharData:
			if elementDepth == 0 && strings.TrimSpace(string(token)) != "" {
				return "", ErrInvalidTextFile
			}
			if textElementDepth > 0 {
				extractedTextBytes = append(extractedTextBytes, token...)
			}
		}
		// В UTF-8 одному символу нужно не более четырёх байт. Этот промежуточный
		// предел ограничивает рост памяти; точное количество символов проверит NormalizeAndValidateText.
		if len(extractedTextBytes) > MaxMaterialTextCharacters*utf8.UTFMax {
			return "", ErrMaterialSizeLimitExceeded
		}
	}
	if !documentRootFound || !documentBodyFound || elementDepth != 0 {
		return "", ErrInvalidTextFile
	}
	return NormalizeAndValidateText(string(extractedTextBytes))
}
