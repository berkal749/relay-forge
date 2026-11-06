package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"relayforge/internal/config"
	"relayforge/internal/database"
	"relayforge/internal/database/db"
	"relayforge/internal/supervisor"
	"relayforge/internal/workertoken"
)

func main() {
	if err := run(); err != nil {
		slog.Error("supervisor stopped", "error", err)
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

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	connectCtx, cancelConnect := context.WithTimeout(ctx, cfg.DatabaseConnectTimeout)
	defer cancelConnect()

	pool, err := database.Connect(connectCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	queries := db.New(pool)
	tokens := workertoken.NewService(queries, nil, cfg.WorkerTokenPepper)

	slog.Info("supervisor started",
		"poll_interval", cfg.SupervisorPollInterval,
		"worker_binary", cfg.SupervisorWorkerBinary,
		"token_dir", cfg.SupervisorTokenDir,
		"api_url", cfg.APIBaseURL,
	)

	runner := supervisor.New(queries, tokens, supervisor.Config{
		PollInterval:  cfg.SupervisorPollInterval,
		WorkerBinary:  cfg.SupervisorWorkerBinary,
		TokenDir:      cfg.SupervisorTokenDir,
		APIBaseURL:    cfg.APIBaseURL,
		EnrollmentTTL: cfg.WorkerEnrollmentTTL,

		MaxActivePoolsPerProject: cfg.MaxActivePoolsPerProject,
		MaxConcurrencyPerProject: cfg.MaxWorkerConcurrencyPerProject,
	})

	return runner.Run(ctx)
}
