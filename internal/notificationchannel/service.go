// Package notificationchannel stores project-scoped notification destinations.
// Destination configuration is encrypted at rest and never returned to the
// dashboard; workers resolve a channel when executing a notification job.
package notificationchannel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"relayforge/internal/database/db"
	"relayforge/internal/project"
	"relayforge/internal/secret"
)

const (
	KindSlack    = "slack"
	KindDiscord  = "discord"
	KindTelegram = "telegram"
	KindSMS      = "sms"
	KindEmail    = "email"

	maxNameLength = 100
)

var (
	ErrInvalidName   = errors.New("channel name must be between 1 and 100 characters")
	ErrInvalidKind   = errors.New("channel kind must be slack, discord, telegram, sms, or email")
	ErrInvalidConfig = errors.New("invalid channel configuration")
	ErrNotFound      = errors.New("notification channel not found")
	ErrNameTaken     = errors.New("a notification channel with that name already exists")
	ErrLimit         = errors.New("notification channel limit reached for this project")
)

// Limits caps per-project channels. A zero value means unlimited.
type Limits struct {
	MaxChannelsPerProject int
}

type Service struct {
	queries  *db.Queries
	projects *project.Service
	cipher   *secret.Cipher
	limits   Limits
}

type CreateInput struct {
	Name   string
	Kind   string
	Config map[string]string
}

// Resolved is the decrypted destination handed to a worker.
type Resolved struct {
	ID     uuid.UUID
	Kind   string
	Config map[string]string
}

func NewService(queries *db.Queries, projects *project.Service, cipher *secret.Cipher, limits Limits) *Service {
	return &Service{queries: queries, projects: projects, cipher: cipher, limits: limits}
}

func (s *Service) Create(ctx context.Context, userID, projectID uuid.UUID, input CreateInput) (db.NotificationChannel, error) {
	if s.projects != nil {
		if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
			return db.NotificationChannel{}, err
		}
	}

	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > maxNameLength {
		return db.NotificationChannel{}, ErrInvalidName
	}
	kind, config, err := normalize(input.Kind, input.Config)
	if err != nil {
		return db.NotificationChannel{}, err
	}

	if s.limits.MaxChannelsPerProject > 0 {
		count, err := s.queries.CountNotificationChannelsByProject(ctx, projectID)
		if err != nil {
			return db.NotificationChannel{}, fmt.Errorf("count notification channels: %w", err)
		}
		if count >= int64(s.limits.MaxChannelsPerProject) {
			return db.NotificationChannel{}, ErrLimit
		}
	}

	encoded, err := json.Marshal(config)
	if err != nil {
		return db.NotificationChannel{}, fmt.Errorf("encode channel config: %w", err)
	}
	encrypted, err := s.cipher.Encrypt(encoded)
	if err != nil {
		return db.NotificationChannel{}, fmt.Errorf("encrypt channel config: %w", err)
	}

	created, err := s.queries.CreateNotificationChannel(ctx, db.CreateNotificationChannelParams{
		ProjectID: projectID,
		Name:      name,
		Kind:      kind,
		Config:    encrypted,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return db.NotificationChannel{}, ErrNameTaken
		}
		return db.NotificationChannel{}, fmt.Errorf("create notification channel: %w", err)
	}
	return created, nil
}

func (s *Service) List(ctx context.Context, userID, projectID uuid.UUID) ([]db.NotificationChannel, error) {
	if s.projects != nil {
		if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
			return nil, err
		}
	}
	channels, err := s.queries.ListNotificationChannelsByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list notification channels: %w", err)
	}
	return channels, nil
}

func (s *Service) Delete(ctx context.Context, userID, projectID, channelID uuid.UUID) error {
	if s.projects != nil {
		if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
			return err
		}
	}
	deleted, err := s.queries.DeleteNotificationChannel(ctx, db.DeleteNotificationChannelParams{
		ID:        channelID,
		ProjectID: projectID,
	})
	if err != nil {
		return fmt.Errorf("delete notification channel: %w", err)
	}
	if deleted == 0 {
		return ErrNotFound
	}
	return nil
}

// Resolve decrypts a channel for worker execution. The project scope prevents a
// job from reading another project's destination.
func (s *Service) Resolve(ctx context.Context, projectID, channelID uuid.UUID) (Resolved, error) {
	channel, err := s.queries.GetNotificationChannel(ctx, db.GetNotificationChannelParams{
		ID:        channelID,
		ProjectID: projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Resolved{}, ErrNotFound
	}
	if err != nil {
		return Resolved{}, fmt.Errorf("get notification channel: %w", err)
	}

	decrypted, err := s.cipher.Decrypt(channel.Config)
	if err != nil {
		return Resolved{}, fmt.Errorf("decrypt notification channel: %w", err)
	}
	config := map[string]string{}
	if err := json.Unmarshal(decrypted, &config); err != nil {
		return Resolved{}, fmt.Errorf("decode notification channel: %w", err)
	}
	return Resolved{ID: channel.ID, Kind: channel.Kind, Config: config}, nil
}

func normalize(kind string, config map[string]string) (string, map[string]string, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	clean := make(map[string]string, len(config))
	for key, value := range config {
		clean[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}

	switch kind {
	case KindSlack, KindDiscord:
		if err := validateWebhookURL(clean["webhook_url"]); err != nil {
			return "", nil, err
		}
		clean = onlyKeys(clean, "webhook_url")
	case KindTelegram:
		if clean["chat_id"] == "" {
			return "", nil, fmt.Errorf("%w: chat_id is required", ErrInvalidConfig)
		}
		clean = onlyKeys(clean, "chat_id")
	case KindSMS:
		if clean["to"] == "" {
			return "", nil, fmt.Errorf("%w: to is required", ErrInvalidConfig)
		}
		clean = onlyKeys(clean, "to", "from")
	case KindEmail:
		recipients := splitRecipients(clean["to"])
		if len(recipients) == 0 {
			return "", nil, fmt.Errorf("%w: at least one recipient is required", ErrInvalidConfig)
		}
		for _, recipient := range recipients {
			if !strings.Contains(recipient, "@") {
				return "", nil, fmt.Errorf("%w: %q is not a valid email", ErrInvalidConfig, recipient)
			}
		}
		clean["to"] = strings.Join(recipients, ",")
		clean = onlyKeys(clean, "to")
	default:
		return "", nil, ErrInvalidKind
	}
	return kind, clean, nil
}

func validateWebhookURL(value string) error {
	if value == "" {
		return fmt.Errorf("%w: webhook_url is required", ErrInvalidConfig)
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("%w: webhook_url must be an http or https URL", ErrInvalidConfig)
	}
	if parsed.User != nil {
		return fmt.Errorf("%w: webhook_url must not contain credentials", ErrInvalidConfig)
	}
	return nil
}

func onlyKeys(config map[string]string, keys ...string) map[string]string {
	allowed := make(map[string]bool, len(keys))
	for _, key := range keys {
		allowed[key] = true
	}
	result := make(map[string]string, len(keys))
	for key, value := range config {
		if allowed[key] && value != "" {
			result[key] = value
		}
	}
	return result
}

func splitRecipients(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
