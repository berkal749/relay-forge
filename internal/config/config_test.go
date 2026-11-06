package config

import (
	"strings"
	"testing"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"APP_ENV", "API_KEY_PEPPER", "WORKER_TOKEN_PEPPER", "REGISTRATION_MODE",
		"TRUST_PROXY_HEADERS", "RELAYFORGE_API_URL", "DATABASE_URL",
	} {
		t.Setenv(name, "")
	}
}

func TestDevelopmentUsesSafeDefaults(t *testing.T) {
	clearEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.RegistrationMode != "open" {
		t.Fatalf("registration mode = %q, want open", cfg.RegistrationMode)
	}
	if cfg.APIKeyPepper == "" {
		t.Fatal("expected a development pepper")
	}
}

func TestProductionRejectsWeakConfig(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{
			name: "missing worker token pepper",
			env: map[string]string{
				"APP_ENV":             "production",
				"API_KEY_PEPPER":      strings.Repeat("a", 32),
				"REGISTRATION_MODE":   "invite",
				"TRUST_PROXY_HEADERS": "true",
			},
			wantErr: "WORKER_TOKEN_PEPPER",
		},
		{
			name: "shared peppers",
			env: map[string]string{
				"APP_ENV":             "production",
				"API_KEY_PEPPER":      strings.Repeat("a", 32),
				"WORKER_TOKEN_PEPPER": strings.Repeat("a", 32),
				"REGISTRATION_MODE":   "invite",
				"TRUST_PROXY_HEADERS": "true",
			},
			wantErr: "must differ",
		},
		{
			name: "open registration",
			env: map[string]string{
				"APP_ENV":             "production",
				"API_KEY_PEPPER":      strings.Repeat("a", 32),
				"WORKER_TOKEN_PEPPER": strings.Repeat("b", 32),
				"REGISTRATION_MODE":   "open",
				"TRUST_PROXY_HEADERS": "true",
			},
			wantErr: "REGISTRATION_MODE=open",
		},
		{
			name: "implicit proxy trust",
			env: map[string]string{
				"APP_ENV":             "production",
				"API_KEY_PEPPER":      strings.Repeat("a", 32),
				"WORKER_TOKEN_PEPPER": strings.Repeat("b", 32),
				"REGISTRATION_MODE":   "invite",
			},
			wantErr: "TRUST_PROXY_HEADERS",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearEnv(t)
			for key, value := range test.env {
				t.Setenv(key, value)
			}
			cfg, err := Load()
			if err == nil {
				err = cfg.ValidateAPI()
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Load() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestProductionValidConfig(t *testing.T) {
	clearEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("API_KEY_PEPPER", strings.Repeat("a", 32))
	t.Setenv("WORKER_TOKEN_PEPPER", strings.Repeat("b", 32))
	t.Setenv("REGISTRATION_MODE", "invite")
	t.Setenv("TRUST_PROXY_HEADERS", "true")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := cfg.ValidateAPI(); err != nil {
		t.Fatalf("ValidateAPI() error = %v", err)
	}
	if cfg.RegistrationMode != "invite" {
		t.Fatalf("registration mode = %q, want invite", cfg.RegistrationMode)
	}
}

func TestProductionWorkerDoesNotNeedApiSecrets(t *testing.T) {
	clearEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("WORKER_MODE", "remote")
	t.Setenv("RELAYFORGE_API_URL", "https://relayforge.example.com")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := cfg.ValidateWorker(); err != nil {
		t.Fatalf("ValidateWorker() error = %v", err)
	}
	if err := cfg.ValidateAPI(); err == nil {
		t.Fatal("ValidateAPI() should reject a worker-only configuration")
	}
}
