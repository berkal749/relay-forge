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

const (
	maxErrorMessageLength = 4000
	defaultLease          = 5 * time.Minute
	defaultReapBatch      = 100
)

var (
	ErrNoJobs                  = errors.New("no jobs available")
	ErrExecutionNotFound       = errors.New("execution not found")
	ErrExecutionNotRunning     = errors.New("execution is no longer running")
	ErrExecutionAlreadyApplied = errors.New("execution was already finished")
	ErrLeaseExpired            = errors.New("worker lease expired")
	ErrResultNotValidJSON      = errors.New("execution result is not valid JSON")
	ErrResultPayloadTooBig     = errors.New("execution result is too large")
	maxExecutionResultBytes    = 1 << 20
)

type Service struct {
	pool        *pgxpool.Pool
	queries     *db.Queries
	retryPolicy RetryPolicy
	queueNames  []string
	maxAttempts int32
	lease       time.Duration
}

type Claim struct {
	Job       db.Job
	Execution db.Execution
}

type ClaimParams struct {
	ProjectID   uuid.UUID
	WorkerID    uuid.UUID
	QueueNames  []string
	MaxAttempts int32
	Lease       time.Duration
}

type FailureOutcome struct {
	Retry     bool
	RetryAt   time.Time
	JobStatus string
}

func NewService(pool *pgxpool.Pool, retryPolicy RetryPolicy, queueNames []string, maxAttempts int32, lease time.Duration) *Service {
	if retryPolicy.Backoff == "" {
		retryPolicy.Backoff = BackoffExponential
	}
	if lease <= 0 {
		lease = defaultLease
	}
	return &Service{
		pool:        pool,
		queries:     db.New(pool),
		retryPolicy: retryPolicy,
		queueNames:  queueNames,
		maxAttempts: maxAttempts,
		lease:       lease,
	}
}

// Claim uses the policy configured on this service. It is the direct-worker path.
func (s *Service) Claim(ctx context.Context, projectID, workerID uuid.UUID) (Claim, error) {
	return s.ClaimJob(ctx, ClaimParams{
		ProjectID:   projectID,
		WorkerID:    workerID,
		QueueNames:  s.queueNames,
		MaxAttempts: s.maxAttempts,
		Lease:       s.lease,
	})
}

// ClaimJob atomically marks a job running, creates its execution attempt, and
// records the lease that keeps remote workers accountable.
func (s *Service) ClaimJob(ctx context.Context, params ClaimParams) (Claim, error) {
	lease := params.Lease
	if lease <= 0 {
		lease = s.lease
	}
	if lease <= 0 {
		lease = defaultLease
	}
	leaseUntil := time.Now().Add(lease)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Claim{}, fmt.Errorf("begin claim transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := db.New(tx)
	job, err := queries.ClaimJob(ctx, db.ClaimJobParams{
		ProjectID:           params.ProjectID,
		QueueNames:          params.QueueNames,
		MaxAttemptsOverride: params.MaxAttempts,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Claim{}, ErrNoJobs
	}
	if err != nil {
		return Claim{}, fmt.Errorf("claim job: %w", err)
	}

	execution, err := queries.CreateExecution(ctx, db.CreateExecutionParams{
		JobID:          job.ID,
		WorkerID:       &params.WorkerID,
		AttemptNumber:  job.Attempts,
		Status:         "running",
		LeaseExpiresAt: &leaseUntil,
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
	return s.finishSucceeded(ctx, claim.Job, claim.Execution, result)
}

// SucceedExecution applies a completion reported by a remote worker. The
// execution and job must belong to the token's project and worker.
func (s *Service) SucceedExecution(ctx context.Context, projectID, workerID, executionID uuid.UUID, result json.RawMessage) error {
	execution, err := s.executionForWorker(ctx, projectID, workerID, executionID)
	if err != nil {
		return err
	}
	job, err := s.jobForExecution(ctx, projectID, execution)
	if err != nil {
		return err
	}
	return s.finishSucceeded(ctx, job, execution, result)
}

func (s *Service) Fail(ctx context.Context, claim Claim, cause error, cancelled bool) (FailureOutcome, error) {
	return s.fail(ctx, claim.Job, claim.Execution, cause, cancelled, s.retryPolicy)
}

// FailExecution applies a failure reported by a remote worker or the lease
// reaper, using the pool's retry policy.
func (s *Service) FailExecution(ctx context.Context, projectID, workerID, executionID uuid.UUID, cause error, cancelled bool, policy RetryPolicy) (FailureOutcome, error) {
	execution, err := s.executionForWorker(ctx, projectID, workerID, executionID)
	if err != nil {
		return FailureOutcome{}, err
	}
	job, err := s.jobForExecution(ctx, projectID, execution)
	if err != nil {
		return FailureOutcome{}, err
	}
	return s.fail(ctx, job, execution, cause, cancelled, policy)
}

// ReapExpired finds running executions whose lease has elapsed and fails them
// so the job can retry or become terminal. Workers that are still alive renew
// their leases through heartbeat.
func (s *Service) ReapExpired(ctx context.Context, batch int32, policyFor func(db.Job, db.Execution) RetryPolicy) (int, error) {
	if batch <= 0 {
		batch = defaultReapBatch
	}
	executions, err := s.queries.ListExpiredExecutions(ctx, batch)
	if err != nil {
		return 0, fmt.Errorf("list expired executions: %w", err)
	}

	reaped := 0
	for _, execution := range executions {
		job, err := s.queries.GetJob(ctx, execution.JobID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return reaped, fmt.Errorf("load job for expired execution: %w", err)
		}
		policy := s.retryPolicy
		if policyFor != nil {
			policy = policyFor(job, execution)
		}
		_, err = s.fail(ctx, job, execution, ErrLeaseExpired, false, policy)
		if errors.Is(err, ErrExecutionNotRunning) || errors.Is(err, ErrExecutionAlreadyApplied) {
			continue
		}
		if err != nil {
			return reaped, err
		}
		reaped++
	}
	return reaped, nil
}

func (s *Service) finishSucceeded(ctx context.Context, job db.Job, execution db.Execution, result json.RawMessage) error {
	if len(result) == 0 {
		result = json.RawMessage(`{}`)
	}
	if len(result) > maxExecutionResultBytes {
		return ErrResultPayloadTooBig
	}
	if !json.Valid(result) {
		return ErrResultNotValidJSON
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin execution transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := db.New(tx)
	if _, err := queries.FinishExecution(ctx, db.FinishExecutionParams{
		ID:     execution.ID,
		Status: "succeeded",
		Result: result,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if finishErr := s.alreadyFinished(ctx, execution.ID, "succeeded"); finishErr != nil {
				if errors.Is(finishErr, ErrExecutionAlreadyApplied) {
					return nil
				}
				return finishErr
			}
			return nil
		}
		return fmt.Errorf("complete execution: %w", err)
	}
	if _, err := queries.MarkJobSucceeded(ctx, job.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("mark job succeeded: %w", ErrExecutionNotRunning)
		}
		return fmt.Errorf("mark job succeeded: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit execution transaction: %w", err)
	}
	return nil
}

func (s *Service) fail(ctx context.Context, job db.Job, execution db.Execution, cause error, cancelled bool, policy RetryPolicy) (FailureOutcome, error) {
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

	retry := job.Attempts < job.MaxAttempts
	var retryAt time.Time
	jobStatus := "failed"
	if retry {
		if policy.Backoff == "" {
			policy.Backoff = BackoffExponential
		}
		retryAt = time.Now().Add(policy.Delay(job.Attempts))
		jobStatus = "scheduled"
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return FailureOutcome{}, fmt.Errorf("begin failure transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := db.New(tx)
	if _, err := queries.FinishExecution(ctx, db.FinishExecutionParams{
		ID:           execution.ID,
		Status:       executionStatus,
		ErrorMessage: &message,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return FailureOutcome{}, s.alreadyFinished(ctx, execution.ID, executionStatus)
		}
		return FailureOutcome{}, fmt.Errorf("finish failed execution: %w", err)
	}

	if retry {
		if _, err := queries.MarkJobScheduled(ctx, db.MarkJobScheduledParams{
			ID:          job.ID,
			ScheduledAt: retryAt,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return FailureOutcome{}, ErrExecutionNotRunning
			}
			return FailureOutcome{}, fmt.Errorf("schedule job retry: %w", err)
		}
	} else {
		if _, err := queries.MarkJobFailed(ctx, job.ID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return FailureOutcome{}, ErrExecutionNotRunning
			}
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

// alreadyFinished distinguishes an idempotent replay from a conflicting write.
func (s *Service) alreadyFinished(ctx context.Context, executionID uuid.UUID, expected string) error {
	current, err := s.queries.GetExecution(ctx, executionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrExecutionNotFound
		}
		return fmt.Errorf("load execution: %w", err)
	}
	if current.Status == expected {
		return ErrExecutionAlreadyApplied
	}
	return ErrExecutionNotRunning
}

func (s *Service) executionForWorker(ctx context.Context, projectID, workerID, executionID uuid.UUID) (db.Execution, error) {
	execution, err := s.queries.GetExecutionForWorker(ctx, db.GetExecutionForWorkerParams{
		ID:       executionID,
		WorkerID: &workerID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Execution{}, ErrExecutionNotFound
	}
	if err != nil {
		return db.Execution{}, fmt.Errorf("load execution: %w", err)
	}
	return execution, nil
}

func (s *Service) jobForExecution(ctx context.Context, projectID uuid.UUID, execution db.Execution) (db.Job, error) {
	job, err := s.queries.GetJobForProject(ctx, db.GetJobForProjectParams{
		ID:        execution.JobID,
		ProjectID: projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Job{}, ErrExecutionNotFound
	}
	if err != nil {
		return db.Job{}, fmt.Errorf("load execution job: %w", err)
	}
	return job, nil
}
