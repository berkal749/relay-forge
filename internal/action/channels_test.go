package action

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"relayforge/internal/execution"
	"relayforge/internal/notificationchannel"
)

type fakeResolver struct {
	channel Channel
	err     error
}

func (f fakeResolver) ResolveChannel(context.Context, uuid.UUID, uuid.UUID) (Channel, error) {
	return f.channel, f.err
}

func TestSlackHandlerResolvesChannel(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	channelID := uuid.New()
	handler := NewSlackHandler(testClient(t, true), "", fakeResolver{
		channel: Channel{
			ID:     channelID,
			Kind:   notificationchannel.KindSlack,
			Config: map[string]string{"webhook_url": server.URL},
		},
	})
	payload, _ := json.Marshal(map[string]any{
		"channel_id": channelID,
		"text":       "from a channel",
	})
	if _, err := handler(context.Background(), testJob(string(payload))); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !called {
		t.Fatal("channel webhook was not called")
	}
}

func TestNotificationChannelErrors(t *testing.T) {
	channelID := uuid.New()
	tests := []struct {
		name     string
		resolver ChannelResolver
		payload  string
	}{
		{
			name:     "channel missing",
			resolver: fakeResolver{err: ErrChannelNotFound},
			payload:  `{"channel_id":"` + channelID.String() + `","text":"hi"}`,
		},
		{
			name: "kind mismatch",
			resolver: fakeResolver{channel: Channel{
				ID:     channelID,
				Kind:   notificationchannel.KindTelegram,
				Config: map[string]string{"chat_id": "1"},
			}},
			payload: `{"channel_id":"` + channelID.String() + `","text":"hi"}`,
		},
		{
			name:     "no resolver configured",
			resolver: nil,
			payload:  `{"channel_id":"` + channelID.String() + `","text":"hi"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := NewSlackHandler(testClient(t, true), "", test.resolver)
			_, err := handler(context.Background(), testJob(test.payload))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !execution.IsPermanent(err) {
				t.Fatalf("expected a permanent error, got %v", err)
			}
		})
	}
}

func TestAPIChannelResolver(t *testing.T) {
	channelID := uuid.New()
	var gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"channel":{"id":"` + channelID.String() + `","kind":"discord","config":{"webhook_url":"https://example.com/hook"}}}`))
	}))
	defer server.Close()

	resolver := NewAPIChannelResolver(server.URL, "worker-token", testClient(t, true))
	channel, err := resolver.ResolveChannel(context.Background(), uuid.New(), channelID)
	if err != nil {
		t.Fatalf("ResolveChannel: %v", err)
	}
	if channel.Kind != "discord" || channel.Config["webhook_url"] != "https://example.com/hook" {
		t.Fatalf("unexpected channel: %+v", channel)
	}
	if gotPath != "/v1/workers/notification-channels/"+channelID.String() {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer worker-token" {
		t.Fatalf("authorization = %q", gotAuth)
	}
}

func TestAPIChannelResolverNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"notification channel not found"}`))
	}))
	defer server.Close()

	resolver := NewAPIChannelResolver(server.URL, "token", testClient(t, true))
	_, err := resolver.ResolveChannel(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, ErrChannelNotFound) {
		t.Fatalf("error = %v, want ErrChannelNotFound", err)
	}
}
