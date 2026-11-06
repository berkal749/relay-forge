// Package secret provides shared hashing helpers for API keys, worker tokens,
// and other bearer credentials.
package secret

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// HMAC returns a hex-encoded HMAC-SHA256 digest of value.
func HMAC(pepper []byte, value string) string {
	mac := hmac.New(sha256.New, pepper)
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

// SHA256 returns a hex-encoded unsalted SHA-256 digest of value. It exists only
// to recognize legacy hashes during upgrades.
func SHA256(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// Prefix returns at most length leading characters of value.
func Prefix(value string, length int) string {
	if length <= 0 {
		return ""
	}
	if len(value) <= length {
		return value
	}
	return value[:length]
}
