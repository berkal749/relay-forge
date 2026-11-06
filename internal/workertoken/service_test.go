package workertoken

import (
	"strings"
	"testing"
)

func TestGenerateToken(t *testing.T) {
	first, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken() error = %v", err)
	}
	if !strings.HasPrefix(first, Prefix) {
		t.Fatalf("generateToken() token %q does not have prefix %q", first, Prefix)
	}

	second, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken() error = %v", err)
	}
	if first == second {
		t.Fatal("generateToken() returned duplicate tokens")
	}
}
