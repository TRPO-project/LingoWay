package admin

import (
	"strings"
	"testing"
)

func TestNormalizeTopicSlug(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"simple", "it", "it", false},
		{"digit and dash", "english-b2", "english-b2", false},
		{"trimmed", "  travel  ", "travel", false},
		{"uppercase", "IT", "", true},
		{"space inside", "my topic", "", true},
		{"underscore", "my_topic", "", true},
		{"leading dash", "-it", "", true},
		{"trailing dash", "it-", "", true},
		{"double dash", "a--b", "", true},
		{"cyrillic", "авиация", "", true},
		{"empty", "   ", "", true},
		{"too long", strings.Repeat("a", 51), "", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeTopicSlug(test.input)
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeTopicName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"simple", "Travel", "Travel", false},
		{"trimmed cyrillic", "  Путешествия  ", "Путешествия", false},
		{"empty", "   ", "", true},
		{"max length", strings.Repeat("я", 100), strings.Repeat("я", 100), false},
		{"too long", strings.Repeat("я", 101), "", true},
		{"nul character", "a\x00b", "", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeTopicName(test.input)
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}
