package catalog

import (
	"strings"
	"testing"

	"lingoway/internal/models"
)

func strPtr(value string) *string { return &value }

func TestMaterialInputNormalize(t *testing.T) {
	tests := []struct {
		name    string
		input   MaterialInput
		wantErr bool
	}{
		{"valid text", MaterialInput{TopicID: 1, Level: models.LevelB1, Type: models.CatalogText, Title: "Title", Body: strPtr("Some text")}, false},
		{"valid link", MaterialInput{TopicID: 1, Level: models.LevelB1, Type: models.CatalogLink, Title: "Title", URL: strPtr("https://example.com/page")}, false},
		{"text without body", MaterialInput{TopicID: 1, Level: models.LevelB1, Type: models.CatalogText, Title: "Title"}, true},
		{"text with url", MaterialInput{TopicID: 1, Level: models.LevelB1, Type: models.CatalogText, Title: "Title", Body: strPtr("x"), URL: strPtr("https://example.com")}, true},
		{"link without url", MaterialInput{TopicID: 1, Level: models.LevelB1, Type: models.CatalogLink, Title: "Title"}, true},
		{"link with body", MaterialInput{TopicID: 1, Level: models.LevelB1, Type: models.CatalogLink, Title: "Title", URL: strPtr("https://example.com"), Body: strPtr("x")}, true},
		{"javascript url", MaterialInput{TopicID: 1, Level: models.LevelB1, Type: models.CatalogLink, Title: "Title", URL: strPtr("javascript:alert(1)")}, true},
		{"uppercase scheme", MaterialInput{TopicID: 1, Level: models.LevelB1, Type: models.CatalogLink, Title: "Title", URL: strPtr("HTTP://example.com")}, true},
		{"url with space", MaterialInput{TopicID: 1, Level: models.LevelB1, Type: models.CatalogLink, Title: "Title", URL: strPtr("https://exa mple.com")}, true},
		{"bad level", MaterialInput{TopicID: 1, Level: "D9", Type: models.CatalogText, Title: "Title", Body: strPtr("x")}, true},
		{"empty title", MaterialInput{TopicID: 1, Level: models.LevelB1, Type: models.CatalogText, Title: "   ", Body: strPtr("x")}, true},
		{"long title", MaterialInput{TopicID: 1, Level: models.LevelB1, Type: models.CatalogText, Title: strings.Repeat("a", 201), Body: strPtr("x")}, true},
		{"long description", MaterialInput{TopicID: 1, Level: models.LevelB1, Type: models.CatalogText, Title: "Title", Description: strPtr(strings.Repeat("a", 501)), Body: strPtr("x")}, true},
		{"unknown type", MaterialInput{TopicID: 1, Level: models.LevelB1, Type: "video", Title: "Title", Body: strPtr("x")}, true},
		{"no topic", MaterialInput{Level: models.LevelB1, Type: models.CatalogText, Title: "Title", Body: strPtr("x")}, true},
		{"nul in body", MaterialInput{TopicID: 1, Level: models.LevelB1, Type: models.CatalogText, Title: "Title", Body: strPtr("a\x00b")}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.input.Normalize()
			if (err != nil) != test.wantErr {
				t.Fatalf("Normalize() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestNormalizeTrimsAndClearsUnusedFields(t *testing.T) {
	input := MaterialInput{
		TopicID: 1, Level: models.LevelA1, Type: models.CatalogText,
		Title: "  Title  ", Description: strPtr("   "), Body: strPtr("text"), URL: strPtr("  "),
	}
	if err := input.Normalize(); err != nil {
		t.Fatal(err)
	}
	if input.Title != "Title" {
		t.Fatalf("title not trimmed: %q", input.Title)
	}
	if input.Description != nil || input.URL != nil {
		t.Fatal("empty description and unused url must become nil")
	}
}
