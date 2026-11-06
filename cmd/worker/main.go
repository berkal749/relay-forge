package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"relayforge/internal/config"
	"relayforge/internal/database"
	"relayforge/internal/database/db"
	"relayforge/internal/execution"
	"relayforge/internal/outbox"
	"relayforge/internal/worker"
	"relayforge/internal/workertoken"
)

const outboxPollInterval = time.Second

func main() {
	if err := run(); err != nil {
		slog.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if cfg.WorkerProjectID == "" && cfg.WorkerToken != "" {
		return fmt.Errorf("WORKER_PROJECT_ID is required")
	}

	connectCtx, cancelConnect := context.WithTimeout(context.Background(), cfg.DatabaseConnectTimeout)
	defer cancelConnect()

	pool, err := database.Connect(connectCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	queries := db.New(pool)
	registerCtx, cancelRegister := context.WithTimeout(context.Background(), 10*time.Second)
	tokenService := workertoken.NewService(queries, nil, cfg.WorkerTokenPepper)
	credentials, err := worker.ResolveCredentials(registerCtx, queries, tokenService, worker.CredentialOptions{
		ProjectID:       cfg.WorkerProjectID,
		Token:           cfg.WorkerToken,
		TokenFile:       cfg.WorkerTokenFile,
		EnrollmentToken: cfg.WorkerEnrollmentToken,
		APIBaseURL:      cfg.APIBaseURL,
	})
	if err != nil {
		cancelRegister()
		return fmt.Errorf("resolve worker credentials: %w", err)
	}

	projectID := credentials.ProjectID
	workerName := cfg.WorkerName
	concurrency := cfg.WorkerConcurrency
	jobTimeout := cfg.WorkerJobTimeout
	retryPolicy := execution.RetryPolicy{
		Backoff: execution.BackoffExponential,
		Base:    cfg.RetryBaseDelay,
		Max:     cfg.RetryMaxDelay,
	}
	var queueNames []string
	var maxAttemptsOverride int32
	if credentials.Pool != nil {
		pool := credentials.Pool
		workerName = pool.Name
		concurrency = int(pool.Concurrency)
		queueNames = pool.QueueNames
		maxAttemptsOverride = pool.MaxAttempts
		if pool.JobTimeoutSeconds > 0 {
			jobTimeout = time.Duration(pool.JobTimeoutSeconds) * time.Second
		}
		if pool.RetryBackoff != "" {
			retryPolicy.Backoff = pool.RetryBackoff
		}
		if pool.RetryBaseDelaySeconds > 0 {
			retryPolicy.Base = time.Duration(pool.RetryBaseDelaySeconds) * time.Second
		}
		if pool.RetryMaxDelaySeconds > 0 {
			retryPolicy.Max = time.Duration(pool.RetryMaxDelaySeconds) * time.Second
		}
		if retryPolicy.Max < retryPolicy.Base {
			retryPolicy.Max = retryPolicy.Base
		}
	}

	workerRecord, err := worker.Register(registerCtx, queries, projectID, credentials.Token.ID, credentials.Token.PoolID, workerName)
	if err != nil {
		cancelRegister()
		return err
	}
	projectRecord, err := queries.GetProject(registerCtx, projectID)
	cancelRegister()
	if err != nil {
		return fmt.Errorf("load project settings: %w", err)
	}

	if projectRecord.MaxConcurrency > 0 && int(projectRecord.MaxConcurrency) < concurrency {
		concurrency = int(projectRecord.MaxConcurrency)
	}
	if projectRecord.DefaultJobTimeoutSeconds > 0 && (credentials.Pool == nil || credentials.Pool.JobTimeoutSeconds == 0) {
		jobTimeout = time.Duration(projectRecord.DefaultJobTimeoutSeconds) * time.Second
	}

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()

	signalCtx, stop := signal.NotifyContext(runCtx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	go worker.RunHeartbeat(signalCtx, queries, workerRecord.ID, cfg.WorkerHeartbeatInterval)
	go worker.RunCleanup(signalCtx, queries, projectID, cfg.WorkerCleanupInterval)

	dispatcher := outbox.NewDispatcher(pool, outbox.PostgresNotifier{})
	var outboxWorkers sync.WaitGroup
	outboxWorkers.Add(1)
	go func() {
		defer outboxWorkers.Done()
		runOutbox(signalCtx, dispatcher)
	}()

	registry := execution.NewRegistry()
	registry.Register("echo", execution.Echo)
	registry.Register("noop", execution.Noop)
	registry.Register("sleep", execution.Sleep)

	executionService := execution.NewService(pool, retryPolicy, queueNames, maxAttemptsOverride)
	poolRunner := worker.NewPool(
		executionService,
		registry,
		projectID,
		workerRecord.ID,
		worker.PoolConfig{
			Concurrency:     concurrency,
			PollInterval:    cfg.WorkerPollInterval,
			JobTimeout:      jobTimeout,
			ShutdownTimeout: cfg.WorkerShutdownTimeout,
			RateLimitPerMin: int(projectRecord.RateLimitPerMinute),
		},
	)

	slog.Info("worker started",
		"worker_id", workerRecord.ID,
		"project_id", projectID,
		"concurrency", concurrency,
		"job_timeout", jobTimeout,
		"rate_limit_per_minute", projectRecord.RateLimitPerMinute,
		"queue_names", queueNames,
		"max_attempts_override", maxAttemptsOverride,
		"retry_backoff", retryPolicy.Backoff,
		"retry_base_delay", retryPolicy.Base,
		"retry_max_delay", retryPolicy.Max,
	)

	runErr := poolRunner.Run(signalCtx)
	cancelRun()
	outboxWorkers.Wait()

	offlineCtx, cancelOffline := context.WithTimeout(context.Background(), 10*time.Second)
	if err := worker.MarkOffline(offlineCtx, queries, workerRecord.ID); err != nil {
		slog.Error("mark worker offline", "error", err)
	}
	cancelOffline()

	return runErr
}

func runOutbox(ctx context.Context, dispatcher *outbox.Dispatcher) {
	ticker := time.NewTicker(outboxPollInterval)
	defer ticker.Stop()

	for {
		processed, err := dispatcher.ProcessBatch(ctx)
		if err != nil && ctx.Err() == nil {
			slog.Error("process outbox batch", "error", err)
		} else if processed > 0 {
			slog.Info("published outbox messages", "count", processed)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
