package workerpool

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"relayforge/internal/database/db"
	"relayforge/internal/project"
	"relayforge/internal/workertoken"
)

const (
	maxNameLength      = 100
	maxConcurrency     = 1000
	DefaultConcurrency = 4
	StatusActive       = "active"
	StatusPaused       = "paused"
	defaultEnrollTTL   = 15 * time.Minute
)

var (
	ErrInvalidName        = errors.New("worker pool name must be between 1 and 100 characters")
	ErrInvalidConcurrency = errors.New("worker pool concurrency must be between 1 and 1000")
	ErrInvalidStatus      = errors.New("worker pool status must be active or paused")
	ErrEmptyUpdate        = errors.New("update must contain at least one field")
	ErrPoolNotFound       = errors.New("worker pool not found")
	ErrNameTaken          = errors.New("a worker pool with that name already exists")
)

type UpdateInput struct {
	Name        *string
	Concurrency *int32
	Status      *string
}

func (i UpdateInput) Empty() bool {
	return i.Name == nil && i.Concurrency == nil && i.Status == nil
}

type Service struct {
	queries  *db.Queries
	projects *project.Service
	tokens   *workertoken.Service
}

func NewService(queries *db.Queries, projects *project.Service, tokens *workertoken.Service) *Service {
	return &Service{queries: queries, projects: projects, tokens: tokens}
}

func (s *Service) Create(ctx context.Context, userID, projectID uuid.UUID, name string, concurrency int32) (db.WorkerPool, error) {
	name, err := validateName(name)
	if err != nil {
		return db.WorkerPool{}, err
	}
	if concurrency == 0 {
		return db.WorkerPool{}, ErrInvalidConcurrency
	}
	if err := validateConcurrency(concurrency); err != nil {
		return db.WorkerPool{}, err
	}

	if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
		return db.WorkerPool{}, err
	}

	pool, err := s.queries.CreateWorkerPool(ctx, db.CreateWorkerPoolParams{
		ProjectID:   projectID,
		Name:        name,
		Concurrency: concurrency,
		Status:      StatusActive,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return db.WorkerPool{}, ErrNameTaken
		}
		return db.WorkerPool{}, fmt.Errorf("create worker pool: %w", err)
	}
	return pool, nil
}

func (s *Service) List(ctx context.Context, userID, projectID uuid.UUID) ([]db.WorkerPool, error) {
	if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
		return nil, err
	}

	pools, err := s.queries.ListWorkerPoolsByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list worker pools: %w", err)
	}
	return pools, nil
}

func (s *Service) Update(ctx context.Context, userID, projectID, poolID uuid.UUID, input UpdateInput) (db.WorkerPool, error) {
	if input.Empty() {
		return db.WorkerPool{}, ErrEmptyUpdate
	}

	params := db.UpdateWorkerPoolParams{
		ID:        poolID,
		ProjectID: projectID,
	}

	if input.Name != nil {
		name, err := validateName(*input.Name)
		if err != nil {
			return db.WorkerPool{}, err
		}
		params.Name = &name
	}
	if input.Concurrency != nil {
		if err := validateConcurrency(*input.Concurrency); err != nil {
			return db.WorkerPool{}, err
		}
		params.Concurrency = input.Concurrency
	}
	if input.Status != nil {
		status := strings.TrimSpace(*input.Status)
		if status != StatusActive && status != StatusPaused {
			return db.WorkerPool{}, ErrInvalidStatus
		}
		params.Status = &status
	}

	if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
		return db.WorkerPool{}, err
	}

	pool, err := s.queries.UpdateWorkerPool(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.WorkerPool{}, ErrPoolNotFound
	}
	if err != nil {
		if isUniqueViolation(err) {
			return db.WorkerPool{}, ErrNameTaken
		}
		return db.WorkerPool{}, fmt.Errorf("update worker pool: %w", err)
	}
	return pool, nil
}

func (s *Service) Delete(ctx context.Context, userID, projectID, poolID uuid.UUID) error {
	if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
		return err
	}

	deleted, err := s.queries.DeleteWorkerPool(ctx, db.DeleteWorkerPoolParams{
		ID:        poolID,
		ProjectID: projectID,
	})
	if err != nil {
		return fmt.Errorf("delete worker pool: %w", err)
	}
	if deleted == 0 {
		return ErrPoolNotFound
	}
	return nil
}

func (s *Service) CreateEnrollmentToken(ctx context.Context, userID, projectID, poolID uuid.UUID, name string, ttl time.Duration) (workertoken.CreatedToken, error) {
	if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
		return workertoken.CreatedToken{}, err
	}

	pool, err := s.queries.GetWorkerPool(ctx, db.GetWorkerPoolParams{
		ID:        poolID,
		ProjectID: projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return workertoken.CreatedToken{}, ErrPoolNotFound
	}
	if err != nil {
		return workertoken.CreatedToken{}, fmt.Errorf("get worker pool: %w", err)
	}
	if ttl == 0 {
		ttl = defaultEnrollTTL
	}
	if name == "" {
		name = pool.Name + " enrollment"
	}

	token, err := s.tokens.CreateEnrollmentToken(ctx, projectID, poolID, name, ttl)
	if err != nil {
		return workertoken.CreatedToken{}, err
	}
	return token, nil
}

func validateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > maxNameLength {
		return "", ErrInvalidName
	}
	return name, nil
}

func validateConcurrency(concurrency int32) error {
	if concurrency < 1 || concurrency > maxConcurrency {
		return ErrInvalidConcurrency
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
