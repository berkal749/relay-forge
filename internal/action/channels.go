package action

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"relayforge/internal/execution"
	"relayforge/internal/notificationchannel"
)

// ErrChannelNotFound is returned when a job references a channel that does not
// exist in its project.
var ErrChannelNotFound = errors.New("notification channel not found")

// Channel is the decrypted destination resolved for a job.
type Channel struct {
	ID     uuid.UUID
	Kind   string
	Config map[string]string
}

// ChannelResolver looks up a project notification channel. Direct workers read
// the database; remote workers call the API.
type ChannelResolver interface {
	ResolveChannel(ctx context.Context, projectID, channelID uuid.UUID) (Channel, error)
}

type directChannelResolver struct {
	service *notificationchannel.Service
}

func NewDirectChannelResolver(service *notificationchannel.Service) ChannelResolver {
	return &directChannelResolver{service: service}
}

func (d *directChannelResolver) ResolveChannel(ctx context.Context, projectID, channelID uuid.UUID) (Channel, error) {
	resolved, err := d.service.Resolve(ctx, projectID, channelID)
	if errors.Is(err, notificationchannel.ErrNotFound) {
		return Channel{}, ErrChannelNotFound
	}
	if err != nil {
		return Channel{}, err
	}
	return Channel{ID: resolved.ID, Kind: resolved.Kind, Config: resolved.Config}, nil
}

type apiChannelResolver struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewAPIChannelResolver(baseURL, token string, client *http.Client) ChannelResolver {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &apiChannelResolver{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  client,
	}
}

func (a *apiChannelResolver) ResolveChannel(ctx context.Context, _, channelID uuid.UUID) (Channel, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/v1/workers/notification-channels/"+channelID.String(), nil)
	if err != nil {
		return Channel{}, fmt.Errorf("build channel request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+a.token)

	response, err := a.client.Do(request)
	if err != nil {
		return Channel{}, fmt.Errorf("channel request: %w", err)
	}
	defer response.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return Channel{}, fmt.Errorf("read channel response: %w", err)
	}
	if response.StatusCode == http.StatusNotFound {
		return Channel{}, ErrChannelNotFound
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(payload, &failure) == nil && failure.Error != "" {
			return Channel{}, fmt.Errorf("resolve channel: %s", failure.Error)
		}
		return Channel{}, fmt.Errorf("resolve channel: unexpected status %d", response.StatusCode)
	}

	var decoded struct {
		Channel struct {
			ID     uuid.UUID         `json:"id"`
			Kind   string            `json:"kind"`
			Config map[string]string `json:"config"`
		} `json:"channel"`
	}
	if err := json.NewDecoder(bytes.NewReader(payload)).Decode(&decoded); err != nil {
		return Channel{}, fmt.Errorf("decode channel response: %w", err)
	}
	return Channel{ID: decoded.Channel.ID, Kind: decoded.Channel.Kind, Config: decoded.Channel.Config}, nil
}

func resolveChannel(ctx context.Context, resolver ChannelResolver, projectID, channelID uuid.UUID, kind string) (Channel, error) {
	if resolver == nil {
		return Channel{}, execution.Permanent(fmt.Errorf("notification channels are not configured on this worker"))
	}
	channel, err := resolver.ResolveChannel(ctx, projectID, channelID)
	if errors.Is(err, ErrChannelNotFound) {
		return Channel{}, execution.Permanent(fmt.Errorf("%w: %s", ErrChannelNotFound, channelID))
	}
	if err != nil {
		return Channel{}, err
	}
	if channel.Kind != kind {
		return Channel{}, execution.Permanent(fmt.Errorf("notification channel %s is %s, not %s", channelID, channel.Kind, kind))
	}
	return channel, nil
}
