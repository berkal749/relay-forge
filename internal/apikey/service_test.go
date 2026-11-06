package apikey

import (
	"strings"
	"testing"
)

func TestGenerateKey(t *testing.T) {
	first, err := generateKey()
	if err != nil {
		t.Fatalf("generateKey() error = %v", err)
	}
	if !strings.HasPrefix(first, Prefix) {
		t.Fatalf("generateKey() key %q does not have prefix %q", first, Prefix)
	}

	second, err := generateKey()
	if err != nil {
		t.Fatalf("generateKey() error = %v", err)
	}
	if first == second {
		t.Fatal("generateKey() returned duplicate keys")
	}
}

func TestHashKey(t *testing.T) {
	pepper := []byte("pepper-value-123")
	first := hashKey(pepper, "rf_live_example")
	second := hashKey(pepper, "rf_live_example")
	if first != second {
		t.Fatal("hashKey() is not deterministic")
	}
	if first == hashKey(pepper, "rf_live_different") {
		t.Fatal("hashKey() returned the same hash for different keys")
	}
	if first == hashKey([]byte("another-pepper-123"), "rf_live_example") {
		t.Fatal("hashKey() is not bound to the pepper")
	}
	if len(first) != 64 {
		t.Fatalf("hashKey() length = %d, want 64", len(first))
	}
	if first == legacyHash("rf_live_example") {
		t.Fatal("hashKey() must differ from the legacy unsalted hash")
	}
}

func TestKeyPrefix(t *testing.T) {
	raw := "rf_live_abcdefghijklmnop"
	if got := keyPrefix(raw); got != "rf_live_abcd" {
		t.Fatalf("keyPrefix() = %q, want rf_live_abcd", got)
	}
}
