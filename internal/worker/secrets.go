package worker

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// LoadSecretsFile reads a JSON object of name/value secrets and places each
// entry in the process environment unless the variable is already set. Secrets
// stay local to the worker host and are never stored in RelayForge.
func LoadSecretsFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read worker secrets file: %w", err)
	}
	secrets := map[string]string{}
	if err := json.Unmarshal(data, &secrets); err != nil {
		return fmt.Errorf("decode worker secrets file: %w", err)
	}
	for name, value := range secrets {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("worker secrets file contains an empty variable name")
		}
		if _, exists := os.LookupEnv(name); exists {
			continue
		}
		if err := os.Setenv(name, value); err != nil {
			return fmt.Errorf("set worker secret %s: %w", name, err)
		}
	}
	return nil
}
