package action

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"relayforge/internal/execution"
)

func TestSlackHandlerPostsText(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	handler := NewSlackHandler(testClient(t, true), "")
	payload, _ := json.Marshal(map[string]any{
		"webhook_url": server.URL,
		"text":        "build finished",
	})
	if _, err := handler(context.Background(), testJob(string(payload))); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if body["text"] != "build finished" {
		t.Fatalf("unexpected slack body: %+v", body)
	}
}

func TestSlackHandlerUsesDefaultWebhook(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	handler := NewSlackHandler(testClient(t, true), server.URL)
	payload, _ := json.Marshal(map[string]any{"content": "hello"})
	if _, err := handler(context.Background(), testJob(string(payload))); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !called {
		t.Fatal("default webhook was not called")
	}
}

func TestDiscordHandlerPostsContent(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	handler := NewDiscordHandler(testClient(t, true), "")
	payload, _ := json.Marshal(map[string]any{
		"webhook_url": server.URL,
		"content":     "deploy complete",
		"username":    "RelayForge",
	})
	if _, err := handler(context.Background(), testJob(string(payload))); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if body["content"] != "deploy complete" || body["username"] != "RelayForge" {
		t.Fatalf("unexpected discord body: %+v", body)
	}
}

func TestTelegramHandler(t *testing.T) {
	var gotPath string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	handler := NewTelegramHandler(testClient(t, true), "secret-bot-token", server.URL)
	payload, _ := json.Marshal(map[string]any{
		"chat_id": "-100123",
		"text":    "hello telegram",
	})
	if _, err := handler(context.Background(), testJob(string(payload))); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if gotPath != "/botsecret-bot-token/sendMessage" {
		t.Fatalf("path = %q", gotPath)
	}
	if body["chat_id"] != "-100123" || body["text"] != "hello telegram" {
		t.Fatalf("unexpected telegram body: %+v", body)
	}
}

func TestSMSHandlerUsesTwilio(t *testing.T) {
	var gotPath, gotAuth string
	var form map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = r.ParseForm()
		form = r.PostForm
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"sid":"SM123"}`))
	}))
	defer server.Close()

	handler := NewSMSHandler(testClient(t, true), TwilioConfig{
		AccountSID: "AC123",
		AuthToken:  "token",
		FromNumber: "+15550000000",
		BaseURL:    server.URL,
	})
	payload, _ := json.Marshal(map[string]any{
		"to":   "+15551111111",
		"body": "code 1234",
	})
	if _, err := handler(context.Background(), testJob(string(payload))); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if gotPath != "/2010-04-01/Accounts/AC123/Messages.json" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth == "" || !strings.HasPrefix(gotAuth, "Basic ") {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if form["To"][0] != "+15551111111" || form["From"][0] != "+15550000000" || form["Body"][0] != "code 1234" {
		t.Fatalf("unexpected form: %+v", form)
	}
}

func TestNotificationValidationIsPermanent(t *testing.T) {
	tests := []struct {
		name    string
		handler execution.HandlerFunc
		payload string
	}{
		{
			name:    "slack without webhook",
			handler: NewSlackHandler(testClient(t, true), ""),
			payload: `{"text":"hi"}`,
		},
		{
			name:    "slack without content",
			handler: NewSlackHandler(testClient(t, true), "https://example.com/hook"),
			payload: `{}`,
		},
		{
			name:    "telegram without token",
			handler: NewTelegramHandler(testClient(t, true), "", ""),
			payload: `{"chat_id":"1","text":"hi"}`,
		},
		{
			name:    "sms without credentials",
			handler: NewSMSHandler(testClient(t, true), TwilioConfig{}),
			payload: `{"to":"+15551111111","body":"hi"}`,
		},
		{
			name:    "email without smtp",
			handler: NewEmailHandler(SMTPConfig{}),
			payload: `{"to":["a@example.com"],"subject":"hi","text":"body"}`,
		},
		{
			name:    "email invalid recipient",
			handler: NewEmailHandler(SMTPConfig{Host: "smtp.example.com", Port: 587, From: "a@example.com"}),
			payload: `{"to":["not-an-email"],"subject":"hi","text":"body"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.handler(context.Background(), testJob(test.payload))
			if err == nil || !execution.IsPermanent(err) {
				t.Fatalf("expected a permanent error, got %v", err)
			}
		})
	}
}

func TestSlackHandlerRetriesOnServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "overloaded")
	}))
	defer server.Close()

	handler := NewSlackHandler(testClient(t, true), server.URL)
	payload, _ := json.Marshal(map[string]any{"text": "hi"})
	_, err := handler(context.Background(), testJob(string(payload)))
	if err == nil {
		t.Fatal("expected an error")
	}
	if execution.IsPermanent(err) {
		t.Fatalf("5xx should be retryable, got %v", err)
	}
}
