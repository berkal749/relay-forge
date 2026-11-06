package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"relayforge/internal/apikey"
	"relayforge/internal/auth"
	"relayforge/internal/config"
	"relayforge/internal/database"
	"relayforge/internal/database/db"
	"relayforge/internal/execution"
	"relayforge/internal/http/handlers"
	"relayforge/internal/http/routes"
	"relayforge/internal/invite"
	"relayforge/internal/job"
	"relayforge/internal/metrics"
	"relayforge/internal/middleware"
	"relayforge/internal/outbox"
	"relayforge/internal/project"
	"relayforge/internal/workerpool"
	"relayforge/internal/workertoken"
)

func main() {
	if err := run(); err != nil {
		slog.Error("API stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.ValidateAPI(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.DatabaseConnectTimeout)
	defer cancel()

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	queries := db.New(pool)
	authService := auth.NewService(queries, cfg.SessionTTL, auth.Options{
		Pool:                pool,
		RegistrationMode:    cfg.RegistrationMode,
		BootstrapAdminEmail: cfg.BootstrapAdminEmail,
	})
	projectService := project.NewService(queries, project.Limits{
		MaxProjectsPerUser: cfg.MaxProjectsPerUser,
	})
	apiKeyService := apikey.NewService(queries, projectService, cfg.APIKeyPepper)
	workerTokenService := workertoken.NewService(queries, projectService, cfg.WorkerTokenPepper)
	workerPoolService := workerpool.NewService(queries, projectService, workerTokenService, workerpool.Limits{
		MaxPoolsPerProject:       cfg.MaxWorkerPoolsPerProject,
		MaxActivePoolsPerProject: cfg.MaxActivePoolsPerProject,
		MaxConcurrencyPerProject: cfg.MaxWorkerConcurrencyPerProject,
	})
	jobService := job.NewService(pool, job.Limits{
		MaxQueuedJobsPerProject: cfg.MaxQueuedJobsPerProject,
	})
	inviteService := invite.NewService(queries, cfg.InviteTTL)
	appMetrics := metrics.New()

	slog.Info("API configuration", cfg.LogArgs()...)

	defaultRetry := execution.RetryPolicy{
		Backoff: execution.BackoffExponential,
		Base:    cfg.RetryBaseDelay,
		Max:     cfg.RetryMaxDelay,
	}
	executionService := execution.NewService(pool, defaultRetry, nil, 0, 0)

	router := routes.New(routes.Dependencies{
		Auth:         handlers.NewAuthHandler(authService),
		Invites:      handlers.NewInviteHandler(inviteService),
		Projects:     handlers.NewProjectHandler(projectService),
		APIKeys:      handlers.NewAPIKeyHandler(apiKeyService),
		WorkerTokens: handlers.NewWorkerTokenHandler(workerTokenService, cfg.TokenRotationGrace),
		WorkerPools:  handlers.NewWorkerPoolHandler(workerPoolService),
		Enrollment:   handlers.NewEnrollmentHandler(workerTokenService),
		WorkerRemote: handlers.NewWorkerRemoteHandler(queries, executionService, handlers.WorkerRemoteConfig{
			LeaseGrace:        cfg.WorkerLeaseGrace,
			TrustProxyHeaders: cfg.TrustProxyHeaders,
			MTLSHeader:        cfg.WorkerMTLSHeader,
			Metrics:           appMetrics,
		}),
		Jobs:                     handlers.NewJobHandler(jobService, projectService),
		Authenticator:            authService,
		APIKeyAuthenticator:      apiKeyService,
		WorkerTokenAuthenticator: workerTokenService,
		RateLimits: middleware.RateLimiters{
			Auth:      middleware.NewRateLimiter(cfg.RateLimitAuthPerMinute),
			API:       middleware.NewRateLimiter(cfg.RateLimitAPIPerMinute),
			JobSubmit: middleware.NewRateLimiter(cfg.RateLimitJobSubmitPerMinute),
		},
		Ready: func(ctx context.Context) error {
			return pool.Ping(ctx)
		},
	})

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           appMetrics.Middleware(router),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdownSignal, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	backgroundCtx, cancelBackground := context.WithCancel(context.Background())
	defer cancelBackground()

	var background sync.WaitGroup
	startBackground(&background, func() {
		runOutbox(backgroundCtx, outbox.NewDispatcher(pool, outbox.PostgresNotifier{}), cfg.APIOutboxInterval, queries, appMetrics)
	})
	startBackground(&background, func() {
		runCleanup(backgroundCtx, queries, cfg.APICleanupInterval)
	})
	startBackground(&background, func() {
		runLeaseReaper(backgroundCtx, executionService, queries, defaultRetry, cfg.APIReaperInterval, appMetrics)
	})
	startBackground(&background, func() {
		runWorkerSweep(backgroundCtx, queries, cfg.WorkerStaleAfter, cfg.APIWorkerSweepInterval)
	})
	startBackground(&background, func() {
		runMetricsCollector(backgroundCtx, queries, appMetrics)
	})

	var metricsServer *http.Server
	if cfg.MetricsEnabled {
		metricsMux := http.NewServeMux()
		metricsMux.Handle("GET /metrics", appMetrics.Handler())
		metricsServer = &http.Server{
			Addr:              cfg.MetricsAddr,
			Handler:           metricsMux,
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			slog.Info("metrics listening", "address", cfg.MetricsAddr)
			if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("metrics server stopped", "error", err)
			}
		}()
	}

	serverError := make(chan error, 1)
	go func() {
		slog.Info("API listening", "address", cfg.HTTPAddr)
		serverError <- server.ListenAndServe()
	}()

	select {
	case err := <-serverError:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-shutdownSignal.Done():
		slog.Info("shutting down API")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	err = server.Shutdown(shutdownCtx)
	if metricsServer != nil {
		_ = metricsServer.Shutdown(shutdownCtx)
	}
	cancelBackground()
	background.Wait()
	return err
}

func startBackground(group *sync.WaitGroup, run func()) {
	group.Add(1)
	go func() {
		defer group.Done()
		run()
	}()
}

func runOutbox(ctx context.Context, dispatcher *outbox.Dispatcher, interval time.Duration, queries *db.Queries, appMetrics *metrics.Metrics) {
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		processed, err := dispatcher.ProcessBatch(ctx)
		if err != nil && ctx.Err() == nil {
			slog.Error("process outbox batch", "error", err)
		} else if processed > 0 {
			slog.Info("published outbox messages", "count", processed)
		}
		if backlog, err := queries.CountOutboxBacklog(ctx); err == nil {
			appMetrics.SetOutboxBacklog(float64(backlog))
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runCleanup(ctx context.Context, queries *db.Queries, interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		deleted, err := queries.DeleteExpiredJobsAllProjects(ctx)
		if err != nil && ctx.Err() == nil {
			slog.Error("delete expired jobs", "error", err)
		} else if deleted > 0 {
			slog.Info("deleted expired jobs", "count", deleted)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runLeaseReaper(ctx context.Context, service *execution.Service, queries *db.Queries, fallback execution.RetryPolicy, interval time.Duration, appMetrics *metrics.Metrics) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		reaped, err := service.ReapExpired(ctx, 100, func(job db.Job, record db.Execution) execution.RetryPolicy {
			return retryPolicyFor(ctx, queries, job, record, fallback)
		})
		if err != nil && ctx.Err() == nil {
			slog.Error("reap expired worker leases", "error", err)
		} else if reaped > 0 {
			slog.Warn("reaped expired worker leases", "count", reaped)
			appMetrics.RecordLeaseReaped(reaped)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func retryPolicyFor(ctx context.Context, queries *db.Queries, job db.Job, record db.Execution, fallback execution.RetryPolicy) execution.RetryPolicy {
	if record.WorkerID == nil {
		return fallback
	}
	worker, err := queries.GetWorker(ctx, *record.WorkerID)
	if err != nil || worker.WorkerPoolID == nil {
		return fallback
	}
	workerPool, err := queries.GetWorkerPool(ctx, db.GetWorkerPoolParams{
		ID:        *worker.WorkerPoolID,
		ProjectID: job.ProjectID,
	})
	if err != nil {
		return fallback
	}

	policy := execution.RetryPolicy{
		Backoff: workerPool.RetryBackoff,
		Base:    time.Duration(workerPool.RetryBaseDelaySeconds) * time.Second,
		Max:     time.Duration(workerPool.RetryMaxDelaySeconds) * time.Second,
	}
	if policy.Backoff == "" {
		policy.Backoff = fallback.Backoff
	}
	if policy.Base <= 0 {
		policy.Base = fallback.Base
	}
	if policy.Max <= 0 {
		policy.Max = fallback.Max
	}
	if policy.Max < policy.Base {
		policy.Max = policy.Base
	}
	return policy
}

func runWorkerSweep(ctx context.Context, queries *db.Queries, staleAfter, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if staleAfter <= 0 {
		staleAfter = 90 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		offline, err := queries.MarkStaleWorkersOffline(ctx, int32(staleAfter/time.Second))
		if err != nil && ctx.Err() == nil {
			slog.Error("mark stale workers offline", "error", err)
		} else if offline > 0 {
			slog.Warn("marked stale workers offline", "count", offline)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runMetricsCollector(ctx context.Context, queries *db.Queries, appMetrics *metrics.Metrics) {
	const interval = 30 * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	collect := func() {
		if depths, err := queries.QueueDepthByProject(ctx); err == nil {
			appMetrics.ResetQueueDepth()
			for _, row := range depths {
				appMetrics.SetQueueDepth(row.ProjectID.String(), float64(row.Depth))
			}
		}
		if workers, err := queries.OnlineWorkersByProject(ctx); err == nil {
			appMetrics.ResetOnlineWorkers()
			for _, row := range workers {
				appMetrics.SetOnlineWorkers(row.ProjectID.String(), float64(row.Online))
			}
		}
	}

	collect()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			collect()
		}
	}
}
