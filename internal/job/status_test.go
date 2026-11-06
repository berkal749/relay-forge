package job

import (
	"testing"
	"time"
)

func TestStatusTransitions(t *testing.T) {
	tests := []struct {
		from Status
		to   Status
		want bool
	}{
		{StatusPending, StatusScheduled, true},
		{StatusPending, StatusRunning, true},
		{StatusPending, StatusCancelled, true},
		{StatusScheduled, StatusRunning, true},
		{StatusScheduled, StatusCancelled, true},
		{StatusRunning, StatusSucceeded, true},
		{StatusRunning, StatusFailed, true},
		{StatusRunning, StatusScheduled, true},
		{StatusRunning, StatusCancelled, true},
		{StatusSucceeded, StatusRunning, false},
		{StatusFailed, StatusRunning, false},
		{StatusCancelled, StatusPending, false},
		{StatusPending, StatusSucceeded, false},
	}

	for _, test := range tests {
		if got := test.from.CanTransitionTo(test.to); got != test.want {
			t.Errorf("%s -> %s = %v, want %v", test.from, test.to, got, test.want)
		}
	}
}

func TestTerminalStatuses(t *testing.T) {
	for _, status := range []Status{StatusSucceeded, StatusFailed, StatusCancelled} {
		if !status.Terminal() {
			t.Errorf("%s should be terminal", status)
		}
	}
	for _, status := range []Status{StatusPending, StatusScheduled, StatusRunning} {
		if status.Terminal() {
			t.Errorf("%s should not be terminal", status)
		}
	}
}

func TestInitialStatus(t *testing.T) {
	now := time.Now()
	if got := InitialStatus(now.Add(-time.Second), now); got != StatusPending {
		t.Fatalf("past job status = %s, want pending", got)
	}
	if got := InitialStatus(now.Add(time.Minute), now); got != StatusScheduled {
		t.Fatalf("future job status = %s, want scheduled", got)
	}
}
