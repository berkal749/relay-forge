package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterAllow(t *testing.T) {
	limiter := NewRateLimiter(2)
	start := time.Now()

	if allowed, remaining, _ := limiter.allow("key", start); !allowed || remaining != 1 {
		t.Fatalf("first allow = %v, remaining = %d, want true, 1", allowed, remaining)
	}
	if allowed, remaining, _ := limiter.allow("key", start); !allowed || remaining != 0 {
		t.Fatalf("second allow = %v, remaining = %d, want true, 0", allowed, remaining)
	}
	if allowed, _, retry := limiter.allow("key", start); allowed {
		t.Fatal("third allow succeeded, want limited")
	} else if retry <= 0 {
		t.Fatalf("retryAfter = %s, want positive", retry)
	}

	if allowed, _, _ := limiter.allow("key", start.Add(time.Minute)); !allowed {
		t.Fatal("allow after window reset = false, want true")
	}
	if allowed, _, _ := limiter.allow("other", start); !allowed {
		t.Fatal("independent key was limited")
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	limiter := NewRateLimiter(1)
	handler := RateLimit(limiter, func(*http.Request) string { return "client" })(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200", first.Code)
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", second.Code)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Fatal("Retry-After header is missing")
	}
}

func TestRateLimitDisabled(t *testing.T) {
	handler := RateLimit(NewRateLimiter(0), func(*http.Request) string { return "client" })(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	for i := 0; i < 5; i++ {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want 200", i+1, recorder.Code)
		}
	}
}
