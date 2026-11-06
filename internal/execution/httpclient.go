package execution

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// NewAllowlistedClient returns an HTTP client that only permits requests to the
// configured domains. Entries may be exact hosts (api.example.com) or a single
// leading wildcard (*.example.com). An empty allowlist returns a normal client.
//
// Egress enforcement is only effective for handlers that use this client.
func NewAllowlistedClient(allowlist []string, timeout time.Duration) (*http.Client, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if len(allowlist) == 0 {
		return &http.Client{Timeout: timeout}, nil
	}

	allowed := make([]string, 0, len(allowlist))
	for _, entry := range allowlist {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			return nil, fmt.Errorf("egress allowlist contains an empty entry")
		}
		if strings.Contains(entry, "://") || strings.Contains(entry, "/") {
			return nil, fmt.Errorf("egress allowlist entries must be hostnames, got %q", entry)
		}
		allowed = append(allowed, entry)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	return &http.Client{
		Timeout: timeout,
		Transport: &allowlistTransport{
			base:    transport,
			allowed: allowed,
		},
	}, nil
}

type allowlistTransport struct {
	base    http.RoundTripper
	allowed []string
}

func (t *allowlistTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	host := strings.ToLower(request.URL.Hostname())
	if !HostAllowed(host, t.allowed) {
		return nil, fmt.Errorf("egress to %s is not allowed by WORKER_EGRESS_ALLOWLIST", host)
	}
	return t.base.RoundTrip(request)
}

// HostAllowed reports whether host matches an allowlist entry. Entries may be
// exact hosts or a leading wildcard such as *.example.com.
func HostAllowed(host string, allowed []string) bool {
	for _, entry := range allowed {
		if entry == host {
			return true
		}
		if strings.HasPrefix(entry, "*.") {
			suffix := strings.TrimPrefix(entry, "*")
			if strings.HasSuffix(host, suffix) && host != strings.TrimPrefix(suffix, ".") {
				return true
			}
		}
	}
	return false
}
