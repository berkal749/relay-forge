package apikey

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"relayforge/internal/database/db"
	"relayforge/internal/project"
	"relayforge/internal/secret"
)

const (
	Prefix              = "rf_live_"
	keyBytes            = 32
	keyPrefixLength     = 12
	maxAPIKeyNameLength = 100
	maxExpiryDays       = 3650
)

var (
	ErrInvalidName   = errors.New("API key name must be between 1 and 100 characters")
	ErrInvalidExpiry = errors.New("API key expiry must be between 1 and 3650 days")
	ErrInvalidKey    = errors.New("invalid API key")
	ErrKeyNotFound   = errors.New("API key not found")
)

type Service struct {
	queries  *db.Queries
	projects *project.Service
	pepper   []byte
}

type CreatedKey struct {
	Key    db.ApiKey
	RawKey string
}

func NewService(queries *db.Queries, projects *project.Service, pepper string) *Service {
	return &Service{
		queries:  queries,
		projects: projects,
		pepper:   []byte(pepper),
	}
}

func (s *Service) Create(ctx context.Context, userID, projectID uuid.UUID, name string, expiresInDays *int32) (CreatedKey, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > maxAPIKeyNameLength {
		return CreatedKey{}, ErrInvalidName
	}

	var expiresAt *time.Time
	if expiresInDays != nil {
		if *expiresInDays < 1 || *expiresInDays > maxExpiryDays {
			return CreatedKey{}, ErrInvalidExpiry
		}
		value := time.Now().Add(time.Duration(*expiresInDays) * 24 * time.Hour)
		expiresAt = &value
	}

	if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
		return CreatedKey{}, err
	}

	rawKey, err := generateKey()
	if err != nil {
		return CreatedKey{}, err
	}

	key, err := s.queries.CreateAPIKey(ctx, db.CreateAPIKeyParams{
		ProjectID: projectID,
		KeyHash:   hashKey(s.pepper, rawKey),
		KeyPrefix: keyPrefix(rawKey),
		Name:      name,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return CreatedKey{}, fmt.Errorf("create API key: %w", err)
	}

	return CreatedKey{Key: key, RawKey: rawKey}, nil
}

func (s *Service) List(ctx context.Context, userID, projectID uuid.UUID) ([]db.ApiKey, error) {
	if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
		return nil, err
	}

	keys, err := s.queries.ListAPIKeysByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list API keys: %w", err)
	}
	return keys, nil
}

func (s *Service) Revoke(ctx context.Context, userID, projectID, keyID uuid.UUID) error {
	if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
		return err
	}

	if _, err := s.queries.RevokeAPIKey(ctx, db.RevokeAPIKeyParams{
		ID:        keyID,
		ProjectID: projectID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrKeyNotFound
		}
		return fmt.Errorf("revoke API key: %w", err)
	}
	return nil
}

func (s *Service) Authenticate(ctx context.Context, rawKey string) (db.ApiKey, error) {
	if rawKey == "" || !strings.HasPrefix(rawKey, Prefix) {
		return db.ApiKey{}, ErrInvalidKey
	}

	hashed := hashKey(s.pepper, rawKey)
	key, err := s.queries.GetAPIKeyByHash(ctx, hashed)
	if errors.Is(err, pgx.ErrNoRows) {
		key, err = s.authenticateLegacy(ctx, rawKey, hashed)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return db.ApiKey{}, ErrInvalidKey
	}
	if err != nil {
		return db.ApiKey{}, fmt.Errorf("get API key: %w", err)
	}

	if err := s.queries.TouchAPIKey(ctx, key.ID); err != nil && ctx.Err() == nil {
		slog.Warn("update API key last used", "error", err, "key_id", key.ID)
	}

	return key, nil
}

// authenticateLegacy accepts keys hashed with the pre-pepper SHA-256 scheme and
// upgrades them to the current HMAC hash.
func (s *Service) authenticateLegacy(ctx context.Context, rawKey, hashed string) (db.ApiKey, error) {
	key, err := s.queries.GetAPIKeyByHash(ctx, legacyHash(rawKey))
	if err != nil {
		return db.ApiKey{}, err
	}

	if err := s.queries.UpdateAPIKeyHash(ctx, db.UpdateAPIKeyHashParams{
		ID:      key.ID,
		KeyHash: hashed,
	}); err != nil {
		slog.Warn("upgrade legacy API key hash", "error", err, "key_id", key.ID)
	} else {
		key.KeyHash = hashed
	}

	return key, nil
}

func generateKey() (string, error) {
	random := make([]byte, keyBytes)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate API key: %w", err)
	}
	return Prefix + base64.RawURLEncoding.EncodeToString(random), nil
}

func hashKey(pepper []byte, rawKey string) string {
	return secret.HMAC(pepper, rawKey)
}

func legacyHash(rawKey string) string {
	return secret.SHA256(rawKey)
}

func keyPrefix(rawKey string) string {
	return secret.Prefix(rawKey, keyPrefixLength)
}
