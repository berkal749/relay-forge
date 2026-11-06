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

	"github.com/google/uuid"

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

	if cfg.WorkerProjectID == "" {
		return fmt.Errorf("WORKER_PROJECT_ID is required")
	}
	if cfg.WorkerToken == "" {
		return fmt.Errorf("WORKER_TOKEN is required")
	}
	projectID, err := uuid.Parse(cfg.WorkerProjectID)
	if err != nil {
		return fmt.Errorf("parse WORKER_PROJECT_ID: %w", err)
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
	workerToken, err := tokenService.Authenticate(registerCtx, projectID, cfg.WorkerToken)
	if err != nil {
		cancelRegister()
		return fmt.Errorf("authenticate worker token: %w", err)
	}
	workerRecord, err := worker.Register(registerCtx, queries, projectID, workerToken.ID, cfg.WorkerName)
	if err != nil {
		cancelRegister()
		return err
	}
	projectRecord, err := queries.GetProject(registerCtx, projectID)
	cancelRegister()
	if err != nil {
		return fmt.Errorf("load project settings: %w", err)
	}

	concurrency := cfg.WorkerConcurrency
	if projectRecord.MaxConcurrency > 0 && int(projectRecord.MaxConcurrency) < concurrency {
		concurrency = int(projectRecord.MaxConcurrency)
	}
	jobTimeout := cfg.WorkerJobTimeout
	if projectRecord.DefaultJobTimeoutSeconds > 0 {
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

	executionService := execution.NewService(pool, cfg.RetryBaseDelay, cfg.RetryMaxDelay)
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
