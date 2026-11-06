package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"relayforge/internal/database/db"
)

var ErrNoHandler = errors.New("no handler registered for job type")

type Executor interface {
	Execute(ctx context.Context, job db.Job) (json.RawMessage, error)
}

type HandlerFunc func(ctx context.Context, job db.Job) (json.RawMessage, error)

type Registry struct {
	handlers map[string]HandlerFunc
}

func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]HandlerFunc)}
}

func (r *Registry) Register(jobType string, handler HandlerFunc) {
	r.handlers[jobType] = handler
}

func (r *Registry) Execute(ctx context.Context, job db.Job) (json.RawMessage, error) {
	handler, ok := r.handlers[job.JobType]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoHandler, job.JobType)
	}
	return handler(ctx, job)
}
