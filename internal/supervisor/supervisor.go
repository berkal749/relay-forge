// Package supervisor reconciles project worker pools with running worker
// processes. It is the automation layer that turns database rows into workers.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"relayforge/internal/database/db"
	"relayforge/internal/workertoken"
)

const stopTimeout = 60 * time.Second

type Config struct {
	PollInterval  time.Duration
	WorkerBinary  string
	TokenDir      string
	APIBaseURL    string
	EnrollmentTTL time.Duration
}

type child struct {
	poolID uuid.UUID
	spec   string
	cmd    *exec.Cmd
	done   chan struct{}
}

type Supervisor struct {
	queries *db.Queries
	tokens  *workertoken.Service
	config  Config

	mu       sync.Mutex
	children map[uuid.UUID]*child
}

func New(queries *db.Queries, tokens *workertoken.Service, config Config) *Supervisor {
	return &Supervisor{
		queries:  queries,
		tokens:   tokens,
		config:   config,
		children: make(map[uuid.UUID]*child),
	}
}

// Run reconciles worker pools until ctx is cancelled, then stops all children.
func (s *Supervisor) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()

	s.reconcile(ctx)

	for {
		select {
		case <-ctx.Done():
			s.stopAll()
			return nil
		case <-ticker.C:
			s.reconcile(ctx)
		}
	}
}

func (s *Supervisor) reconcile(ctx context.Context) {
	pools, err := s.queries.ListActiveWorkerPools(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("list active worker pools", "error", err)
		}
		return
	}

	active := make(map[uuid.UUID]db.WorkerPool, len(pools))
	for _, pool := range pools {
		active[pool.ID] = pool
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for id, process := range s.children {
		pool, ok := active[id]
		if !ok {
			slog.Info("worker pool is no longer active; stopping worker", "pool_id", id)
			s.stopLocked(id, process)
			continue
		}
		if process.spec != specFor(pool) {
			slog.Info("worker pool configuration changed; restarting worker", "pool_id", id)
			s.stopLocked(id, process)
		}
	}

	for id, pool := range active {
		if _, running := s.children[id]; running {
			continue
		}
		if err := s.startLocked(ctx, pool); err != nil {
			slog.Error("start worker pool", "error", err, "pool_id", id)
		}
	}
}

func (s *Supervisor) startLocked(ctx context.Context, pool db.WorkerPool) error {
	enrollment, err := s.tokens.CreateEnrollmentToken(ctx, pool.ProjectID, pool.ID, pool.Name, s.config.EnrollmentTTL)
	if err != nil {
		return fmt.Errorf("issue enrollment token: %w", err)
	}

	if err := os.MkdirAll(s.config.TokenDir, 0o700); err != nil {
		return fmt.Errorf("create token directory: %w", err)
	}
	tokenFile := filepath.Join(s.config.TokenDir, "pool-"+pool.ID.String()+".json")

	command := exec.Command(s.config.WorkerBinary)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Env = mergeEnv(os.Environ(), map[string]string{
		"WORKER_TOKEN":            "",
		"WORKER_ENROLLMENT_TOKEN": enrollment.RawToken,
		"WORKER_TOKEN_FILE":       tokenFile,
		"WORKER_PROJECT_ID":       pool.ProjectID.String(),
		"WORKER_NAME":             pool.Name,
		"WORKER_CONCURRENCY":      strconv.Itoa(int(pool.Concurrency)),
		"RELAYFORGE_API_URL":      s.config.APIBaseURL,
	})

	if err := command.Start(); err != nil {
		return fmt.Errorf("start worker process: %w", err)
	}

	process := &child{
		poolID: pool.ID,
		spec:   specFor(pool),
		cmd:    command,
		done:   make(chan struct{}),
	}
	s.children[pool.ID] = process

	slog.Info("worker started for pool",
		"pool_id", pool.ID,
		"project_id", pool.ProjectID,
		"name", pool.Name,
		"concurrency", pool.Concurrency,
		"pid", command.Process.Pid,
	)

	go s.wait(pool.ID, process)
	return nil
}

func (s *Supervisor) wait(poolID uuid.UUID, process *child) {
	err := process.cmd.Wait()
	close(process.done)

	s.mu.Lock()
	if s.children[poolID] == process {
		delete(s.children, poolID)
	}
	s.mu.Unlock()

	if err != nil {
		slog.Warn("worker process exited", "pool_id", poolID, "error", err)
		return
	}
	slog.Info("worker process exited", "pool_id", poolID)
}

func (s *Supervisor) stopLocked(poolID uuid.UUID, process *child) {
	delete(s.children, poolID)
	if process.cmd.Process == nil {
		return
	}

	if err := process.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		slog.Warn("signal worker process", "pool_id", poolID, "error", err)
	}

	go func() {
		select {
		case <-process.done:
		case <-time.After(stopTimeout):
			slog.Warn("worker did not stop in time; killing", "pool_id", poolID)
			_ = process.cmd.Process.Kill()
		}
	}()
}

func (s *Supervisor) stopAll() {
	s.mu.Lock()
	children := make([]*child, 0, len(s.children))
	ids := make([]uuid.UUID, 0, len(s.children))
	for id, process := range s.children {
		ids = append(ids, id)
		children = append(children, process)
	}
	s.children = make(map[uuid.UUID]*child)
	s.mu.Unlock()

	for i, process := range children {
		if process.cmd.Process != nil {
			_ = process.cmd.Process.Signal(syscall.SIGTERM)
		}
		select {
		case <-process.done:
		case <-time.After(stopTimeout):
			_ = process.cmd.Process.Kill()
			<-process.done
		}
		slog.Info("worker stopped", "pool_id", ids[i])
	}
}

func specFor(pool db.WorkerPool) string {
	return pool.Name + "|" + strconv.Itoa(int(pool.Concurrency))
}

func mergeEnv(base []string, overrides map[string]string) []string {
	env := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		name, _, _ := strings.Cut(entry, "=")
		if _, ok := overrides[name]; ok {
			continue
		}
		env = append(env, entry)
	}
	for name, value := range overrides {
		env = append(env, name+"="+value)
	}
	return env
}
