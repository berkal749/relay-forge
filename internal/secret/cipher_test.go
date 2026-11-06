package secret

import (
	"strings"
	"testing"
)

func testCipher(t *testing.T) *Cipher {
	t.Helper()
	cipher, err := NewCipher(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	return cipher
}

func TestCipherRoundTrip(t *testing.T) {
	cipher := testCipher(t)
	plaintext := []byte(`{"webhook_url":"https://discord.com/api/webhooks/1/secret"}`)

	encrypted, err := cipher.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if strings.Contains(string(encrypted), "discord.com") {
		t.Fatal("ciphertext contains plaintext")
	}

	decrypted, err := cipher.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Fatalf("decrypted = %s, want %s", decrypted, plaintext)
	}
}

func TestCipherRejectsTampering(t *testing.T) {
	cipher := testCipher(t)
	encrypted, err := cipher.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	encrypted[len(encrypted)-1] ^= 0xff
	if _, err := cipher.Decrypt(encrypted); err == nil {
		t.Fatal("expected tampered ciphertext to fail")
	}
	if _, err := cipher.Decrypt([]byte("short")); err == nil {
		t.Fatal("expected short ciphertext to fail")
	}
}

func TestCipherKeyFormats(t *testing.T) {
	hexKey := strings.Repeat("ab", 32)
	for _, key := range []string{
		hexKey,
		strings.Repeat("a", 32),
	} {
		if _, err := NewCipher(key); err != nil {
			t.Fatalf("NewCipher(%q): %v", key, err)
		}
	}
	for _, key := range []string{"", "too-short", strings.Repeat("z", 64)} {
		if _, err := NewCipher(key); err == nil {
			t.Fatalf("NewCipher(%q) should fail", key)
		}
	}
	other := testCipher(t)
	encrypted, _ := other.Encrypt([]byte("secret"))
	wrong, _ := NewCipher(strings.Repeat("cd", 32))
	if _, err := wrong.Decrypt(encrypted); err == nil {
		t.Fatal("expected a different key to fail")
	}
}
