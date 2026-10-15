package llm

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"lingoway/internal/models/users"

	"gorm.io/gorm/schema"
)

func TestEntityJSONHidesPrivateData(t *testing.T) {
	secret := "must-never-be-public"
	authorID := int64(42)
	cases := []struct {
		name        string
		value       any
		privateKeys []string
	}{
		{"user", users.User{PasswordHash: secret}, []string{"password_hash"}},
		{"module", Module{CreatedByUserID: &authorID, Creator: &users.User{Email: secret}}, []string{"created_by_user_id", "creator", "Creator"}},
		{"exercise", Exercise{Content: JSON(`{"options":[]}`), AnswerKey: JSON(`{"secret":"` + secret + `"}`), Explanation: secret}, []string{"answer_key", "explanation"}},
		{"material", Material{ObjectKey: &secret, ContentSHA256: &secret}, []string{"object_key", "content_sha256"}},
		{"llm_request", LLMRequest{
			Parameters: JSON(`{"secret":"` + secret + `"}`), Result: JSON(`{"secret":"` + secret + `"}`),
			Provider: &secret, Model: &secret, ErrorMessage: &secret, LeaseToken: &secret,
			PromptVersion: secret, OwnerUser: &users.User{Email: secret},
		}, []string{"owner_user_id", "parameters", "result", "provider", "model", "prompt_version", "error_message", "lease_token", "lease_expires_at", "attempt_count", "available_at"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), secret) {
				t.Fatalf("private value leaked: %s", encoded)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			for _, key := range tc.privateKeys {
				if _, exists := fields[key]; exists {
					t.Errorf("private JSON key %q leaked", key)
				}
			}
		})
	}
}

func TestGeneratedAnswerFieldAndRelationshipDirections(t *testing.T) {
	cache := &sync.Map{}
	naming := schema.NamingStrategy{}
	answerSchema, err := schema.Parse(&Answer{}, cache, naming)
	if err != nil {
		t.Fatal(err)
	}
	correct := answerSchema.LookUpField("IsCorrect")
	if correct == nil || !correct.Readable || correct.Creatable || correct.Updatable || !correct.IgnoreMigration {
		t.Fatalf("generated is_correct must be read-only and migrated by SQL: %+v", correct)
	}
	for _, value := range []any{&Material{}, &Module{}, &Exercise{}, &Answer{}, &LLMRequest{}} {
		parsed, err := schema.Parse(value, cache, naming)
		if err != nil {
			t.Fatal(err)
		}
		for name, relationship := range parsed.Relationships.Relations {
			if name == "Exercises" {
				if relationship.Type != schema.HasMany {
					t.Errorf("%s.Exercises = %s; want has_many", parsed.Name, relationship.Type)
				}
				continue
			}
			// GORM может добавить обратную связь при разборе циклических моделей;
			// проверяем только явно объявленные поля текущей модели.
			if relationship.Field.Schema == parsed && relationship.Type != schema.BelongsTo {
				t.Errorf("%s.%s = %s; want belongs_to", parsed.Name, name, relationship.Type)
			}
		}
	}
}
