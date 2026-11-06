// Package action implements the built-in worker handlers: outbound HTTP
// requests, delayed job submission, and notifications.
package action

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"relayforge/internal/execution"
)

const (
	defaultActionTimeout = 30 * time.Second
	actionDialTimeout    = 10 * time.Second
)

// NetworkConfig controls outbound HTTP from handlers.
type NetworkConfig struct {
	// EgressAllowlist restricts destinations to these hosts. Entries may be
	// exact hosts or a leading wildcard such as *.example.com. Empty means any
	// host that passes the private-address check.
	EgressAllowlist []string
	// AllowPrivateEgress permits loopback, private, and link-local targets.
	// It is required for local testing and for intentionally internal webhooks.
	AllowPrivateEgress bool
	Timeout            time.Duration
}

// NewHTTPClient builds an HTTP client that enforces the egress allowlist and,
// unless explicitly allowed, refuses loopback and private network targets. The
// address check happens when the connection is dialled, so it also covers
// redirects and DNS rebinding.
func NewHTTPClient(config NetworkConfig) (*http.Client, error) {
	allowed := make([]string, 0, len(config.EgressAllowlist))
	for _, entry := range config.EgressAllowlist {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			return nil, fmt.Errorf("egress allowlist contains an empty entry")
		}
		if strings.Contains(entry, "://") || strings.Contains(entry, "/") {
			return nil, fmt.Errorf("egress allowlist entries must be hostnames, got %q", entry)
		}
		allowed = append(allowed, entry)
	}

	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultActionTimeout
	}

	dialer := &net.Dialer{Timeout: actionDialTimeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, fmt.Errorf("split outbound address: %w", err)
			}
			if len(allowed) > 0 && !execution.HostAllowed(strings.ToLower(host), allowed) {
				return nil, fmt.Errorf("egress to %s is not allowed by WORKER_EGRESS_ALLOWLIST", host)
			}

			addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("resolve %s: %w", host, err)
			}

			var lastErr error
			for _, address := range addresses {
				if !config.AllowPrivateEgress && isPrivateAddress(address.IP) {
					lastErr = fmt.Errorf("egress to %s is blocked: private or loopback address %s", host, address.IP)
					continue
				}
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			if lastErr == nil {
				lastErr = fmt.Errorf("no addresses resolved for %s", host)
			}
			return nil, lastErr
		},
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: actionDialTimeout,
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
	}

	return &http.Client{Timeout: timeout, Transport: transport}, nil
}

func isPrivateAddress(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsMulticast() ||
		ip.IsInterfaceLocalMulticast()
}

// statusError builds a retryable or permanent error from an upstream response.
// 4xx responses (except 408, 425, and 429) will never succeed on retry.
func statusError(service string, status int, body []byte) error {
	snippet := strings.TrimSpace(string(body))
	if len(snippet) > 500 {
		snippet = snippet[:500]
	}
	if snippet == "" {
		snippet = http.StatusText(status)
	}
	err := fmt.Errorf("%s returned status %d: %s", service, status, snippet)
	if status >= 400 && status < 500 && status != http.StatusRequestTimeout &&
		status != http.StatusTooEarly && status != http.StatusTooManyRequests {
		return execution.Permanent(err)
	}
	return err
}
