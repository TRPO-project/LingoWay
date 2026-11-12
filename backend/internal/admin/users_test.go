package admin

import (
	"testing"

	"lingoway/internal/models/users"
)

func strPtr(value string) *string { return &value }
func boolPtr(value bool) *bool    { return &value }

func TestNewUserPatch(t *testing.T) {
	tests := []struct {
		name    string
		role    *string
		blocked *bool
		wantErr bool
	}{
		{"empty", nil, nil, true},
		{"student", strPtr("student"), nil, false},
		{"admin", strPtr("admin"), nil, false},
		{"teacher not assignable", strPtr("teacher"), nil, true},
		{"unknown role", strPtr("root"), nil, true},
		{"empty role", strPtr(""), nil, true},
		{"block only", nil, boolPtr(true), false},
		{"unblock only", nil, boolPtr(false), false},
		{"both", strPtr("student"), boolPtr(true), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			patch, err := NewUserPatch(test.role, test.blocked)
			if (err != nil) != test.wantErr {
				t.Fatalf("NewUserPatch() error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil && test.role != nil && (patch.Role == nil || string(*patch.Role) != *test.role) {
				t.Fatalf("role not carried over: %v", patch.Role)
			}
		})
	}
}

func TestEscapeLike(t *testing.T) {
	if got := escapeLike(`50%_a\b`); got != `50\%\_a\\b` {
		t.Fatalf("escapeLike = %q", got)
	}
	_ = users.RoleAdmin // пакет users используется только в основном файле
}
