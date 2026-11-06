package workerpool

import (
	"context"
	"errors"
	"fmt"
	"net"
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

	BackoffExponential = "exponential"
	BackoffFixed       = "fixed"

	maxQueues        = 50
	maxQueueNameLen  = 100
	maxJobTimeoutSec = 86400
	maxAttempts      = 100
	maxRetryDelaySec = 86400
	maxAllowedCIDRs  = 50

	defaultEnrollTTL = 15 * time.Minute
)

var (
	ErrInvalidName         = errors.New("worker pool name must be between 1 and 100 characters")
	ErrInvalidConcurrency  = errors.New("worker pool concurrency must be between 1 and 1000")
	ErrInvalidStatus       = errors.New("worker pool status must be active or paused")
	ErrInvalidQueueNames   = errors.New("worker pool queues must be 1-100 characters each and contain at most 50 queues")
	ErrInvalidJobTimeout   = errors.New("worker pool job timeout must be between 0 and 86400 seconds")
	ErrInvalidMaxAttempts  = errors.New("worker pool max attempts must be between 0 and 100")
	ErrInvalidRetryBackoff = errors.New("worker pool retry backoff must be fixed or exponential")
	ErrInvalidRetryDelay   = errors.New("worker pool retry delays must be between 0 and 86400 seconds, with max >= base")
	ErrInvalidAllowedCIDRs = errors.New("worker pool allowed CIDRs must be valid IPv4/IPv6 CIDRs and contain at most 50 entries")
	ErrEmptyUpdate         = errors.New("update must contain at least one field")
	ErrPoolNotFound        = errors.New("worker pool not found")
	ErrNameTaken           = errors.New("a worker pool with that name already exists")
	ErrPoolLimit           = errors.New("worker pool limit reached for this project")
	ErrActivePoolLimit     = errors.New("active worker pool limit reached for this project")
	ErrConcurrencyLimit    = errors.New("worker concurrency limit reached for this project")
)

// Limits caps per-project worker resources. A zero value means unlimited.
type Limits struct {
	MaxPoolsPerProject       int
	MaxActivePoolsPerProject int
	MaxConcurrencyPerProject int
}

type CreateInput struct {
	Name                  string
	Concurrency           int32
	QueueNames            []string
	JobTimeoutSeconds     int32
	MaxAttempts           int32
	RetryBackoff          string
	RetryBaseDelaySeconds int32
	RetryMaxDelaySeconds  int32
	AllowedCIDRs          []string
}

type UpdateInput struct {
	Name                  *string
	Concurrency           *int32
	Status                *string
	QueueNames            *[]string
	JobTimeoutSeconds     *int32
	MaxAttempts           *int32
	RetryBackoff          *string
	RetryBaseDelaySeconds *int32
	RetryMaxDelaySeconds  *int32
	AllowedCIDRs          *[]string
}

func (i UpdateInput) Empty() bool {
	return i.Name == nil &&
		i.Concurrency == nil &&
		i.Status == nil &&
		i.QueueNames == nil &&
		i.JobTimeoutSeconds == nil &&
		i.MaxAttempts == nil &&
		i.RetryBackoff == nil &&
		i.RetryBaseDelaySeconds == nil &&
		i.RetryMaxDelaySeconds == nil &&
		i.AllowedCIDRs == nil
}

type Service struct {
	queries  *db.Queries
	projects *project.Service
	tokens   *workertoken.Service
	limits   Limits
}

func NewService(queries *db.Queries, projects *project.Service, tokens *workertoken.Service, limits Limits) *Service {
	return &Service{queries: queries, projects: projects, tokens: tokens, limits: limits}
}

// Usage reports current project quota consumption for the dashboard.
type Usage struct {
	PoolCount      int64 `json:"pool_count"`
	MaxPools       int   `json:"max_pools"`
	ActiveCount    int64 `json:"active_pool_count"`
	MaxActivePools int   `json:"max_active_pools"`
	Concurrency    int64 `json:"concurrency"`
	MaxConcurrency int   `json:"max_concurrency"`
}

func (s *Service) Usage(ctx context.Context, userID, projectID uuid.UUID) (Usage, error) {
	if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
		return Usage{}, err
	}
	pools, err := s.queries.CountWorkerPoolsByProject(ctx, projectID)
	if err != nil {
		return Usage{}, fmt.Errorf("count worker pools: %w", err)
	}
	active, err := s.queries.CountActiveWorkerPoolsByProject(ctx, projectID)
	if err != nil {
		return Usage{}, fmt.Errorf("count active worker pools: %w", err)
	}
	concurrency, err := s.queries.SumWorkerConcurrencyByProject(ctx, projectID)
	if err != nil {
		return Usage{}, fmt.Errorf("sum worker concurrency: %w", err)
	}
	return Usage{
		PoolCount:      pools,
		MaxPools:       s.limits.MaxPoolsPerProject,
		ActiveCount:    active,
		MaxActivePools: s.limits.MaxActivePoolsPerProject,
		Concurrency:    concurrency,
		MaxConcurrency: s.limits.MaxConcurrencyPerProject,
	}, nil
}

func (s *Service) checkUpdateQuota(ctx context.Context, projectID uuid.UUID, existing db.WorkerPool, input UpdateInput) error {
	newStatus := existing.Status
	if input.Status != nil {
		newStatus = strings.TrimSpace(*input.Status)
	}
	if newStatus != StatusActive {
		return nil
	}

	if s.limits.MaxActivePoolsPerProject > 0 && existing.Status != StatusActive {
		count, err := s.queries.CountActiveWorkerPoolsByProjectExcluding(ctx, db.CountActiveWorkerPoolsByProjectExcludingParams{
			ProjectID: projectID,
			ID:        existing.ID,
		})
		if err != nil {
			return fmt.Errorf("count active worker pools: %w", err)
		}
		if count >= int64(s.limits.MaxActivePoolsPerProject) {
			return ErrActivePoolLimit
		}
	}

	if s.limits.MaxConcurrencyPerProject > 0 {
		newConcurrency := existing.Concurrency
		if input.Concurrency != nil {
			newConcurrency = *input.Concurrency
		}
		if newConcurrency != existing.Concurrency {
			total, err := s.queries.SumWorkerConcurrencyByProjectExcluding(ctx, db.SumWorkerConcurrencyByProjectExcludingParams{
				ProjectID: projectID,
				ID:        existing.ID,
			})
			if err != nil {
				return fmt.Errorf("sum worker concurrency: %w", err)
			}
			if total+int64(newConcurrency) > int64(s.limits.MaxConcurrencyPerProject) {
				return ErrConcurrencyLimit
			}
		}
	}
	return nil
}

func (s *Service) checkCreateQuota(ctx context.Context, projectID uuid.UUID, concurrency int32) error {
	if s.limits.MaxPoolsPerProject > 0 {
		count, err := s.queries.CountWorkerPoolsByProject(ctx, projectID)
		if err != nil {
			return fmt.Errorf("count worker pools: %w", err)
		}
		if count >= int64(s.limits.MaxPoolsPerProject) {
			return ErrPoolLimit
		}
	}
	if s.limits.MaxActivePoolsPerProject > 0 {
		count, err := s.queries.CountActiveWorkerPoolsByProject(ctx, projectID)
		if err != nil {
			return fmt.Errorf("count active worker pools: %w", err)
		}
		if count >= int64(s.limits.MaxActivePoolsPerProject) {
			return ErrActivePoolLimit
		}
	}
	if s.limits.MaxConcurrencyPerProject > 0 {
		total, err := s.queries.SumWorkerConcurrencyByProject(ctx, projectID)
		if err != nil {
			return fmt.Errorf("sum worker concurrency: %w", err)
		}
		if total+int64(concurrency) > int64(s.limits.MaxConcurrencyPerProject) {
			return ErrConcurrencyLimit
		}
	}
	return nil
}

func (s *Service) Create(ctx context.Context, userID, projectID uuid.UUID, input CreateInput) (db.WorkerPool, error) {
	name, err := validateName(input.Name)
	if err != nil {
		return db.WorkerPool{}, err
	}
	if input.Concurrency == 0 {
		input.Concurrency = DefaultConcurrency
	}
	if err := validateConcurrency(input.Concurrency); err != nil {
		return db.WorkerPool{}, err
	}
	if input.RetryBackoff == "" {
		input.RetryBackoff = BackoffExponential
	}
	queueNames, backoff, allowedCIDRs, err := validatePolicy(
		input.QueueNames,
		input.JobTimeoutSeconds,
		input.MaxAttempts,
		input.RetryBackoff,
		input.RetryBaseDelaySeconds,
		input.RetryMaxDelaySeconds,
		input.AllowedCIDRs,
	)
	if err != nil {
		return db.WorkerPool{}, err
	}

	if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
		return db.WorkerPool{}, err
	}
	if err := s.checkCreateQuota(ctx, projectID, input.Concurrency); err != nil {
		return db.WorkerPool{}, err
	}

	pool, err := s.queries.CreateWorkerPool(ctx, db.CreateWorkerPoolParams{
		ProjectID:             projectID,
		Name:                  name,
		Concurrency:           input.Concurrency,
		Status:                StatusActive,
		QueueNames:            queueNames,
		JobTimeoutSeconds:     input.JobTimeoutSeconds,
		MaxAttempts:           input.MaxAttempts,
		RetryBackoff:          backoff,
		RetryBaseDelaySeconds: input.RetryBaseDelaySeconds,
		RetryMaxDelaySeconds:  input.RetryMaxDelaySeconds,
		AllowedCidrs:          allowedCIDRs,
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

func (s *Service) Stats(ctx context.Context, userID, projectID uuid.UUID) ([]db.GetWorkerPoolStatsRow, error) {
	if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
		return nil, err
	}
	stats, err := s.queries.GetWorkerPoolStats(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("worker pool stats: %w", err)
	}
	return stats, nil
}

func (s *Service) Workers(ctx context.Context, userID, projectID uuid.UUID, poolID *uuid.UUID) ([]db.ListWorkersWithLoadRow, error) {
	if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
		return nil, err
	}
	workers, err := s.queries.ListWorkersWithLoad(ctx, db.ListWorkersWithLoadParams{
		ProjectID: projectID,
		PoolID:    poolID,
	})
	if err != nil {
		return nil, fmt.Errorf("list workers: %w", err)
	}
	return workers, nil
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
	if input.QueueNames != nil {
		queueNames, err := normalizeQueueNames(*input.QueueNames)
		if err != nil {
			return db.WorkerPool{}, err
		}
		params.QueueNames = queueNames
	}
	if input.JobTimeoutSeconds != nil {
		if err := validateJobTimeout(*input.JobTimeoutSeconds); err != nil {
			return db.WorkerPool{}, err
		}
		params.JobTimeoutSeconds = input.JobTimeoutSeconds
	}
	if input.MaxAttempts != nil {
		if err := validateMaxAttempts(*input.MaxAttempts); err != nil {
			return db.WorkerPool{}, err
		}
		params.MaxAttempts = input.MaxAttempts
	}
	if input.RetryBackoff != nil {
		backoff, err := validateBackoff(*input.RetryBackoff)
		if err != nil {
			return db.WorkerPool{}, err
		}
		params.RetryBackoff = &backoff
	}
	if input.RetryBaseDelaySeconds != nil {
		if err := validateDelay(*input.RetryBaseDelaySeconds); err != nil {
			return db.WorkerPool{}, err
		}
		params.RetryBaseDelaySeconds = input.RetryBaseDelaySeconds
	}
	if input.RetryMaxDelaySeconds != nil {
		if err := validateDelay(*input.RetryMaxDelaySeconds); err != nil {
			return db.WorkerPool{}, err
		}
		params.RetryMaxDelaySeconds = input.RetryMaxDelaySeconds
	}
	if input.AllowedCIDRs != nil {
		allowedCIDRs, err := normalizeCIDRs(*input.AllowedCIDRs)
		if err != nil {
			return db.WorkerPool{}, err
		}
		params.AllowedCidrs = allowedCIDRs
	}
	if input.RetryBaseDelaySeconds != nil && input.RetryMaxDelaySeconds != nil {
		if *input.RetryBaseDelaySeconds > 0 && *input.RetryMaxDelaySeconds > 0 &&
			*input.RetryMaxDelaySeconds < *input.RetryBaseDelaySeconds {
			return db.WorkerPool{}, ErrInvalidRetryDelay
		}
	}

	if _, err := s.projects.GetOwned(ctx, userID, projectID); err != nil {
		return db.WorkerPool{}, err
	}

	existing, err := s.queries.GetWorkerPool(ctx, db.GetWorkerPoolParams{
		ID:        poolID,
		ProjectID: projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.WorkerPool{}, ErrPoolNotFound
	}
	if err != nil {
		return db.WorkerPool{}, fmt.Errorf("get worker pool: %w", err)
	}
	if err := s.checkUpdateQuota(ctx, projectID, existing, input); err != nil {
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

func validatePolicy(queueNames []string, jobTimeoutSeconds, maxAttemptsValue int32, backoff string, baseDelaySeconds, maxDelaySeconds int32, cidrs []string) ([]string, string, []string, error) {
	normalizedQueues, err := normalizeQueueNames(queueNames)
	if err != nil {
		return nil, "", nil, err
	}
	if err := validateJobTimeout(jobTimeoutSeconds); err != nil {
		return nil, "", nil, err
	}
	if err := validateMaxAttempts(maxAttemptsValue); err != nil {
		return nil, "", nil, err
	}
	normalizedBackoff, err := validateBackoff(backoff)
	if err != nil {
		return nil, "", nil, err
	}
	if err := validateDelay(baseDelaySeconds); err != nil {
		return nil, "", nil, err
	}
	if err := validateDelay(maxDelaySeconds); err != nil {
		return nil, "", nil, err
	}
	if baseDelaySeconds > 0 && maxDelaySeconds > 0 && maxDelaySeconds < baseDelaySeconds {
		return nil, "", nil, ErrInvalidRetryDelay
	}
	normalizedCIDRs, err := normalizeCIDRs(cidrs)
	if err != nil {
		return nil, "", nil, err
	}
	return normalizedQueues, normalizedBackoff, normalizedCIDRs, nil
}

func normalizeQueueNames(queueNames []string) ([]string, error) {
	if len(queueNames) > maxQueues {
		return nil, ErrInvalidQueueNames
	}
	normalized := make([]string, 0, len(queueNames))
	seen := make(map[string]struct{}, len(queueNames))
	for _, queue := range queueNames {
		queue = strings.TrimSpace(queue)
		if queue == "" || len(queue) > maxQueueNameLen {
			return nil, ErrInvalidQueueNames
		}
		if _, ok := seen[queue]; ok {
			continue
		}
		seen[queue] = struct{}{}
		normalized = append(normalized, queue)
	}
	return normalized, nil
}

func normalizeCIDRs(cidrs []string) ([]string, error) {
	if len(cidrs) > maxAllowedCIDRs {
		return nil, ErrInvalidAllowedCIDRs
	}
	normalized := make([]string, 0, len(cidrs))
	seen := make(map[string]struct{}, len(cidrs))
	for _, cidr := range cidrs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			return nil, ErrInvalidAllowedCIDRs
		}
		if _, network, parseErr := net.ParseCIDR(cidr); parseErr == nil {
			cidr = network.String()
		} else if ip := net.ParseIP(cidr); ip != nil {
			if ip.To4() != nil {
				cidr = ip.String() + "/32"
			} else {
				cidr = ip.String() + "/128"
			}
		} else {
			return nil, ErrInvalidAllowedCIDRs
		}
		if _, ok := seen[cidr]; ok {
			continue
		}
		seen[cidr] = struct{}{}
		normalized = append(normalized, cidr)
	}
	return normalized, nil
}

func validateJobTimeout(seconds int32) error {
	if seconds < 0 || seconds > maxJobTimeoutSec {
		return ErrInvalidJobTimeout
	}
	return nil
}

func validateMaxAttempts(value int32) error {
	if value < 0 || value > maxAttempts {
		return ErrInvalidMaxAttempts
	}
	return nil
}

func validateBackoff(value string) (string, error) {
	value = strings.TrimSpace(value)
	switch value {
	case BackoffFixed, BackoffExponential:
		return value, nil
	default:
		return "", ErrInvalidRetryBackoff
	}
}

func validateDelay(seconds int32) error {
	if seconds < 0 || seconds > maxRetryDelaySec {
		return ErrInvalidRetryDelay
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
