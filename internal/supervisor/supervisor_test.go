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
	pool := db.WorkerPool{Name: "general", Concurrency: 4}
	if spec := specFor(pool); spec != "general|4" {
		t.Fatalf("specFor() = %q, want %q", spec, "general|4")
	}
}
