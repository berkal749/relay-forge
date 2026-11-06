package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"relayforge/internal/database/db"
	"relayforge/internal/execution"
)

// DirectBackend runs jobs through the local execution service and renews
// execution leases in PostgreSQL on heartbeat.
type DirectBackend struct {
	execution *execution.Service
	queries   *db.Queries
	projectID uuid.UUID
	workerID  uuid.UUID
	lease     time.Duration
}

func NewDirectBackend(executionService *execution.Service, queries *db.Queries, projectID, workerID uuid.UUID, lease time.Duration) *DirectBackend {
	if lease <= 0 {
		lease = 5 * time.Minute
	}
	return &DirectBackend{
		execution: executionService,
		queries:   queries,
		projectID: projectID,
		workerID:  workerID,
		lease:     lease,
	}
}

func (b *DirectBackend) Claim(ctx context.Context) (execution.Claim, error) {
	return b.execution.Claim(ctx, b.projectID, b.workerID)
}

func (b *DirectBackend) Succeed(ctx context.Context, claim execution.Claim, result []byte) error {
	return b.execution.Succeed(ctx, claim, result)
}

func (b *DirectBackend) Fail(ctx context.Context, claim execution.Claim, cause error, cancelled bool) (execution.FailureOutcome, error) {
	return b.execution.Fail(ctx, claim, cause, cancelled)
}

func (b *DirectBackend) Heartbeat(ctx context.Context) error {
	if err := Heartbeat(ctx, b.queries, b.workerID); err != nil {
		return err
	}
	_, err := b.queries.RenewWorkerLeases(ctx, db.RenewWorkerLeasesParams{
		WorkerID:     &b.workerID,
		LeaseSeconds: int32(b.lease / time.Second),
	})
	return err
}

func (b *DirectBackend) Offline(ctx context.Context) error {
	return MarkOffline(ctx, b.queries, b.workerID)
}

var _ Backend = (*DirectBackend)(nil)

// RunHeartbeat calls Backend.Heartbeat immediately and then on an interval.
func RunHeartbeat(ctx context.Context, backend Backend, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	record := func() {
		if err := backend.Heartbeat(ctx); err != nil && ctx.Err() == nil {
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
