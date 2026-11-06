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
	Prefix           = "rf_worker_"
	EnrollmentPrefix = "rf_enroll_"

	tokenBytes        = 32
	tokenPrefixLength = 16
	maxNameLength     = 100
	maxExpiryDays     = 3650
	maxEnrollmentTTL  = 24 * time.Hour
)

var (
	ErrInvalidName   = errors.New("worker token name must be between 1 and 100 characters")
	ErrInvalidExpiry = errors.New("worker token expiry must be between 1 and 3650 days")
	ErrInvalidTTL    = errors.New("enrollment token lifetime must be between 1 minute and 24 hours")
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

// Enrollment is the result of redeeming a single-use enrollment token. The
// returned token is scoped to the pool that issued the enrollment token.
type Enrollment struct {
	Pool      db.WorkerPool
	Token     CreatedToken
	ProjectID uuid.UUID
}

func NewService(queries *db.Queries, projects *project.Service, pepper string) *Service {
	return &Service{
		queries:  queries,
		projects: projects,
		pepper:   []byte(pepper),
	}
}

func (s *Service) Create(ctx context.Context, userID, projectID uuid.UUID, name string, expiresInDays *int32) (CreatedToken, error) {
	name, err := validateName(name)
	if err != nil {
		return CreatedToken{}, err
	}

	expiresAt, err := expiresInDaysToTime(expiresInDays)
	if err != nil {
		return CreatedToken{}, err
	}

	if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
		return CreatedToken{}, err
	}

	return s.create(ctx, projectID, nil, "worker", name, expiresAt)
}

// CreateForPool issues a worker token bound to a pool without performing an
// ownership check. Callers are responsible for authorizing the request.
func (s *Service) CreateForPool(ctx context.Context, projectID, poolID uuid.UUID, name string) (CreatedToken, error) {
	name, err := validateName(name)
	if err != nil {
		return CreatedToken{}, err
	}
	return s.create(ctx, projectID, &poolID, "worker", name, nil)
}

// CreateEnrollmentToken issues a short-lived, single-use token that a worker can
// exchange for a pool-scoped worker token. Callers are responsible for
// authorizing the request.
func (s *Service) CreateEnrollmentToken(ctx context.Context, projectID, poolID uuid.UUID, name string, ttl time.Duration) (CreatedToken, error) {
	name, err := validateName(name)
	if err != nil {
		return CreatedToken{}, err
	}
	if ttl < time.Minute || ttl > maxEnrollmentTTL {
		return CreatedToken{}, ErrInvalidTTL
	}
	expiresAt := time.Now().Add(ttl)
	return s.create(ctx, projectID, &poolID, "enrollment", name, &expiresAt)
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

// Enroll redeems a single-use enrollment token for a pool-scoped worker token.
// The enrollment token is consumed atomically, so concurrent redemption
// attempts cannot both succeed.
func (s *Service) Enroll(ctx context.Context, rawToken string) (Enrollment, error) {
	if rawToken == "" || !strings.HasPrefix(rawToken, EnrollmentPrefix) {
		return Enrollment{}, ErrInvalidToken
	}

	enrollmentToken, err := s.queries.GetEnrollmentTokenByHash(ctx, secret.HMAC(s.pepper, rawToken))
	if errors.Is(err, pgx.ErrNoRows) {
		return Enrollment{}, ErrInvalidToken
	}
	if err != nil {
		return Enrollment{}, fmt.Errorf("get enrollment token: %w", err)
	}
	if enrollmentToken.PoolID == nil {
		return Enrollment{}, ErrInvalidToken
	}

	if _, err := s.queries.ConsumeEnrollmentToken(ctx, enrollmentToken.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Enrollment{}, ErrInvalidToken
		}
		return Enrollment{}, fmt.Errorf("consume enrollment token: %w", err)
	}

	pool, err := s.queries.GetWorkerPool(ctx, db.GetWorkerPoolParams{
		ID:        *enrollmentToken.PoolID,
		ProjectID: enrollmentToken.ProjectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Enrollment{}, ErrInvalidToken
	}
	if err != nil {
		return Enrollment{}, fmt.Errorf("get worker pool: %w", err)
	}

	created, err := s.CreateForPool(ctx, pool.ProjectID, pool.ID, pool.Name)
	if err != nil {
		return Enrollment{}, err
	}

	return Enrollment{
		Pool:      pool,
		Token:     created,
		ProjectID: pool.ProjectID,
	}, nil
}

func (s *Service) create(ctx context.Context, projectID uuid.UUID, poolID *uuid.UUID, kind, name string, expiresAt *time.Time) (CreatedToken, error) {
	prefix := Prefix
	if kind == "enrollment" {
		prefix = EnrollmentPrefix
	}

	rawToken, err := generateToken(prefix)
	if err != nil {
		return CreatedToken{}, err
	}

	token, err := s.queries.CreateWorkerToken(ctx, db.CreateWorkerTokenParams{
		ProjectID:   projectID,
		PoolID:      poolID,
		Kind:        kind,
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

func validateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > maxNameLength {
		return "", ErrInvalidName
	}
	return name, nil
}

func expiresInDaysToTime(expiresInDays *int32) (*time.Time, error) {
	if expiresInDays == nil {
		return nil, nil
	}
	if *expiresInDays < 1 || *expiresInDays > maxExpiryDays {
		return nil, ErrInvalidExpiry
	}
	value := time.Now().Add(time.Duration(*expiresInDays) * 24 * time.Hour)
	return &value, nil
}

func generateToken(prefix string) (string, error) {
	random := make([]byte, tokenBytes)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate worker token: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(random), nil
}
