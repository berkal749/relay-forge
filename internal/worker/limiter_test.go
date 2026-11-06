package worker

import (
	"testing"
	"time"
)

func TestRateLimiterUnlimited(t *testing.T) {
	limiter := newRateLimiter(0)
	now := time.Now()
	for i := 0; i < 100; i++ {
		if wait, ok := limiter.reserve(now); !ok {
			t.Fatalf("reserve() denied with unlimited limit, wait = %s", wait)
		}
	}
}

func TestRateLimiterFixedWindow(t *testing.T) {
	limiter := newRateLimiter(2)
	start := time.Now()

	for i := 0; i < 2; i++ {
		if _, ok := limiter.reserve(start); !ok {
			t.Fatalf("reserve() denied within limit on call %d", i+1)
		}
	}

	wait, ok := limiter.reserve(start.Add(10 * time.Second))
	if ok {
		t.Fatal("reserve() allowed above limit")
	}
	if wait <= 0 || wait > rateLimitWindow {
		t.Fatalf("reserve() wait = %s, want within (0, %s]", wait, rateLimitWindow)
	}

	if _, ok := limiter.reserve(start.Add(rateLimitWindow)); !ok {
		t.Fatal("reserve() denied after window reset")
	}
}
