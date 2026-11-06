package schedule

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"relayforge/internal/database/db"
)

func TestValidateCronAndNextRun(t *testing.T) {
	expression, location, err := validateCron("0 2 * * *", "UTC")
	if err != nil {
		t.Fatalf("validateCron: %v", err)
	}
	after := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)
	next, err := nextRun(expression, location, after)
	if err != nil {
		t.Fatalf("nextRun: %v", err)
	}
	want := time.Date(2026, 1, 2, 2, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next = %s, want %s", next, want)
	}
}

func TestValidateCronRejectsInvalidValues(t *testing.T) {
	if _, _, err := validateCron("not a cron", "UTC"); !errors.Is(err, ErrInvalidCron) {
		t.Fatalf("invalid cron error = %v", err)
	}
	if _, _, err := validateCron("0 2 * * *", "Mars/Olympus"); !errors.Is(err, ErrInvalidTimezone) {
		t.Fatalf("invalid timezone error = %v", err)
	}
	if _, _, err := validateCron("", "UTC"); !errors.Is(err, ErrInvalidCron) {
		t.Fatalf("empty cron error = %v", err)
	}
}

func TestValidateCronHonorsTimezone(t *testing.T) {
	expression, location, err := validateCron("0 9 * * *", "America/New_York")
	if err != nil {
		t.Fatalf("validateCron: %v", err)
	}
	after := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	next, err := nextRun(expression, location, after)
	if err != nil {
		t.Fatalf("nextRun: %v", err)
	}
	want := time.Date(2026, 1, 1, 14, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next = %s, want %s", next, want)
	}
}

func TestValidateTarget(t *testing.T) {
	jobType, queue, payload, err := validateTarget("http.request", " webhooks ", nil)
	if err != nil {
		t.Fatalf("validateTarget: %v", err)
	}
	if jobType != "http.request" || queue != "webhooks" || string(payload) != "{}" {
		t.Fatalf("unexpected target: %q %q %s", jobType, queue, payload)
	}

	if _, _, _, err := validateTarget("", "", nil); !errors.Is(err, ErrInvalidJobType) {
		t.Fatalf("empty job type error = %v", err)
	}
	if _, _, _, err := validateTarget("noop", "", json.RawMessage(`[1,2]`)); !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("array payload error = %v", err)
	}
	if _, _, _, err := validateTarget("noop", "", json.RawMessage(`{"a":`)); !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("invalid payload error = %v", err)
	}
	if _, _, _, err := validateTarget("noop", "", json.RawMessage(make([]byte, maxPayloadBytes+1))); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("large payload error = %v", err)
	}
}

func TestValidatePriorityAttempts(t *testing.T) {
	if err := validatePriorityAttempts(0, 0); err != nil {
		t.Fatalf("zero values should be valid: %v", err)
	}
	if err := validatePriorityAttempts(-1, 1); !errors.Is(err, ErrInvalidPriority) {
		t.Fatalf("negative priority error = %v", err)
	}
	if err := validatePriorityAttempts(2000, 1); !errors.Is(err, ErrInvalidPriority) {
		t.Fatalf("large priority error = %v", err)
	}
	if err := validatePriorityAttempts(1, 101); !errors.Is(err, ErrInvalidAttempts) {
		t.Fatalf("large attempts error = %v", err)
	}
}

func TestScheduleKeyIsStableAndUnique(t *testing.T) {
	item := sampleSchedule()
	first := scheduleKey(item)
	second := scheduleKey(item)
	if first != second {
		t.Fatal("scheduleKey is not deterministic")
	}
	if first == "" {
		t.Fatal("scheduleKey is empty")
	}

	item.NextRunAt = item.NextRunAt.Add(time.Minute)
	if scheduleKey(item) == first {
		t.Fatal("scheduleKey did not change with next_run_at")
	}
}

func sampleSchedule() db.Schedule {
	return db.Schedule{
		ID:        uuid.New(),
		NextRunAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	}
}
