package worker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"relayforge/internal/execution"
)

func TestRemoteClaimNoContentMeansNoJobs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workers/claim" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	backend := NewRemoteBackend(server.URL, "rf_worker_test", [16]byte{}, nil)
	_, err := backend.Claim(context.Background())
	if !errors.Is(err, execution.ErrNoJobs) {
		t.Fatalf("Claim() error = %v, want ErrNoJobs", err)
	}
}

func TestRemoteUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	backend := NewRemoteBackend(server.URL, "rf_worker_test", [16]byte{}, nil)
	if err := backend.Heartbeat(context.Background()); !errors.Is(err, ErrRemoteUnauthorized) {
		t.Fatalf("Heartbeat() error = %v, want ErrRemoteUnauthorized", err)
	}
}
