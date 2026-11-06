package project

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"relayforge/internal/database/db"
)

const (
	maxProjectNameLength  = 100
	maxQueueNameLength    = 100
	maxDefaultMaxAttempts = 100
	maxJobRetentionDays   = 3650
	maxConcurrencyLimit   = 1000
	maxRateLimitPerMinute = 1_000_000
	maxJobTimeoutSeconds  = 86_400
)

var (
	ErrProjectLimit            = errors.New("project limit reached for this account")
	ErrInvalidName             = errors.New("project name must be between 1 and 100 characters")
	ErrProjectNotFound         = errors.New("project not found")
	ErrEmptyUpdate             = errors.New("update must contain at least one field")
	ErrInvalidDefaultQueueName = errors.New("default queue name must be between 1 and 100 characters")
	ErrInvalidDefaultAttempts  = errors.New("default max attempts must be between 1 and 100")
	ErrInvalidRetentionDays    = errors.New("job retention days must be between 0 and 3650")
	ErrInvalidMaxConcurrency   = errors.New("max concurrency must be between 0 and 1000")
	ErrInvalidRateLimit        = errors.New("rate limit per minute must be between 0 and 1000000")
	ErrInvalidJobTimeout       = errors.New("default job timeout seconds must be between 0 and 86400")
)

// UpdateInput uses nil fields to represent values that should not change.
type UpdateInput struct {
	Name                     *string
	DefaultQueueName         *string
	DefaultMaxAttempts       *int32
	DefaultPriority          *int32
	JobRetentionDays         *int32
	MaxConcurrency           *int32
	RateLimitPerMinute       *int32
	DefaultJobTimeoutSeconds *int32
}

func (i UpdateInput) Empty() bool {
	return i.Name == nil &&
		i.DefaultQueueName == nil &&
		i.DefaultMaxAttempts == nil &&
		i.DefaultPriority == nil &&
		i.JobRetentionDays == nil &&
		i.MaxConcurrency == nil &&
		i.RateLimitPerMinute == nil &&
		i.DefaultJobTimeoutSeconds == nil
}

// Limits caps per-account and per-project resource usage. A zero value means
// unlimited, which is useful for development.
type Limits struct {
	MaxProjectsPerUser int
}

type Service struct {
	queries *db.Queries
	limits  Limits
}

func NewService(queries *db.Queries, limits Limits) *Service {
	return &Service{queries: queries, limits: limits}
}

func (s *Service) Create(ctx context.Context, userID uuid.UUID, name string) (db.Project, error) {
	name, err := validateName(name)
	if err != nil {
		return db.Project{}, err
	}

	if s.limits.MaxProjectsPerUser > 0 {
		count, err := s.queries.CountProjectsByUser(ctx, userID)
		if err != nil {
			return db.Project{}, fmt.Errorf("count projects: %w", err)
		}
		if count >= int64(s.limits.MaxProjectsPerUser) {
			return db.Project{}, ErrProjectLimit
		}
	}

	project, err := s.queries.CreateProject(ctx, db.CreateProjectParams{
		UserID: userID,
		Name:   name,
	})
	if err != nil {
		return db.Project{}, fmt.Errorf("create project: %w", err)
	}
	return project, nil
}

func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]db.Project, error) {
	projects, err := s.queries.ListProjectsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	return projects, nil
}

func (s *Service) Get(ctx context.Context, userID, projectID uuid.UUID) (db.Project, error) {
	project, err := s.queries.GetProject(ctx, projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Project{}, ErrProjectNotFound
	}
	if err != nil {
		return db.Project{}, fmt.Errorf("get project: %w", err)
	}
	if project.UserID != userID {
		return db.Project{}, ErrProjectNotFound
	}
	return project, nil
}

func (s *Service) Update(ctx context.Context, userID, projectID uuid.UUID, input UpdateInput) (db.Project, error) {
	params, err := input.params(projectID, userID)
	if err != nil {
		return db.Project{}, err
	}

	project, err := s.queries.UpdateProject(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Project{}, ErrProjectNotFound
	}
	if err != nil {
		return db.Project{}, fmt.Errorf("update project: %w", err)
	}
	return project, nil
}

func (i UpdateInput) params(projectID, userID uuid.UUID) (db.UpdateProjectParams, error) {
	if i.Empty() {
		return db.UpdateProjectParams{}, ErrEmptyUpdate
	}

	params := db.UpdateProjectParams{
		ID:     projectID,
		UserID: userID,
	}

	if i.Name != nil {
		name, err := validateName(*i.Name)
		if err != nil {
			return db.UpdateProjectParams{}, err
		}
		params.Name = &name
	}

	if i.DefaultQueueName != nil {
		queueName := strings.TrimSpace(*i.DefaultQueueName)
		if queueName == "" || len(queueName) > maxQueueNameLength {
			return db.UpdateProjectParams{}, ErrInvalidDefaultQueueName
		}
		params.DefaultQueueName = &queueName
	}

	if i.DefaultMaxAttempts != nil {
		if *i.DefaultMaxAttempts < 1 || *i.DefaultMaxAttempts > maxDefaultMaxAttempts {
			return db.UpdateProjectParams{}, ErrInvalidDefaultAttempts
		}
		params.DefaultMaxAttempts = i.DefaultMaxAttempts
	}

	if i.DefaultPriority != nil {
		params.DefaultPriority = i.DefaultPriority
	}

	if i.JobRetentionDays != nil {
		if *i.JobRetentionDays < 0 || *i.JobRetentionDays > maxJobRetentionDays {
			return db.UpdateProjectParams{}, ErrInvalidRetentionDays
		}
		params.JobRetentionDays = i.JobRetentionDays
	}

	if i.MaxConcurrency != nil {
		if *i.MaxConcurrency < 0 || *i.MaxConcurrency > maxConcurrencyLimit {
			return db.UpdateProjectParams{}, ErrInvalidMaxConcurrency
		}
		params.MaxConcurrency = i.MaxConcurrency
	}

	if i.RateLimitPerMinute != nil {
		if *i.RateLimitPerMinute < 0 || *i.RateLimitPerMinute > maxRateLimitPerMinute {
			return db.UpdateProjectParams{}, ErrInvalidRateLimit
		}
		params.RateLimitPerMinute = i.RateLimitPerMinute
	}

	if i.DefaultJobTimeoutSeconds != nil {
		if *i.DefaultJobTimeoutSeconds < 0 || *i.DefaultJobTimeoutSeconds > maxJobTimeoutSeconds {
			return db.UpdateProjectParams{}, ErrInvalidJobTimeout
		}
		params.DefaultJobTimeoutSeconds = i.DefaultJobTimeoutSeconds
	}

	return params, nil
}

// Delete archives a project without deleting its jobs, executions, or other history.
func (s *Service) Delete(ctx context.Context, userID, projectID uuid.UUID) error {
	if _, err := s.queries.ArchiveProject(ctx, db.ArchiveProjectParams{
		ID:     projectID,
		UserID: userID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrProjectNotFound
		}
		return fmt.Errorf("archive project: %w", err)
	}
	return nil
}

func (s *Service) GetOwned(ctx context.Context, userID, projectID uuid.UUID) (db.Project, error) {
	return s.Get(ctx, userID, projectID)
}

func validateName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" || len(name) > maxProjectNameLength {
		return "", ErrInvalidName
	}
	return name, nil
}
