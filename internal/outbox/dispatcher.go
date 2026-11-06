package outbox

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"relayforge/internal/database/db"
)

const (
	defaultBatchSize = 100
	defaultChannel   = "relayforge_jobs"
)

type Notifier interface {
	Notify(ctx context.Context, tx pgx.Tx, jobID uuid.UUID) error
}

type PostgresNotifier struct {
	Channel string
}

func (n PostgresNotifier) Notify(ctx context.Context, tx pgx.Tx, jobID uuid.UUID) error {
	channel := n.Channel
	if channel == "" {
		channel = defaultChannel
	}
	if _, err := tx.Exec(ctx, "SELECT pg_notify($1, $2)", channel, jobID.String()); err != nil {
		return fmt.Errorf("publish job notification: %w", err)
	}
	return nil
}

type Dispatcher struct {
	pool      *pgxpool.Pool
	notifier  Notifier
	batchSize int32
}

func NewDispatcher(pool *pgxpool.Pool, notifier Notifier) *Dispatcher {
	return &Dispatcher{
		pool:      pool,
		notifier:  notifier,
		batchSize: defaultBatchSize,
	}
}

// ProcessBatch publishes pending outbox messages and marks them processed in one transaction.
func (d *Dispatcher) ProcessBatch(ctx context.Context) (int, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin outbox transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := db.New(tx)
	messages, err := queries.ClaimOutboxMessages(ctx, d.batchSize)
	if err != nil {
		return 0, fmt.Errorf("claim outbox messages: %w", err)
	}

	for _, message := range messages {
		if err := d.notifier.Notify(ctx, tx, message.JobID); err != nil {
			return 0, err
		}
		if _, err := queries.MarkOutboxMessageProcessed(ctx, message.ID); err != nil {
			return 0, fmt.Errorf("mark outbox message processed: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit outbox transaction: %w", err)
	}

	return len(messages), nil
}
