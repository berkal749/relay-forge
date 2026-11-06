package job

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestNormalizeIdempotencyKey(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{name: "empty is allowed", input: "", want: ""},
		{name: "trims surrounding space", input: "  abc-123  ", want: "abc-123"},
		{name: "rejects too long", input: strings.Repeat("a", maxIdempotencyKeyLength+1), wantErr: ErrInvalidIdempotencyKey},
		{name: "rejects control characters", input: "abc\n123", wantErr: ErrInvalidIdempotencyKey},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeIdempotencyKey(test.input)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("normalizeIdempotencyKey() error = %v, want %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("normalizeIdempotencyKey() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSubmitFingerprint(t *testing.T) {
	payload := json.RawMessage(`{"user_id":"123"}`)
	base := SubmitInput{QueueName: "default", JobType: "send_email", Priority: int32Ptr(1)}

	first := submitFingerprint("send_email", payload, base)
	if second := submitFingerprint("send_email", payload, base); first != second {
		t.Fatal("submitFingerprint() is not deterministic")
	}

	differentPayload := submitFingerprint("send_email", json.RawMessage(`{"user_id":"456"}`), base)
	if first == differentPayload {
		t.Fatal("submitFingerprint() ignored the payload")
	}

	differentQueue := base
	differentQueue.QueueName = "critical"
	if first == submitFingerprint("send_email", payload, differentQueue) {
		t.Fatal("submitFingerprint() ignored the queue")
	}

	differentPriority := base
	differentPriority.Priority = int32Ptr(2)
	if first == submitFingerprint("send_email", payload, differentPriority) {
		t.Fatal("submitFingerprint() ignored the priority")
	}
}
