package execution

import (
	"testing"
	"time"
)

func TestRetryDelay(t *testing.T) {
	base := 5 * time.Second
	maximum := time.Minute

	tests := []struct {
		attempt int32
		want    time.Duration
	}{
		{attempt: 0, want: 5 * time.Second},
		{attempt: 1, want: 5 * time.Second},
		{attempt: 2, want: 10 * time.Second},
		{attempt: 3, want: 20 * time.Second},
		{attempt: 4, want: 40 * time.Second},
		{attempt: 5, want: time.Minute},
		{attempt: 10, want: time.Minute},
	}

	for _, test := range tests {
		if got := RetryDelay(test.attempt, base, maximum); got != test.want {
			t.Errorf("RetryDelay(%d) = %s, want %s", test.attempt, got, test.want)
		}
	}
}
