package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"relayforge/internal/action"
	"relayforge/internal/config"
	"relayforge/internal/database"
	"relayforge/internal/database/db"
	"relayforge/internal/execution"
	"relayforge/internal/job"
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

	if err := cfg.ValidateWorker(); err != nil {
		return err
	}
	if err := worker.LoadSecretsFile(cfg.WorkerSecretsFile); err != nil {
		return err
	}

	if cfg.WorkerMode == "remote" {
		return runRemote(cfg)
	}
	return runDirect(cfg)
}

func runRemote(cfg config.Config) error {
	registerCtx, cancelRegister := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelRegister()

	workerNameOption := ""
	if os.Getenv("WORKER_NAME") != "" {
		workerNameOption = cfg.WorkerName
	}
	identity, err := worker.ResolveRemoteIdentity(registerCtx, worker.CredentialOptions{
		Token:           cfg.WorkerToken,
		TokenFile:       cfg.WorkerTokenFile,
		EnrollmentToken: cfg.WorkerEnrollmentToken,
		APIBaseURL:      cfg.APIBaseURL,
		WorkerName:      workerNameOption,
	})
	if err != nil {
		return fmt.Errorf("resolve remote worker identity: %w", err)
	}

	remote := identity.Config
	workerName := cfg.WorkerName
	concurrency := cfg.WorkerConcurrency
	jobTimeout := cfg.WorkerJobTimeout
	queueNames := []string(nil)
	maxAttemptsOverride := int32(0)
	retryPolicy := execution.RetryPolicy{
		Backoff: execution.BackoffExponential,
		Base:    cfg.RetryBaseDelay,
		Max:     cfg.RetryMaxDelay,
	}

	if remote.WorkerPool != nil {
		pool := remote.WorkerPool
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

	if remote.Project.MaxConcurrency > 0 && int(remote.Project.MaxConcurrency) < concurrency {
		concurrency = int(remote.Project.MaxConcurrency)
	}
	if remote.Project.DefaultJobTimeoutSeconds > 0 && (remote.WorkerPool == nil || remote.WorkerPool.JobTimeoutSeconds == 0) {
		jobTimeout = time.Duration(remote.Project.DefaultJobTimeoutSeconds) * time.Second
	}

	backend := worker.NewRemoteBackend(cfg.APIBaseURL, identity.Token, remote.WorkerID, nil)
	registry, err := action.NewRegistry(action.FromConfig(cfg), action.NewAPISubmitter(cfg.APIBaseURL, identity.Token, nil))
	if err != nil {
		return fmt.Errorf("build handler registry: %w", err)
	}
	slog.Info("remote worker started",
		"worker_id", remote.WorkerID,
		"project_id", remote.ProjectID,
		"name", workerName,
		"api_url", cfg.APIBaseURL,
		"concurrency", concurrency,
		"job_timeout", jobTimeout,
		"queue_names", queueNames,
		"max_attempts_override", maxAttemptsOverride,
		"retry_backoff", retryPolicy.Backoff,
		"rate_limit_per_minute", remote.Project.RateLimitPerMinute,
	)

	return runPool(cfg, backend, registry, concurrency, jobTimeout, int(remote.Project.RateLimitPerMinute))
}

func runDirect(cfg config.Config) error {
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
	defer cancelRegister()

	tokenService := workertoken.NewService(queries, nil, cfg.WorkerTokenPepper)
	credentials, err := worker.ResolveCredentials(registerCtx, queries, tokenService, worker.CredentialOptions{
		ProjectID:       cfg.WorkerProjectID,
		Token:           cfg.WorkerToken,
		TokenFile:       cfg.WorkerTokenFile,
		EnrollmentToken: cfg.WorkerEnrollmentToken,
		APIBaseURL:      cfg.APIBaseURL,
		WorkerName:      cfg.WorkerName,
	})
	if err != nil {
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
		configured := credentials.Pool
		workerName = configured.Name
		concurrency = int(configured.Concurrency)
		queueNames = configured.QueueNames
		maxAttemptsOverride = configured.MaxAttempts
		if configured.JobTimeoutSeconds > 0 {
			jobTimeout = time.Duration(configured.JobTimeoutSeconds) * time.Second
		}
		if configured.RetryBackoff != "" {
			retryPolicy.Backoff = configured.RetryBackoff
		}
		if configured.RetryBaseDelaySeconds > 0 {
			retryPolicy.Base = time.Duration(configured.RetryBaseDelaySeconds) * time.Second
		}
		if configured.RetryMaxDelaySeconds > 0 {
			retryPolicy.Max = time.Duration(configured.RetryMaxDelaySeconds) * time.Second
		}
		if retryPolicy.Max < retryPolicy.Base {
			retryPolicy.Max = retryPolicy.Base
		}
	}

	workerRecord, err := worker.Register(registerCtx, queries, projectID, credentials.Token.ID, credentials.Token.PoolID, workerName, "direct")
	if err != nil {
		return err
	}
	projectRecord, err := queries.GetProject(registerCtx, projectID)
	if err != nil {
		return fmt.Errorf("load project settings: %w", err)
	}

	if projectRecord.MaxConcurrency > 0 && int(projectRecord.MaxConcurrency) < concurrency {
		concurrency = int(projectRecord.MaxConcurrency)
	}
	if projectRecord.DefaultJobTimeoutSeconds > 0 && (credentials.Pool == nil || credentials.Pool.JobTimeoutSeconds == 0) {
		jobTimeout = time.Duration(projectRecord.DefaultJobTimeoutSeconds) * time.Second
	}

	lease := jobTimeout + cfg.WorkerLeaseGrace
	executionService := execution.NewService(pool, retryPolicy, queueNames, maxAttemptsOverride, lease)
	backend := worker.NewDirectBackend(executionService, queries, projectID, workerRecord.ID, lease)
	jobService := job.NewService(pool, job.Limits{MaxQueuedJobsPerProject: cfg.MaxQueuedJobsPerProject})
	registry, err := action.NewRegistry(action.FromConfig(cfg), action.NewDirectSubmitter(jobService, projectID))
	if err != nil {
		return fmt.Errorf("build handler registry: %w", err)
	}

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()

	signalCtx, stop := signal.NotifyContext(runCtx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	go worker.RunHeartbeat(signalCtx, backend, cfg.WorkerHeartbeatInterval)
	go worker.RunCleanup(signalCtx, queries, projectID, cfg.WorkerCleanupInterval)

	dispatcher := outbox.NewDispatcher(pool, outbox.PostgresNotifier{})
	var outboxWorkers sync.WaitGroup
	outboxWorkers.Add(1)
	go func() {
		defer outboxWorkers.Done()
		runOutbox(signalCtx, dispatcher)
	}()

	slog.Info("worker started",
		"worker_id", workerRecord.ID,
		"project_id", projectID,
		"mode", "direct",
		"concurrency", concurrency,
		"job_timeout", jobTimeout,
		"rate_limit_per_minute", projectRecord.RateLimitPerMinute,
		"queue_names", queueNames,
		"max_attempts_override", maxAttemptsOverride,
		"retry_backoff", retryPolicy.Backoff,
		"retry_base_delay", retryPolicy.Base,
		"retry_max_delay", retryPolicy.Max,
	)

	runErr := runPoolWithContext(signalCtx, cfg, backend, registry, concurrency, jobTimeout, int(projectRecord.RateLimitPerMinute))
	cancelRun()
	outboxWorkers.Wait()

	offlineCtx, cancelOffline := context.WithTimeout(context.Background(), 10*time.Second)
	if err := backend.Offline(offlineCtx); err != nil {
		slog.Error("mark worker offline", "error", err)
	}
	cancelOffline()

	return runErr
}

func runPool(cfg config.Config, backend worker.Backend, registry *execution.Registry, concurrency int, jobTimeout time.Duration, rateLimit int) error {
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()

	signalCtx, stop := signal.NotifyContext(runCtx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	go worker.RunHeartbeat(signalCtx, backend, cfg.WorkerHeartbeatInterval)

	runErr := runPoolWithContext(signalCtx, cfg, backend, registry, concurrency, jobTimeout, rateLimit)
	cancelRun()

	offlineCtx, cancelOffline := context.WithTimeout(context.Background(), 10*time.Second)
	if err := backend.Offline(offlineCtx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("mark worker offline", "error", err)
	}
	cancelOffline()

	return runErr
}

func runPoolWithContext(ctx context.Context, cfg config.Config, backend worker.Backend, registry *execution.Registry, concurrency int, jobTimeout time.Duration, rateLimit int) error {
	poolRunner := worker.NewPool(backend, registry, worker.PoolConfig{
		Concurrency:     concurrency,
		PollInterval:    cfg.WorkerPollInterval,
		JobTimeout:      jobTimeout,
		ShutdownTimeout: cfg.WorkerShutdownTimeout,
		RateLimitPerMin: rateLimit,
	})
	return poolRunner.Run(ctx)
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
