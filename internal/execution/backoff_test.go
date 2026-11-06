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

func TestRetryPolicyDelay(t *testing.T) {
	tests := []struct {
		name    string
		policy  RetryPolicy
		attempt int32
		want    time.Duration
	}{
		{
			name:    "fixed ignores attempt",
			policy:  RetryPolicy{Backoff: BackoffFixed, Base: 5 * time.Second, Max: time.Minute},
			attempt: 4,
			want:    5 * time.Second,
		},
		{
			name:    "fixed clamps to max",
			policy:  RetryPolicy{Backoff: BackoffFixed, Base: time.Minute, Max: 10 * time.Second},
			attempt: 1,
			want:    10 * time.Second,
		},
		{
			name:    "exponential grows",
			policy:  RetryPolicy{Backoff: BackoffExponential, Base: time.Second, Max: time.Minute},
			attempt: 3,
			want:    4 * time.Second,
		},
		{
			name:    "empty backoff defaults to exponential",
			policy:  RetryPolicy{Base: time.Second, Max: time.Minute},
			attempt: 2,
			want:    2 * time.Second,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.policy.Delay(test.attempt); got != test.want {
				t.Fatalf("Delay(%d) = %s, want %s", test.attempt, got, test.want)
			}
		})
	}
}
