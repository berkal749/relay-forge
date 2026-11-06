package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"relayforge/internal/apikey"
	"relayforge/internal/auth"
	"relayforge/internal/config"
	"relayforge/internal/database"
	"relayforge/internal/database/db"
	"relayforge/internal/http/handlers"
	"relayforge/internal/http/routes"
	"relayforge/internal/job"
	"relayforge/internal/middleware"
	"relayforge/internal/project"
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

	ctx, cancel := context.WithTimeout(context.Background(), cfg.DatabaseConnectTimeout)
	defer cancel()

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	queries := db.New(pool)
	authService := auth.NewService(queries, cfg.SessionTTL)
	projectService := project.NewService(queries)
	apiKeyService := apikey.NewService(queries, projectService, cfg.APIKeyPepper)
	workerTokenService := workertoken.NewService(queries, projectService, cfg.WorkerTokenPepper)
	jobService := job.NewService(pool)

	router := routes.New(routes.Dependencies{
		Auth:                handlers.NewAuthHandler(authService),
		Projects:            handlers.NewProjectHandler(projectService),
		APIKeys:             handlers.NewAPIKeyHandler(apiKeyService),
		WorkerTokens:        handlers.NewWorkerTokenHandler(workerTokenService),
		Jobs:                handlers.NewJobHandler(jobService, projectService),
		Authenticator:       authService,
		APIKeyAuthenticator: apiKeyService,
		RateLimits: middleware.RateLimiters{
			Auth:      middleware.NewRateLimiter(cfg.RateLimitAuthPerMinute),
			API:       middleware.NewRateLimiter(cfg.RateLimitAPIPerMinute),
			JobSubmit: middleware.NewRateLimiter(cfg.RateLimitJobSubmitPerMinute),
		},
	})

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdownSignal, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	return server.Shutdown(shutdownCtx)
}
