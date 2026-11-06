package worker

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"relayforge/internal/database/db"
)

func Register(ctx context.Context, queries *db.Queries, projectID, tokenID uuid.UUID, poolID *uuid.UUID, name, mode string) (db.Worker, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return db.Worker{}, fmt.Errorf("worker name cannot be empty")
	}

	created, err := queries.CreateWorker(ctx, db.CreateWorkerParams{
		ProjectID:     projectID,
		WorkerTokenID: &tokenID,
		WorkerPoolID:  poolID,
		Name:          name,
		Status:        "online",
		Mode:          mode,
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
