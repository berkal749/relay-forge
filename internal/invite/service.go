// Package invite manages single-use registration invites for invite-only beta
// access. Raw invite tokens are returned once at creation and stored as
// SHA-256 hashes.
package invite

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"

	"relayforge/internal/database/db"
)

const (
	RoleUser  = "user"
	RoleAdmin = "admin"

	DefaultTTL = 7 * 24 * time.Hour
	MinTTL     = time.Hour
	MaxTTL     = 30 * 24 * time.Hour

	tokenBytes = 32
)

var (
	ErrInvalidEmail = errors.New("invalid invite email")
	ErrInvalidRole  = errors.New("invite role must be user or admin")
	ErrInvalidTTL   = errors.New("invite expiry must be between 1 hour and 30 days")
	ErrNotFound     = errors.New("invite not found")
)

type Service struct {
	queries    *db.Queries
	defaultTTL time.Duration
}

type CreateInput struct {
	Email     string
	Role      string
	CreatedBy *uuid.UUID
	TTL       time.Duration
}

type Created struct {
	Invite db.Invite
	Token  string
}

func NewService(queries *db.Queries, defaultTTL time.Duration) *Service {
	if defaultTTL <= 0 {
		defaultTTL = DefaultTTL
	}
	return &Service{queries: queries, defaultTTL: defaultTTL}
}

func (s *Service) Create(ctx context.Context, input CreateInput) (Created, error) {
	role := strings.TrimSpace(input.Role)
	if role == "" {
		role = RoleUser
	}
	if role != RoleUser && role != RoleAdmin {
		return Created{}, ErrInvalidRole
	}

	var email *string
	if strings.TrimSpace(input.Email) != "" {
		normalized, err := normalizeEmail(input.Email)
		if err != nil {
			return Created{}, err
		}
		email = &normalized
	}

	ttl := input.TTL
	if ttl == 0 {
		ttl = s.defaultTTL
	}
	if ttl < MinTTL || ttl > MaxTTL {
		return Created{}, ErrInvalidTTL
	}

	token, tokenHash, err := newToken()
	if err != nil {
		return Created{}, err
	}

	created, err := s.queries.CreateInvite(ctx, db.CreateInviteParams{
		TokenHash: tokenHash,
		Email:     email,
		Role:      role,
		CreatedBy: input.CreatedBy,
		ExpiresAt: time.Now().Add(ttl),
	})
	if err != nil {
		return Created{}, fmt.Errorf("create invite: %w", err)
	}

	return Created{Invite: created, Token: token}, nil
}

func (s *Service) List(ctx context.Context, limit int32) ([]db.Invite, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	invites, err := s.queries.ListInvites(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	return invites, nil
}

func (s *Service) Revoke(ctx context.Context, id uuid.UUID) error {
	deleted, err := s.queries.DeleteInvite(ctx, id)
	if err != nil {
		return fmt.Errorf("delete invite: %w", err)
	}
	if deleted == 0 {
		return ErrNotFound
	}
	return nil
}

// HashToken hashes a raw invite token for storage and lookup.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newToken() (token string, tokenHash string, err error) {
	random := make([]byte, tokenBytes)
	if _, err := rand.Read(random); err != nil {
		return "", "", fmt.Errorf("generate invite token: %w", err)
	}
	token = "rf_invite_" + base64.RawURLEncoding.EncodeToString(random)
	return token, HashToken(token), nil
}

func normalizeEmail(value string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(value))
	if email == "" || len(email) > 254 {
		return "", ErrInvalidEmail
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return "", ErrInvalidEmail
	}
	return email, nil
}
