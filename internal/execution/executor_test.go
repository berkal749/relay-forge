package execution

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"relayforge/internal/database/db"
)

func TestRegistry(t *testing.T) {
	registry := NewRegistry()
	registry.Register("echo", Echo)

	job := db.Job{JobType: "echo", Payload: []byte(`{"value":1}`)}
	result, err := registry.Execute(context.Background(), job)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if string(result) != string(job.Payload) {
		t.Fatalf("Execute() = %s, want %s", result, job.Payload)
	}

	_, err = registry.Execute(context.Background(), db.Job{JobType: "missing"})
	if !errors.Is(err, ErrNoHandler) {
		t.Fatalf("Execute() error = %v, want ErrNoHandler", err)
	}
}

func TestNoopResultIsJSON(t *testing.T) {
	result, err := Noop(context.Background(), db.Job{})
	if err != nil {
		t.Fatalf("Noop() error = %v", err)
	}
	if !json.Valid(result) {
		t.Fatalf("Noop() returned invalid JSON: %s", result)
	}
}
