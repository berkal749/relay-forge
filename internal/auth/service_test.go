package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{name: "lowercases and trims", input: "  User@Example.COM ", want: "user@example.com"},
		{name: "rejects empty", input: " ", wantErr: ErrInvalidEmail},
		{name: "rejects display name", input: "User <user@example.com>", wantErr: ErrInvalidEmail},
		{name: "rejects invalid address", input: "not-an-email", wantErr: ErrInvalidEmail},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeEmail(test.input)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("normalizeEmail() error = %v, want %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("normalizeEmail() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr error
	}{
		{name: "accepts valid password", value: "correct-horse-battery"},
		{name: "rejects short password", value: "short", wantErr: ErrWeakPassword},
		{name: "rejects bcrypt overflow", value: strings.Repeat("a", 73), wantErr: ErrWeakPassword},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validatePassword(test.value); !errors.Is(err, test.wantErr) {
				t.Fatalf("validatePassword() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}
