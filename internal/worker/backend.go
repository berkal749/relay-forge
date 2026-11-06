package worker

import (
	"context"

	"relayforge/internal/execution"
)

// Backend is the job transport a worker pool uses. Direct workers talk to
// PostgreSQL; remote workers talk to the API and never open a database
// connection.
type Backend interface {
	Claim(ctx context.Context) (execution.Claim, error)
	Succeed(ctx context.Context, claim execution.Claim, result []byte) error
	Fail(ctx context.Context, claim execution.Claim, cause error, cancelled bool) (execution.FailureOutcome, error)
	Heartbeat(ctx context.Context) error
	Offline(ctx context.Context) error
}
