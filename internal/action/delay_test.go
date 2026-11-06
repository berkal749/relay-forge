package action

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"relayforge/internal/execution"
)

type recordingSubmitter struct {
	inputs []SubmitJobInput
	err    error
}

func (r *recordingSubmitter) SubmitJob(_ context.Context, input SubmitJobInput) (uuid.UUID, error) {
	r.inputs = append(r.inputs, input)
	if r.err != nil {
		return uuid.Nil, r.err
	}
	return uuid.New(), nil
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func TestResolveDelayTime(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		payload delayPayload
		want    time.Time
		wantErr bool
	}{
		{
			name:    "delay seconds",
			payload: delayPayload{DelaySeconds: 300},
			want:    now.Add(5 * time.Minute),
		},
		{
			name:    "immediate",
			payload: delayPayload{},
			want:    now,
		},
		{
			name:    "absolute time",
			payload: delayPayload{RunAt: timePtr(now.Add(time.Hour))},
			want:    now.Add(time.Hour),
		},
		{
			name:    "past time becomes now",
			payload: delayPayload{RunAt: timePtr(now.Add(-time.Hour))},
			want:    now,
		},
		{
			name:    "both fields",
			payload: delayPayload{DelaySeconds: 60, RunAt: timePtr(now.Add(time.Hour))},
			wantErr: true,
		},
		{
			name:    "negative delay",
			payload: delayPayload{DelaySeconds: -1},
			wantErr: true,
		},
		{
			name:    "delay too large",
			payload: delayPayload{DelaySeconds: maxDelaySeconds + 1},
			wantErr: true,
		},
		{
			name:    "run too far ahead",
			payload: delayPayload{RunAt: timePtr(now.Add(2 * 365 * 24 * time.Hour))},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveDelayTime(test.payload, now)
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %s", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveDelayTime: %v", err)
			}
			if !got.Equal(test.want) {
				t.Fatalf("got %s, want %s", got, test.want)
			}
		})
	}
}

func TestDelayHandlerSubmitsFollowUp(t *testing.T) {
	submitter := &recordingSubmitter{}
	handler := NewDelayHandler(submitter)
	job := testJob(`{
		"delay_seconds": 600,
		"job": {
			"job_type": "http.request",
			"payload": {"url": "https://example.com"},
			"queue_name": "webhooks",
			"max_attempts": 5
		}
	}`)

	result, err := handler(context.Background(), job)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if len(submitter.inputs) != 1 {
		t.Fatalf("expected one submission, got %d", len(submitter.inputs))
	}
	input := submitter.inputs[0]
	if input.JobType != "http.request" || input.QueueName != "webhooks" {
		t.Fatalf("unexpected submission: %+v", input)
	}
	if input.IdempotencyKey != "delay:"+job.ID.String() {
		t.Fatalf("idempotency key = %q", input.IdempotencyKey)
	}
	if input.MaxAttempts == nil || *input.MaxAttempts != 5 {
		t.Fatalf("max attempts not forwarded: %+v", input.MaxAttempts)
	}
	if time.Until(input.ScheduledAt) < 9*time.Minute {
		t.Fatalf("scheduled too soon: %s", input.ScheduledAt)
	}

	var decoded delayResult
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if decoded.JobID == uuid.Nil {
		t.Fatal("result did not include the follow-up job id")
	}
}

func TestDelayHandlerValidation(t *testing.T) {
	handler := NewDelayHandler(&recordingSubmitter{})

	tests := []struct {
		name    string
		payload string
	}{
		{name: "missing job type", payload: `{"delay_seconds":10}`},
		{name: "bad json", payload: `{`},
		{name: "both timings", payload: `{"delay_seconds":10,"run_at":"2030-01-01T00:00:00Z","job":{"job_type":"noop"}}`},
		{name: "bad attempts", payload: `{"job":{"job_type":"noop","max_attempts":500}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := handler(context.Background(), testJob(test.payload))
			if err == nil || !execution.IsPermanent(err) {
				t.Fatalf("expected a permanent error, got %v", err)
			}
		})
	}
}

func TestDelayHandlerPropagatesSubmitFailure(t *testing.T) {
	submitter := &recordingSubmitter{err: fmt.Errorf("queue full")}
	handler := NewDelayHandler(submitter)
	_, err := handler(context.Background(), testJob(`{"job":{"job_type":"noop"}}`))
	if err == nil {
		t.Fatal("expected an error")
	}
	if execution.IsPermanent(err) {
		t.Fatalf("submission failure should be retryable, got %v", err)
	}
}

func TestAPISubmitterSendsIdempotencyKey(t *testing.T) {
	returnedID := uuid.New()
	var gotKey, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("Idempotency-Key")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"job":{"id":"` + returnedID.String() + `"}}`))
	}))
	defer server.Close()

	submitter := NewAPISubmitter(server.URL, "token", testClient(t, true))
	jobID, err := submitter.SubmitJob(context.Background(), SubmitJobInput{
		JobType:        "noop",
		Payload:        json.RawMessage(`{}`),
		IdempotencyKey: "delay:abc",
	})
	if err != nil {
		t.Fatalf("SubmitJob: %v", err)
	}
	if jobID != returnedID {
		t.Fatalf("job id = %s, want %s", jobID, returnedID)
	}
	if gotKey != "delay:abc" {
		t.Fatalf("idempotency key = %q", gotKey)
	}
	if gotAuth != "Bearer token" {
		t.Fatalf("authorization = %q", gotAuth)
	}
}
