package llm

import (
	"encoding/json"
	"testing"
)

func TestJSONDatabaseRoundTrip(t *testing.T) {
	source := []byte(`{"options":[{"id":"a","text":"hello"}]}`)
	var value JSON
	if err := value.Scan(source); err != nil {
		t.Fatal(err)
	}
	// Драйвер вправе переиспользовать свой буфер после Scan.
	source[2] = 'X'
	want := `{"options":[{"id":"a","text":"hello"}]}`
	stored, err := value.Value()
	if err != nil || stored != want {
		t.Fatalf("Value() = %v, %v; want independent JSON string %s", stored, err, want)
	}
	encoded, err := json.Marshal(struct {
		Content JSON `json:"content"`
	}{Content: value})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"content":`+want+`}` {
		t.Fatalf("JSON was not embedded as an object: %s", encoded)
	}
	var readBack JSON
	if err := readBack.Scan(stored); err != nil || string(readBack) != want {
		t.Fatalf("Scan(Value()) = %s, %v", readBack, err)
	}
}

func TestJSONNullAndInvalidInput(t *testing.T) {
	value := JSON(`{"old":true}`)
	if err := value.Scan(nil); err != nil || value != nil {
		t.Fatalf("SQL NULL = %s, %v", value, err)
	}
	stored, err := value.Value()
	if err != nil || stored != nil {
		t.Fatalf("Value(nil) = %v, %v", stored, err)
	}
	encoded, err := json.Marshal(value)
	if err != nil || string(encoded) != "null" {
		t.Fatalf("Marshal(nil) = %s, %v", encoded, err)
	}
	for _, source := range []any{123, "{", []byte("invalid")} {
		if err := value.Scan(source); err == nil {
			t.Fatalf("Scan(%v) accepted invalid input", source)
		}
	}
	if _, err := JSON(`{`).Value(); err == nil {
		t.Fatal("Value accepted invalid JSON")
	}
	if err := json.Unmarshal([]byte(`{"name":"value"}`), &value); err != nil {
		t.Fatal(err)
	}
	if string(value) != `{"name":"value"}` {
		t.Fatalf("Unmarshal = %s", value)
	}
}
