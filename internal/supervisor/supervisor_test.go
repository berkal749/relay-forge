package supervisor

import (
	"strings"
	"testing"

	"relayforge/internal/database/db"
)

func TestMergeEnv(t *testing.T) {
	got := mergeEnv([]string{"A=1", "B=2", "C=3"}, map[string]string{
		"B": "override",
		"D": "new",
	})

	joined := strings.Join(got, "\n")
	for _, want := range []string{"A=1", "B=override", "C=3", "D=new"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("mergeEnv() = %q, missing %q", joined, want)
		}
	}
	if strings.Contains(joined, "B=2") {
		t.Fatalf("mergeEnv() kept overridden value: %q", joined)
	}
}

func TestSpecFor(t *testing.T) {
	pool := db.WorkerPool{
		Name:                  "general",
		Concurrency:           4,
		QueueNames:            []string{"default", "emails"},
		JobTimeoutSeconds:     30,
		MaxAttempts:           5,
		RetryBackoff:          "fixed",
		RetryBaseDelaySeconds: 10,
		RetryMaxDelaySeconds:  60,
	}
	want := "general|4|default,emails|30|5|fixed|10|60"
	if spec := specFor(pool); spec != want {
		t.Fatalf("specFor() = %q, want %q", spec, want)
	}
}
