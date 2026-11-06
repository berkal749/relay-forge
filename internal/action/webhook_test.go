package action

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"relayforge/internal/database/db"
	"relayforge/internal/execution"
)

func testJob(payload string) db.Job {
	return db.Job{
		ID:          uuid.New(),
		ProjectID:   uuid.New(),
		QueueName:   "default",
		JobType:     JobTypeHTTPRequest,
		Payload:     []byte(payload),
		Attempts:    1,
		MaxAttempts: 3,
	}
}

func testClient(t *testing.T, allowPrivate bool) *http.Client {
	t.Helper()
	client, err := NewHTTPClient(NetworkConfig{AllowPrivateEgress: allowPrivate})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	return client
}

func TestRenderTemplate(t *testing.T) {
	variables := map[string]any{
		"job_id": "abc",
		"count":  42,
		"name":   `a "quoted" value`,
	}
	rendered, err := renderTemplate(`{"id":"{{job_id}}","count":{{count}},"name":"{{name}}"}`, variables)
	if err != nil {
		t.Fatalf("renderTemplate: %v", err)
	}
	if !json.Valid([]byte(rendered)) {
		t.Fatalf("rendered template is not valid JSON: %s", rendered)
	}
	if !strings.Contains(rendered, `"id":"abc"`) || !strings.Contains(rendered, `"count":42`) {
		t.Fatalf("unexpected render: %s", rendered)
	}

	if _, err := renderTemplate("{{missing}}", variables); err == nil {
		t.Fatal("expected an error for an undefined variable")
	}
}

func TestBuildRequestBodyRejectsBothBodyAndTemplate(t *testing.T) {
	_, _, err := buildRequestBody(webhookPayload{
		Body:         json.RawMessage(`{"a":1}`),
		BodyTemplate: "hi",
	}, nil)
	if err == nil {
		t.Fatal("expected an error when both body fields are set")
	}
}

func TestWebhookSuccess(t *testing.T) {
	var gotMethod, gotBody, gotHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotHeader = r.Header.Get("X-Job")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("X-Reply", "ok")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"accepted":true}`))
	}))
	defer server.Close()

	handler := NewWebhookHandler(testClient(t, true))
	payload, _ := json.Marshal(map[string]any{
		"url":     server.URL,
		"method":  "POST",
		"headers": map[string]string{"X-Job": "{{job_id}}"},
		"body":    map[string]any{"kind": "ping"},
	})
	result, err := handler(context.Background(), testJob(string(payload)))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %s, want POST", gotMethod)
	}
	if gotHeader == "" {
		t.Fatal("templated header was not sent")
	}
	if !strings.Contains(gotBody, `"kind":"ping"`) {
		t.Fatalf("body = %s", gotBody)
	}

	var decoded webhookResult
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if decoded.Status != http.StatusAccepted || decoded.Headers["X-Reply"] != "ok" {
		t.Fatalf("unexpected result: %+v", decoded)
	}
	if decoded.Body != `{"accepted":true}` {
		t.Fatalf("unexpected response body %q", decoded.Body)
	}
}

func TestWebhookClassifiesFailures(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		permanent bool
	}{
		{name: "bad request is permanent", status: http.StatusBadRequest, permanent: true},
		{name: "not found is permanent", status: http.StatusNotFound, permanent: true},
		{name: "too many requests retries", status: http.StatusTooManyRequests},
		{name: "server error retries", status: http.StatusInternalServerError},
		{name: "bad gateway retries", status: http.StatusBadGateway},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte("upstream says no"))
			}))
			defer server.Close()

			handler := NewWebhookHandler(testClient(t, true))
			payload, _ := json.Marshal(map[string]any{"url": server.URL})
			_, err := handler(context.Background(), testJob(string(payload)))
			if err == nil {
				t.Fatal("expected an error")
			}
			if execution.IsPermanent(err) != test.permanent {
				t.Fatalf("IsPermanent(%v) = %v, want %v", err, execution.IsPermanent(err), test.permanent)
			}
		})
	}
}

func TestWebhookAllowNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"reason":"duplicate"}`))
	}))
	defer server.Close()

	handler := NewWebhookHandler(testClient(t, true))
	payload, _ := json.Marshal(map[string]any{
		"url":           server.URL,
		"allow_non_2xx": true,
	})
	result, err := handler(context.Background(), testJob(string(payload)))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var decoded webhookResult
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Status != http.StatusConflict {
		t.Fatalf("status = %d, want 409", decoded.Status)
	}
}

func TestWebhookBlocksPrivateEgressByDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("should not be reached"))
	}))
	defer server.Close()

	handler := NewWebhookHandler(testClient(t, false))
	payload, _ := json.Marshal(map[string]any{"url": server.URL})
	_, err := handler(context.Background(), testJob(string(payload)))
	if err == nil {
		t.Fatal("expected private egress to be blocked")
	}
	if execution.IsPermanent(err) {
		t.Fatalf("network rejection should be retryable, got %v", err)
	}
	if !strings.Contains(err.Error(), "private or loopback") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWebhookRejectsInvalidPayload(t *testing.T) {
	handler := NewWebhookHandler(testClient(t, true))
	_, err := handler(context.Background(), testJob(`{"url":"ftp://example.com"}`))
	if err == nil || !execution.IsPermanent(err) {
		t.Fatalf("expected a permanent invalid-url error, got %v", err)
	}
}

func TestHTTPClientEnforcesAllowlist(t *testing.T) {
	client, err := NewHTTPClient(NetworkConfig{
		EgressAllowlist:    []string{"allowed.example.com"},
		AllowPrivateEgress: true,
	})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	request, _ := http.NewRequest(http.MethodGet, "http://blocked.example.com", nil)
	if _, err := client.Do(request); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected an allowlist error, got %v", err)
	}
	if _, err := NewHTTPClient(NetworkConfig{EgressAllowlist: []string{"https://example.com"}}); err == nil {
		t.Fatal("expected scheme entries to be rejected")
	}
}

func TestPermanentError(t *testing.T) {
	base := errors.New("boom")
	if execution.IsPermanent(base) {
		t.Fatal("unwrapped error must not be permanent")
	}
	wrapped := execution.Permanent(base)
	if !execution.IsPermanent(wrapped) || !errors.Is(wrapped, base) {
		t.Fatal("permanent wrapper lost its cause")
	}
}
