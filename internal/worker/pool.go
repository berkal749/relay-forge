package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"relayforge/internal/execution"
)

const finishTimeout = 15 * time.Second

type PoolConfig struct {
	Concurrency     int
	PollInterval    time.Duration
	JobTimeout      time.Duration
	ShutdownTimeout time.Duration
	RateLimitPerMin int
}

type Pool struct {
	execution *execution.Service
	executor  execution.Executor
	projectID uuid.UUID
	workerID  uuid.UUID
	config    PoolConfig
	limiter   *rateLimiter

	activeCtx    context.Context
	cancelActive context.CancelFunc
}

func NewPool(
	executionService *execution.Service,
	executor execution.Executor,
	projectID uuid.UUID,
	workerID uuid.UUID,
	config PoolConfig,
) *Pool {
	return &Pool{
		execution: executionService,
		executor:  executor,
		projectID: projectID,
		workerID:  workerID,
		config:    config,
		limiter:   newRateLimiter(config.RateLimitPerMin),
	}
}

// Run starts the worker pool and gracefully drains in-flight jobs on shutdown.
func (p *Pool) Run(ctx context.Context) error {
	if p.config.Concurrency < 1 {
		return fmt.Errorf("worker concurrency must be at least 1")
	}

	p.activeCtx, p.cancelActive = context.WithCancel(context.Background())
	defer p.cancelActive()

	var workers sync.WaitGroup
	for i := 0; i < p.config.Concurrency; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			p.runWorker(ctx)
		}()
	}

	<-ctx.Done()

	done := make(chan struct{})
	go func() {
		workers.Wait()
		close(done)
	}()

	timer := time.NewTimer(p.config.ShutdownTimeout)
	defer timer.Stop()

	select {
	case <-done:
		return nil
	case <-timer.C:
		slog.Warn("worker shutdown timeout reached; cancelling in-flight jobs")
		p.cancelActive()
		<-done
		return nil
	}
}

func (p *Pool) runWorker(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}

		if waitFor, ok := p.limiter.reserve(time.Now()); !ok {
			if !wait(ctx, waitFor) {
				return
			}
			continue
		}

		claimCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		claim, err := p.execution.Claim(claimCtx, p.projectID, p.workerID)
		cancel()

		if errors.Is(err, execution.ErrNoJobs) {
			if !wait(ctx, p.config.PollInterval) {
				return
			}
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("claim job", "error", err)
			if !wait(ctx, p.config.PollInterval) {
				return
			}
			continue
		}

		p.execute(claim)
	}
}

func (p *Pool) execute(claim execution.Claim) {
	jobCtx, cancel := context.WithTimeout(p.activeCtx, p.config.JobTimeout)
	result, executeErr := p.executor.Execute(jobCtx, claim.Job)
	contextErr := jobCtx.Err()
	cancel()

	cancelled := errors.Is(contextErr, context.Canceled) && p.activeCtx.Err() != nil
	if errors.Is(contextErr, context.DeadlineExceeded) {
		executeErr = fmt.Errorf("job timed out after %s", p.config.JobTimeout)
	} else if executeErr == nil && contextErr != nil {
		executeErr = contextErr
	}
	if cancelled && executeErr == nil {
		executeErr = context.Canceled
	}

	finishCtx, finishCancel := context.WithTimeout(context.Background(), finishTimeout)
	defer finishCancel()

	if executeErr == nil {
		if err := p.execution.Succeed(finishCtx, claim, result); err != nil {
			slog.Error("persist successful execution", "error", err, "job_id", claim.Job.ID)
			persistErr := fmt.Errorf("persist execution result: %w", err)
			if _, failErr := p.execution.Fail(finishCtx, claim, persistErr, false); failErr != nil {
				slog.Error("persist failed execution", "error", failErr, "job_id", claim.Job.ID)
			}
			return
		}
		slog.Info("job succeeded",
			"job_id", claim.Job.ID,
			"job_type", claim.Job.JobType,
			"attempt", claim.Job.Attempts,
		)
		return
	}

	outcome, err := p.execution.Fail(finishCtx, claim, executeErr, cancelled)
	if err != nil {
		slog.Error("persist failed execution", "error", err, "job_id", claim.Job.ID)
		return
	}

	attributes := []any{
		"job_id", claim.Job.ID,
		"job_type", claim.Job.JobType,
		"attempt", claim.Job.Attempts,
		"error", executeErr,
		"retry", outcome.Retry,
		"job_status", outcome.JobStatus,
	}
	if outcome.Retry {
		attributes = append(attributes, "retry_at", outcome.RetryAt)
	}
	slog.Warn("job attempt failed", attributes...)
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
