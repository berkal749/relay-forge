package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"relayforge/internal/database/db"
)

const maxErrorMessageLength = 4000

var ErrNoJobs = errors.New("no jobs available")

type Service struct {
	pool        *pgxpool.Pool
	retryPolicy RetryPolicy
	queueNames  []string
	maxAttempts int32
}

type Claim struct {
	Job       db.Job
	Execution db.Execution
}

type FailureOutcome struct {
	Retry     bool
	RetryAt   time.Time
	JobStatus string
}

func NewService(pool *pgxpool.Pool, retryPolicy RetryPolicy, queueNames []string, maxAttempts int32) *Service {
	if retryPolicy.Backoff == "" {
		retryPolicy.Backoff = BackoffExponential
	}
	return &Service{
		pool:        pool,
		retryPolicy: retryPolicy,
		queueNames:  queueNames,
		maxAttempts: maxAttempts,
	}
}

// Claim atomically marks a job running and creates its execution attempt.
func (s *Service) Claim(ctx context.Context, projectID, workerID uuid.UUID) (Claim, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Claim{}, fmt.Errorf("begin claim transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := db.New(tx)
	job, err := queries.ClaimJob(ctx, db.ClaimJobParams{
		ProjectID:           projectID,
		QueueNames:          s.queueNames,
		MaxAttemptsOverride: s.maxAttempts,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Claim{}, ErrNoJobs
	}
	if err != nil {
		return Claim{}, fmt.Errorf("claim job: %w", err)
	}

	execution, err := queries.CreateExecution(ctx, db.CreateExecutionParams{
		JobID:         job.ID,
		WorkerID:      &workerID,
		AttemptNumber: job.Attempts,
		Status:        "running",
	})
	if err != nil {
		return Claim{}, fmt.Errorf("create execution: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Claim{}, fmt.Errorf("commit claim transaction: %w", err)
	}

	return Claim{Job: job, Execution: execution}, nil
}

func (s *Service) Succeed(ctx context.Context, claim Claim, result json.RawMessage) error {
	if len(result) == 0 {
		result = json.RawMessage(`{}`)
	}
	if !json.Valid(result) {
		return errors.New("execution result is not valid JSON")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin execution transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := db.New(tx)
	if _, err := queries.FinishExecution(ctx, db.FinishExecutionParams{
		ID:     claim.Execution.ID,
		Status: "succeeded",
		Result: result,
	}); err != nil {
		return fmt.Errorf("complete execution: %w", err)
	}
	if _, err := queries.MarkJobSucceeded(ctx, claim.Job.ID); err != nil {
		return fmt.Errorf("mark job succeeded: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit execution transaction: %w", err)
	}
	return nil
}

func (s *Service) Fail(ctx context.Context, claim Claim, cause error, cancelled bool) (FailureOutcome, error) {
	message := "job failed"
	if cause != nil {
		message = cause.Error()
	}
	if cancelled && (cause == nil || errors.Is(cause, context.Canceled)) {
		message = "job execution cancelled during shutdown"
	}
	if len(message) > maxErrorMessageLength {
		message = message[:maxErrorMessageLength]
	}

	executionStatus := "failed"
	if cancelled {
		executionStatus = "cancelled"
	}

	retry := claim.Job.Attempts < claim.Job.MaxAttempts
	var retryAt time.Time
	jobStatus := "failed"
	if retry {
		retryAt = time.Now().Add(s.retryPolicy.Delay(claim.Job.Attempts))
		jobStatus = "scheduled"
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return FailureOutcome{}, fmt.Errorf("begin failure transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := db.New(tx)
	if _, err := queries.FinishExecution(ctx, db.FinishExecutionParams{
		ID:           claim.Execution.ID,
		Status:       executionStatus,
		ErrorMessage: &message,
	}); err != nil {
		return FailureOutcome{}, fmt.Errorf("finish failed execution: %w", err)
	}

	if retry {
		if _, err := queries.MarkJobScheduled(ctx, db.MarkJobScheduledParams{
			ID:          claim.Job.ID,
			ScheduledAt: retryAt,
		}); err != nil {
			return FailureOutcome{}, fmt.Errorf("schedule job retry: %w", err)
		}
	} else {
		if _, err := queries.MarkJobFailed(ctx, claim.Job.ID); err != nil {
			return FailureOutcome{}, fmt.Errorf("mark job failed: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return FailureOutcome{}, fmt.Errorf("commit failure transaction: %w", err)
	}

	return FailureOutcome{
		Retry:     retry,
		RetryAt:   retryAt,
		JobStatus: jobStatus,
	}, nil
}
