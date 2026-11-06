package worker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"relayforge/internal/database/db"
)

func Register(ctx context.Context, queries *db.Queries, projectID, tokenID uuid.UUID, name string) (db.Worker, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return db.Worker{}, fmt.Errorf("worker name cannot be empty")
	}

	created, err := queries.CreateWorker(ctx, db.CreateWorkerParams{
		ProjectID:     projectID,
		WorkerTokenID: &tokenID,
		Name:          name,
		Status:        "online",
	})
	if err != nil {
		return db.Worker{}, fmt.Errorf("register worker: %w", err)
	}
	return created, nil
}

func Heartbeat(ctx context.Context, queries *db.Queries, workerID uuid.UUID) error {
	if _, err := queries.UpdateWorkerHeartbeat(ctx, workerID); err != nil {
		return fmt.Errorf("update worker heartbeat: %w", err)
	}
	return nil
}

func MarkOffline(ctx context.Context, queries *db.Queries, workerID uuid.UUID) error {
	if err := queries.MarkWorkerOffline(ctx, workerID); err != nil {
		return fmt.Errorf("mark worker offline: %w", err)
	}
	return nil
}

func RunHeartbeat(ctx context.Context, queries *db.Queries, workerID uuid.UUID, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	record := func() {
		if err := Heartbeat(ctx, queries, workerID); err != nil && ctx.Err() == nil {
			slog.Error("worker heartbeat failed", "error", err)
		}
	}

	record()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			record()
		}
	}
}
