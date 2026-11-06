package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"relayforge/internal/database/db"
)

// Echo returns the submitted payload as the execution result. It is a development handler.
func Echo(_ context.Context, job db.Job) (json.RawMessage, error) {
	return json.RawMessage(job.Payload), nil
}

// Noop completes immediately with an empty object result. It is a development handler.
func Noop(_ context.Context, _ db.Job) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

// Sleep waits for payload.duration_ms and respects the execution context. It is a development handler.
func Sleep(ctx context.Context, job db.Job) (json.RawMessage, error) {
	var payload struct {
		DurationMS int64 `json:"duration_ms"`
	}
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode sleep payload: %w", err)
	}
	if payload.DurationMS <= 0 {
		return json.RawMessage(`{}`), nil
	}

	timer := time.NewTimer(time.Duration(payload.DurationMS) * time.Millisecond)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return json.RawMessage(fmt.Sprintf(`{"slept_ms":%d}`, payload.DurationMS)), nil
	}
}
