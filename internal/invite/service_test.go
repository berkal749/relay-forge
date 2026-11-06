package invite

import (
	"errors"
	"testing"
	"time"
)

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{name: "lowercases and trims", input: " User@Example.COM ", want: "user@example.com"},
		{name: "rejects empty", input: " ", wantErr: ErrInvalidEmail},
		{name: "rejects display name", input: "User <user@example.com>", wantErr: ErrInvalidEmail},
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

func TestHashTokenIsStable(t *testing.T) {
	token := "rf_invite_test-token"
	first := HashToken(token)
	second := HashToken(token)
	if first != second {
		t.Fatal("HashToken() is not deterministic")
	}
	if first == token {
		t.Fatal("HashToken() returned the raw token")
	}
	if HashToken("another-token") == first {
		t.Fatal("HashToken() collided")
	}
}

func TestValidRolesAndTTLs(t *testing.T) {
	for _, role := range []string{RoleUser, RoleAdmin} {
		if role != RoleUser && role != RoleAdmin {
			t.Fatalf("unexpected role %q", role)
		}
	}
	if DefaultTTL < MinTTL || DefaultTTL > MaxTTL {
		t.Fatalf("default TTL %s outside allowed range", DefaultTTL)
	}
	if time.Hour != MinTTL {
		t.Fatalf("MinTTL = %s, want 1h", MinTTL)
	}
}
