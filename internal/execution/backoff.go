package execution

import "time"

const (
	BackoffExponential = "exponential"
	BackoffFixed       = "fixed"
)

// RetryPolicy describes how a worker pool delays and limits retries.
// A zero Base means retries are scheduled immediately.
type RetryPolicy struct {
	Backoff string
	Base    time.Duration
	Max     time.Duration
}

// Delay returns the retry delay for the given attempt number.
func (p RetryPolicy) Delay(attempt int32) time.Duration {
	if p.Backoff == BackoffFixed {
		if p.Base <= 0 {
			return 0
		}
		if p.Max > 0 && p.Base > p.Max {
			return p.Max
		}
		return p.Base
	}
	return RetryDelay(attempt, p.Base, p.Max)
}

// RetryDelay returns exponential backoff for the given attempt number.
func RetryDelay(attempt int32, base, maximum time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if base <= 0 {
		return 0
	}
	if maximum <= 0 {
		return base
	}

	delay := base
	for i := int32(1); i < attempt; i++ {
		if delay >= maximum/2 {
			return maximum
		}
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}
