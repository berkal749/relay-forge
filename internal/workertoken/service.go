package workertoken

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
	Prefix            = "rf_worker_"
	tokenBytes        = 32
	tokenPrefixLength = 16
	maxNameLength     = 100
	maxExpiryDays     = 3650
)

var (
	ErrInvalidName   = errors.New("worker token name must be between 1 and 100 characters")
	ErrInvalidExpiry = errors.New("worker token expiry must be between 1 and 3650 days")
	ErrInvalidToken  = errors.New("invalid worker token")
	ErrTokenNotFound = errors.New("worker token not found")
)

type Service struct {
	queries  *db.Queries
	projects *project.Service
	pepper   []byte
}

type CreatedToken struct {
	Token    db.WorkerToken
	RawToken string
}

func NewService(queries *db.Queries, projects *project.Service, pepper string) *Service {
	return &Service{
		queries:  queries,
		projects: projects,
		pepper:   []byte(pepper),
	}
}

func (s *Service) Create(ctx context.Context, userID, projectID uuid.UUID, name string, expiresInDays *int32) (CreatedToken, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > maxNameLength {
		return CreatedToken{}, ErrInvalidName
	}

	var expiresAt *time.Time
	if expiresInDays != nil {
		if *expiresInDays < 1 || *expiresInDays > maxExpiryDays {
			return CreatedToken{}, ErrInvalidExpiry
		}
		value := time.Now().Add(time.Duration(*expiresInDays) * 24 * time.Hour)
		expiresAt = &value
	}

	if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
		return CreatedToken{}, err
	}

	rawToken, err := generateToken()
	if err != nil {
		return CreatedToken{}, err
	}

	token, err := s.queries.CreateWorkerToken(ctx, db.CreateWorkerTokenParams{
		ProjectID:   projectID,
		TokenHash:   secret.HMAC(s.pepper, rawToken),
		TokenPrefix: secret.Prefix(rawToken, tokenPrefixLength),
		Name:        name,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		return CreatedToken{}, fmt.Errorf("create worker token: %w", err)
	}

	return CreatedToken{Token: token, RawToken: rawToken}, nil
}

func (s *Service) List(ctx context.Context, userID, projectID uuid.UUID) ([]db.WorkerToken, error) {
	if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
		return nil, err
	}

	tokens, err := s.queries.ListWorkerTokensByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list worker tokens: %w", err)
	}
	return tokens, nil
}

func (s *Service) Revoke(ctx context.Context, userID, projectID, tokenID uuid.UUID) error {
	if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
		return err
	}

	if _, err := s.queries.RevokeWorkerToken(ctx, db.RevokeWorkerTokenParams{
		ID:        tokenID,
		ProjectID: projectID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTokenNotFound
		}
		return fmt.Errorf("revoke worker token: %w", err)
	}
	return nil
}

// Authenticate validates a raw worker token and ensures it belongs to projectID.
func (s *Service) Authenticate(ctx context.Context, projectID uuid.UUID, rawToken string) (db.WorkerToken, error) {
	if rawToken == "" || !strings.HasPrefix(rawToken, Prefix) {
		return db.WorkerToken{}, ErrInvalidToken
	}

	token, err := s.queries.GetWorkerTokenByHash(ctx, secret.HMAC(s.pepper, rawToken))
	if errors.Is(err, pgx.ErrNoRows) {
		return db.WorkerToken{}, ErrInvalidToken
	}
	if err != nil {
		return db.WorkerToken{}, fmt.Errorf("get worker token: %w", err)
	}
	if token.ProjectID != projectID {
		return db.WorkerToken{}, ErrInvalidToken
	}

	if err := s.queries.TouchWorkerToken(ctx, token.ID); err != nil && ctx.Err() == nil {
		slog.Warn("update worker token last used", "error", err, "token_id", token.ID)
	}

	return token, nil
}

func generateToken() (string, error) {
	random := make([]byte, tokenBytes)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate worker token: %w", err)
	}
	return Prefix + base64.RawURLEncoding.EncodeToString(random), nil
}
