package middleware

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"relayforge/internal/http/httpx"
)

const (
	defaultRateLimitWindow = time.Minute
	rateLimitEntryTTL      = 5 * time.Minute
)

type rateLimitEntry struct {
	windowStart time.Time
	count       int
	lastSeen    time.Time
}

// RateLimiter is an in-memory fixed-window limiter. A limit of zero disables
// limiting. It is per-process; deployments with multiple API instances need a
// shared limiter for global enforcement.
type RateLimiter struct {
	limit       int
	window      time.Duration
	mu          sync.Mutex
	entries     map[string]*rateLimitEntry
	lastCleanup time.Time
}

type RateLimiters struct {
	Auth      *RateLimiter
	API       *RateLimiter
	JobSubmit *RateLimiter
}

func NewRateLimiter(limitPerMinute int) *RateLimiter {
	return &RateLimiter{
		limit:   limitPerMinute,
		window:  defaultRateLimitWindow,
		entries: make(map[string]*rateLimitEntry),
	}
}

func (l *RateLimiter) allow(key string, now time.Time) (allowed bool, remaining int, retryAfter time.Duration) {
	if l == nil || l.limit <= 0 {
		return true, -1, 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.lastCleanup.IsZero() || now.Sub(l.lastCleanup) >= time.Minute {
		l.cleanupLocked(now)
		l.lastCleanup = now
	}

	entry, ok := l.entries[key]
	if !ok || now.Sub(entry.windowStart) >= l.window {
		entry = &rateLimitEntry{windowStart: now}
		l.entries[key] = entry
	}
	entry.lastSeen = now

	if entry.count >= l.limit {
		return false, 0, windowRemaining(now, entry.windowStart, l.window)
	}

	entry.count++
	return true, l.limit - entry.count, 0
}

func (l *RateLimiter) cleanupLocked(now time.Time) {
	for key, entry := range l.entries {
		if now.Sub(entry.lastSeen) > rateLimitEntryTTL {
			delete(l.entries, key)
		}
	}
}

func windowRemaining(now, windowStart time.Time, window time.Duration) time.Duration {
	remaining := windowStart.Add(window).Sub(now)
	if remaining <= 0 {
		return time.Millisecond
	}
	return remaining
}

func RateLimit(limiter *RateLimiter, keyFn func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limiter == nil || limiter.limit <= 0 {
				next.ServeHTTP(w, r)
				return
			}

			allowed, remaining, retryAfter := limiter.allow(keyFn(r), time.Now())
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limiter.limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(max(remaining, 0)))

			if !allowed {
				retrySeconds := int(math.Ceil(retryAfter.Seconds()))
				if retrySeconds < 1 {
					retrySeconds = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
				httpx.WriteError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func RateLimitKeyByIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return "ip:" + host
}

func RateLimitKeyBySession(r *http.Request) string {
	if session, ok := SessionFromContext(r.Context()); ok {
		return "user:" + session.User.ID.String()
	}
	return RateLimitKeyByIP(r)
}

func RateLimitKeyByAPIKey(r *http.Request) string {
	if key, ok := APIKeyFromContext(r.Context()); ok {
		return "apikey:" + key.ID.String()
	}
	return RateLimitKeyByIP(r)
}
