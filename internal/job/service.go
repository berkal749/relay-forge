package job

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"relayforge/internal/database/db"
)

const (
	maxQueueNameLength      = 100
	maxJobTypeLength        = 100
	maxMaxAttempts          = 100
	maxPayloadBytes         = 256 * 1024
	maxIdempotencyKeyLength = 255
)

var (
	ErrInvalidQueueName      = errors.New("queue name must be between 1 and 100 characters")
	ErrInvalidJobType        = errors.New("job type must be between 1 and 100 characters")
	ErrInvalidPayload        = errors.New("payload must be a non-null JSON value")
	ErrPayloadTooLarge       = errors.New("payload must be at most 256 KiB")
	ErrInvalidMaxAttempts    = errors.New("max attempts must be between 1 and 100")
	ErrJobNotFound           = errors.New("job not found")
	ErrProjectNotFound       = errors.New("project not found")
	ErrInvalidIdempotencyKey = errors.New("idempotency key must be between 1 and 255 printable characters")
	ErrIdempotencyConflict   = errors.New("idempotency key was already used with a different request")
	ErrQueueLimit            = errors.New("queued job limit reached for this project")
)

// Limits caps per-project job volume. A zero value means unlimited.
type Limits struct {
	MaxQueuedJobsPerProject int
}

type Service struct {
	pool    *pgxpool.Pool
	queries *db.Queries
	limits  Limits
}

type SubmitInput struct {
	ProjectID      uuid.UUID
	QueueName      string
	JobType        string
	Payload        json.RawMessage
	Priority       *int32
	MaxAttempts    *int32
	ScheduledAt    time.Time
	IdempotencyKey string
}

type SubmitResult struct {
	Job      db.Job
	Replayed bool
}

func NewService(pool *pgxpool.Pool, limits Limits) *Service {
	return &Service{
		pool:    pool,
		queries: db.New(pool),
		limits:  limits,
	}
}

func (s *Service) Submit(ctx context.Context, input SubmitInput) (SubmitResult, error) {
	jobType := strings.TrimSpace(input.JobType)
	if jobType == "" || len(jobType) > maxJobTypeLength {
		return SubmitResult{}, ErrInvalidJobType
	}

	payload, err := normalizePayload(input.Payload)
	if err != nil {
		return SubmitResult{}, err
	}

	idempotencyKey, err := normalizeIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return SubmitResult{}, err
	}

	var idempotencyKeyParam *string
	var idempotencyHashParam *string
	if idempotencyKey != "" {
		hash := submitFingerprint(jobType, payload, input)
		idempotencyKeyParam = &idempotencyKey
		idempotencyHashParam = &hash
	}

	settings, err := s.queries.GetProject(ctx, input.ProjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return SubmitResult{}, ErrProjectNotFound
	}
	if err != nil {
		return SubmitResult{}, fmt.Errorf("get project settings: %w", err)
	}

	queueName, priority, maxAttempts, err := resolveSubmitDefaults(settings, input)
	if err != nil {
		return SubmitResult{}, err
	}

	if s.limits.MaxQueuedJobsPerProject > 0 {
		queued, err := s.queries.CountQueuedJobsByProject(ctx, input.ProjectID)
		if err != nil {
			return SubmitResult{}, fmt.Errorf("count queued jobs: %w", err)
		}
		if queued >= int64(s.limits.MaxQueuedJobsPerProject) {
			return SubmitResult{}, ErrQueueLimit
		}
	}

	now := time.Now()
	scheduledAt := input.ScheduledAt
	if scheduledAt.IsZero() {
		scheduledAt = now
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("begin job transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := db.New(tx)
	created, err := queries.CreateJob(ctx, db.CreateJobParams{
		ProjectID:       input.ProjectID,
		QueueName:       queueName,
		JobType:         jobType,
		Payload:         payload,
		Status:          string(InitialStatus(scheduledAt, now)),
		Priority:        priority,
		MaxAttempts:     maxAttempts,
		ScheduledAt:     scheduledAt,
		IdempotencyKey:  idempotencyKeyParam,
		IdempotencyHash: idempotencyHashParam,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, replayErr := s.replayIdempotentSubmit(ctx, queries, input.ProjectID, idempotencyKey, idempotencyHashParam)
		if replayErr != nil {
			return SubmitResult{}, replayErr
		}
		if err := tx.Commit(ctx); err != nil {
			return SubmitResult{}, fmt.Errorf("commit idempotent job transaction: %w", err)
		}
		return SubmitResult{Job: existing, Replayed: true}, nil
	}
	if err != nil {
		return SubmitResult{}, fmt.Errorf("create job: %w", err)
	}

	if _, err := queries.CreateOutboxMessage(ctx, created.ID); err != nil {
		return SubmitResult{}, fmt.Errorf("create job outbox message: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return SubmitResult{}, fmt.Errorf("commit job transaction: %w", err)
	}

	return SubmitResult{Job: created}, nil
}

func (s *Service) replayIdempotentSubmit(ctx context.Context, queries *db.Queries, projectID uuid.UUID, key string, hash *string) (db.Job, error) {
	if key == "" || hash == nil {
		return db.Job{}, fmt.Errorf("create job returned no row without an idempotency key")
	}

	existing, err := queries.GetJobByIdempotencyKey(ctx, db.GetJobByIdempotencyKeyParams{
		ProjectID:      projectID,
		IdempotencyKey: &key,
	})
	if err != nil {
		return db.Job{}, fmt.Errorf("get idempotent job: %w", err)
	}
	if existing.IdempotencyHash == nil || *existing.IdempotencyHash != *hash {
		return db.Job{}, ErrIdempotencyConflict
	}
	return existing, nil
}

func normalizeIdempotencyKey(value string) (string, error) {
	key := strings.TrimSpace(value)
	if key == "" {
		return "", nil
	}
	if len(key) > maxIdempotencyKeyLength {
		return "", ErrInvalidIdempotencyKey
	}
	for _, char := range key {
		if char < 0x20 || char == 0x7f {
			return "", ErrInvalidIdempotencyKey
		}
	}
	return key, nil
}

func submitFingerprint(jobType string, payload json.RawMessage, input SubmitInput) string {
	var scheduledAt *int64
	if !input.ScheduledAt.IsZero() {
		value := input.ScheduledAt.UTC().UnixNano()
		scheduledAt = &value
	}

	canonical := struct {
		QueueName   string          `json:"queue_name"`
		JobType     string          `json:"job_type"`
		Payload     json.RawMessage `json:"payload"`
		Priority    *int32          `json:"priority"`
		MaxAttempts *int32          `json:"max_attempts"`
		ScheduledAt *int64          `json:"scheduled_at"`
	}{
		QueueName:   strings.TrimSpace(input.QueueName),
		JobType:     jobType,
		Payload:     payload,
		Priority:    input.Priority,
		MaxAttempts: input.MaxAttempts,
		ScheduledAt: scheduledAt,
	}

	encoded, err := json.Marshal(canonical)
	if err != nil {
		// The canonical struct only contains JSON-marshalable values.
		panic(fmt.Sprintf("marshal idempotency fingerprint: %v", err))
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func (s *Service) Get(ctx context.Context, projectID, jobID uuid.UUID) (db.Job, error) {
	found, err := s.queries.GetJobForProject(ctx, db.GetJobForProjectParams{
		ID:        jobID,
		ProjectID: projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Job{}, ErrJobNotFound
	}
	if err != nil {
		return db.Job{}, fmt.Errorf("get job: %w", err)
	}
	return found, nil
}

func (s *Service) List(ctx context.Context, projectID uuid.UUID, limit, offset int32) ([]db.Job, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	jobs, err := s.queries.ListJobsByProject(ctx, db.ListJobsByProjectParams{
		ProjectID: projectID,
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	return jobs, nil
}

func (s *Service) Executions(ctx context.Context, projectID, jobID uuid.UUID) ([]db.Execution, error) {
	if _, err := s.Get(ctx, projectID, jobID); err != nil {
		return nil, err
	}

	executions, err := s.queries.ListExecutionsByJob(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("list executions: %w", err)
	}
	return executions, nil
}

// resolveSubmitDefaults applies project settings to any submission fields that
// were not provided explicitly.
func resolveSubmitDefaults(settings db.Project, input SubmitInput) (queueName string, priority, maxAttempts int32, err error) {
	queueName = strings.TrimSpace(input.QueueName)
	if queueName == "" {
		queueName = strings.TrimSpace(settings.DefaultQueueName)
	}
	if queueName == "" || len(queueName) > maxQueueNameLength {
		return "", 0, 0, ErrInvalidQueueName
	}

	maxAttempts = settings.DefaultMaxAttempts
	if input.MaxAttempts != nil {
		maxAttempts = *input.MaxAttempts
	}
	if maxAttempts < 1 || maxAttempts > maxMaxAttempts {
		return "", 0, 0, ErrInvalidMaxAttempts
	}

	priority = settings.DefaultPriority
	if input.Priority != nil {
		priority = *input.Priority
	}

	return queueName, priority, maxAttempts, nil
}

func normalizePayload(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	if len(raw) > maxPayloadBytes {
		return nil, ErrPayloadTooLarge
	}

	trimmed := bytes.TrimSpace(raw)
	if !json.Valid(trimmed) || bytes.Equal(trimmed, []byte("null")) {
		return nil, ErrInvalidPayload
	}
	return trimmed, nil
}
