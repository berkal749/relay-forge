package worker

import (
	"sync"
	"time"
)

const rateLimitWindow = time.Minute

// rateLimiter allows at most limit reservations in a fixed one-minute window.
// A limit of zero means unlimited.
type rateLimiter struct {
	mu          sync.Mutex
	limit       int
	windowStart time.Time
	count       int
}

func newRateLimiter(limit int) *rateLimiter {
	return &rateLimiter{limit: limit}
}

// reserve attempts to consume one slot. When no slot is available it returns
// the duration until the current window resets.
func (l *rateLimiter) reserve(now time.Time) (time.Duration, bool) {
	if l.limit <= 0 {
		return 0, true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.windowStart.IsZero() || now.Sub(l.windowStart) >= rateLimitWindow {
		l.windowStart = now
		l.count = 0
	}

	if l.count >= l.limit {
		return windowRemaining(now, l.windowStart), false
	}

	l.count++
	return 0, true
}

func windowRemaining(now, windowStart time.Time) time.Duration {
	remaining := windowStart.Add(rateLimitWindow).Sub(now)
	if remaining <= 0 {
		return time.Millisecond
	}
	return remaining
}
