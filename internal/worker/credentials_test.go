package worker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoredCredentialsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "worker-token.json")
	want := storedCredentials{ProjectID: "5b1f3a2e-0000-0000-0000-000000000000", Token: "rf_worker_example"}

	if err := writeStoredCredentials(path, want); err != nil {
		t.Fatalf("writeStoredCredentials() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode = %o, want 600", info.Mode().Perm())
	}

	got, err := readStoredCredentials(path)
	if err != nil {
		t.Fatalf("readStoredCredentials() error = %v", err)
	}
	if got != want {
		t.Fatalf("readStoredCredentials() = %+v, want %+v", got, want)
	}
}

func TestRequireProjectID(t *testing.T) {
	if _, err := requireProjectID(""); err == nil {
		t.Fatal("requireProjectID(\"\") expected error")
	}
	if _, err := requireProjectID("not-a-uuid"); err == nil {
		t.Fatal("requireProjectID(invalid) expected error")
	}
	if _, err := requireProjectID("5b1f3a2e-0000-0000-0000-000000000000"); err != nil {
		t.Fatalf("requireProjectID(valid) error = %v", err)
	}
}
