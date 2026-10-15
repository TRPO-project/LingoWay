package llm

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// JSON хранит исходный JSON без лишней вложенности и отображается в PostgreSQL JSONB.
// SQL-ограничения требуют объект для content, answer_key, response, parameters и result.
// Проверка конкретной схемы каждого типа задания остаётся в прикладном коде.
// nil соответствует SQL NULL; для обязательного пустого объекта используйте JSON(`{}`).
type JSON []byte

// Value возвращает текст: это позволяет драйверу PostgreSQL передать JSONB,
// не интерпретируя []byte как двоичные данные (bytea).
func (value JSON) Value() (driver.Value, error) {
	if value == nil {
		return nil, nil
	}
	if !json.Valid(value) {
		return nil, fmt.Errorf("llm.JSON: invalid JSON")
	}
	return string(value), nil
}

// Scan копирует буфер драйвера: он может быть повторно использован после чтения строки.
func (value *JSON) Scan(source any) error {
	if value == nil {
		return fmt.Errorf("llm.JSON: Scan on nil receiver")
	}
	if source == nil {
		*value = nil
		return nil
	}
	var raw []byte
	switch source := source.(type) {
	case []byte:
		raw = source
	case string:
		raw = []byte(source)
	default:
		return fmt.Errorf("llm.JSON: cannot scan %T", source)
	}
	if !json.Valid(raw) {
		return fmt.Errorf("llm.JSON: invalid JSON")
	}
	*value = append(JSON(nil), raw...)
	return nil
}

// MarshalJSON сохраняет объект объектом, а не base64-строкой из []byte.
func (value JSON) MarshalJSON() ([]byte, error) {
	if value == nil {
		return []byte("null"), nil
	}
	if !json.Valid(value) {
		return nil, fmt.Errorf("llm.JSON: invalid JSON")
	}
	return value, nil
}

// UnmarshalJSON сохраняет независимую копию данных запроса.
func (value *JSON) UnmarshalJSON(raw []byte) error {
	if value == nil {
		return fmt.Errorf("llm.JSON: UnmarshalJSON on nil receiver")
	}
	if !json.Valid(raw) {
		return fmt.Errorf("llm.JSON: invalid JSON")
	}
	*value = append(JSON(nil), raw...)
	return nil
}

// GormDataType позволяет GORM распознавать JSON как пользовательский тип.
func (JSON) GormDataType() string { return "json" }

// GormDBDataType выбирает JSONB для основной базы проекта.
func (JSON) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db.Dialector.Name() == "postgres" {
		return "JSONB"
	}
	return "JSON"
}
