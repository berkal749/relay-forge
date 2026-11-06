package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"relayforge/internal/database/db"
	"relayforge/internal/execution"
	"relayforge/internal/http/httpx"
	"relayforge/internal/job"
	"relayforge/internal/metrics"
	"relayforge/internal/middleware"
	"relayforge/internal/notificationchannel"
)

type WorkerRemoteConfig struct {
	LeaseGrace        time.Duration
	TrustProxyHeaders bool
	MTLSHeader        string
	Metrics           *metrics.Metrics
}

// WorkerRemoteHandler implements the API-only worker protocol. Workers
// authenticate with rf_worker_ tokens and never connect to PostgreSQL.
type WorkerRemoteHandler struct {
	queries   *db.Queries
	execution *execution.Service
	jobs      *job.Service
	channels  *notificationchannel.Service
	config    WorkerRemoteConfig
}

func NewWorkerRemoteHandler(queries *db.Queries, executionService *execution.Service, jobs *job.Service, channels *notificationchannel.Service, config WorkerRemoteConfig) *WorkerRemoteHandler {
	if config.LeaseGrace <= 0 {
		config.LeaseGrace = time.Minute
	}
	return &WorkerRemoteHandler{queries: queries, execution: executionService, jobs: jobs, channels: channels, config: config}
}

type workerRegisterRequest struct {
	Name string `json:"name"`
}

type workerIDRequest struct {
	WorkerID uuid.UUID `json:"worker_id"`
}

type workerCompleteRequest struct {
	ExecutionID uuid.UUID       `json:"execution_id"`
	Result      json.RawMessage `json:"result"`
}

type workerFailRequest struct {
	ExecutionID uuid.UUID `json:"execution_id"`
	Error       string    `json:"error"`
	Cancelled   bool      `json:"cancelled"`
	Permanent   bool      `json:"permanent"`
}

type workerSubmitRequest struct {
	QueueName   string          `json:"queue_name"`
	JobType     string          `json:"job_type"`
	Payload     json.RawMessage `json:"payload"`
	Priority    *int32          `json:"priority"`
	MaxAttempts *int32          `json:"max_attempts"`
	ScheduledAt *time.Time      `json:"scheduled_at"`
}

type workerRegisterResponse struct {
	WorkerID     uuid.UUID      `json:"worker_id"`
	ProjectID    uuid.UUID      `json:"project_id"`
	WorkerPool   *db.WorkerPool `json:"worker_pool"`
	LeaseSeconds int32          `json:"lease_seconds"`
	Project      struct {
		MaxConcurrency           int32 `json:"max_concurrency"`
		RateLimitPerMinute       int32 `json:"rate_limit_per_minute"`
		DefaultJobTimeoutSeconds int32 `json:"default_job_timeout_seconds"`
	} `json:"project"`
}

func (h *WorkerRemoteHandler) Register(w http.ResponseWriter, r *http.Request) {
	token, pool, project, ok := h.authorize(w, r)
	if !ok {
		return
	}

	var request workerRegisterRequest
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = token.Name
	}
	if strings.TrimSpace(request.Name) == "" && pool != nil {
		name = pool.Name
	}

	if err := h.queries.MarkTokenWorkersOffline(r.Context(), &token.ID); err != nil {
		slog.Warn("mark previous token workers offline", "error", err, "token_id", token.ID)
	}
	created, err := h.queries.CreateWorker(r.Context(), db.CreateWorkerParams{
		ProjectID:     project.ID,
		WorkerTokenID: &token.ID,
		WorkerPoolID:  token.PoolID,
		Name:          name,
		Status:        "online",
		Mode:          "remote",
	})
	if err != nil {
		slog.Error("register remote worker", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	_, _, _, _, lease := h.policy(pool, project)
	response := workerRegisterResponse{
		WorkerID:     created.ID,
		ProjectID:    project.ID,
		WorkerPool:   pool,
		LeaseSeconds: int32(lease / time.Second),
	}
	response.Project.MaxConcurrency = project.MaxConcurrency
	response.Project.RateLimitPerMinute = project.RateLimitPerMinute
	response.Project.DefaultJobTimeoutSeconds = project.DefaultJobTimeoutSeconds

	httpx.WriteJSON(w, http.StatusCreated, response)
}

func (h *WorkerRemoteHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	token, pool, project, ok := h.authorize(w, r)
	if !ok {
		return
	}
	var request workerIDRequest
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	worker, ok := h.workerForToken(w, r, token, pool, request.WorkerID)
	if !ok {
		return
	}

	if _, err := h.queries.UpdateWorkerHeartbeat(r.Context(), worker.ID); err != nil {
		slog.Error("update worker heartbeat", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	_, _, _, _, lease := h.policy(pool, project)
	if _, err := h.queries.RenewWorkerLeases(r.Context(), db.RenewWorkerLeasesParams{
		WorkerID:     &worker.ID,
		LeaseSeconds: int32(lease / time.Second),
	}); err != nil {
		slog.Error("renew worker leases", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *WorkerRemoteHandler) Claim(w http.ResponseWriter, r *http.Request) {
	token, pool, project, ok := h.authorize(w, r)
	if !ok {
		return
	}
	var request workerIDRequest
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	worker, ok := h.workerForToken(w, r, token, pool, request.WorkerID)
	if !ok {
		return
	}

	queueNames, maxAttempts, _, _, lease := h.policy(pool, project)
	claim, err := h.execution.ClaimJob(r.Context(), execution.ClaimParams{
		ProjectID:   project.ID,
		WorkerID:    worker.ID,
		QueueNames:  queueNames,
		MaxAttempts: maxAttempts,
		Lease:       lease,
	})
	if errors.Is(err, execution.ErrNoJobs) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		slog.Error("remote claim job", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if h.config.Metrics != nil {
		h.config.Metrics.RecordClaim(project.ID.String())
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"job":       toJobResponse(claim.Job),
		"execution": toExecutionResponse(claim.Execution),
	})
}

func (h *WorkerRemoteHandler) Complete(w http.ResponseWriter, r *http.Request) {
	token, pool, _, ok := h.authorize(w, r)
	if !ok {
		return
	}
	jobID, err := uuid.Parse(r.PathValue("jobID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid job id")
		return
	}

	var request workerCompleteRequest
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if request.ExecutionID == uuid.Nil {
		httpx.WriteError(w, http.StatusBadRequest, "execution_id is required")
		return
	}

	executionRecord, err := h.queries.GetExecution(r.Context(), request.ExecutionID)
	if err != nil || executionRecord.JobID != jobID {
		httpx.WriteError(w, http.StatusNotFound, "execution not found")
		return
	}
	worker, ok := h.workerForToken(w, r, token, pool, workerIDOf(executionRecord))
	if !ok {
		return
	}
	if err := h.execution.SucceedExecution(r.Context(), token.ProjectID, worker.ID, request.ExecutionID, request.Result); err != nil {
		h.writeExecutionError(w, "complete remote execution", err)
		return
	}
	if h.config.Metrics != nil {
		h.config.Metrics.RecordSuccess(token.ProjectID.String())
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *WorkerRemoteHandler) Fail(w http.ResponseWriter, r *http.Request) {
	token, pool, project, ok := h.authorize(w, r)
	if !ok {
		return
	}
	jobID, err := uuid.Parse(r.PathValue("jobID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid job id")
		return
	}

	var request workerFailRequest
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if request.ExecutionID == uuid.Nil {
		httpx.WriteError(w, http.StatusBadRequest, "execution_id is required")
		return
	}

	executionRecord, err := h.queries.GetExecution(r.Context(), request.ExecutionID)
	if err != nil || executionRecord.JobID != jobID {
		httpx.WriteError(w, http.StatusNotFound, "execution not found")
		return
	}
	worker, ok := h.workerForToken(w, r, token, pool, workerIDOf(executionRecord))
	if !ok {
		return
	}

	message := strings.TrimSpace(request.Error)
	if message == "" {
		message = "remote worker reported failure"
	}
	_, _, _, retryPolicy, _ := h.policy(pool, project)
	cause := errors.New(message)
	if request.Permanent {
		cause = execution.Permanent(cause)
	}
	outcome, err := h.execution.FailExecution(r.Context(), token.ProjectID, worker.ID, request.ExecutionID, cause, request.Cancelled, retryPolicy)
	if errors.Is(err, execution.ErrExecutionAlreadyApplied) {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"already_applied": true})
		return
	}
	if err != nil {
		h.writeExecutionError(w, "fail remote execution", err)
		return
	}
	if h.config.Metrics != nil {
		h.config.Metrics.RecordFailure(token.ProjectID.String())
		if outcome.Retry {
			h.config.Metrics.RecordRetry(token.ProjectID.String())
		}
	}

	response := map[string]any{
		"retry":      outcome.Retry,
		"job_status": outcome.JobStatus,
	}
	if outcome.Retry {
		response["retry_at"] = outcome.RetryAt
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}

// Submit lets a worker enqueue a follow-up job in its own project. Remote
// workers use it for delay.run without needing database access.
func (h *WorkerRemoteHandler) Submit(w http.ResponseWriter, r *http.Request) {
	_, _, project, ok := h.authorize(w, r)
	if !ok {
		return
	}
	if h.jobs == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "job submission is not configured")
		return
	}

	var request workerSubmitRequest
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var scheduledAt time.Time
	if request.ScheduledAt != nil {
		scheduledAt = *request.ScheduledAt
	}

	result, err := h.jobs.Submit(r.Context(), job.SubmitInput{
		ProjectID:      project.ID,
		QueueName:      request.QueueName,
		JobType:        request.JobType,
		Payload:        request.Payload,
		Priority:       request.Priority,
		MaxAttempts:    request.MaxAttempts,
		ScheduledAt:    scheduledAt,
		IdempotencyKey: strings.TrimSpace(r.Header.Get("Idempotency-Key")),
	})
	if err != nil {
		writeJobError(w, "submit worker job", err)
		return
	}

	if result.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"job": map[string]any{
			"id":           result.Job.ID,
			"scheduled_at": result.Job.ScheduledAt,
			"status":       result.Job.Status,
		},
	})
}

// ResolveNotificationChannel returns the decrypted destination for a channel in
// the worker token's project. Job payloads only carry the channel ID.
func (h *WorkerRemoteHandler) ResolveNotificationChannel(w http.ResponseWriter, r *http.Request) {
	token, _, _, ok := h.authorize(w, r)
	if !ok {
		return
	}
	if h.channels == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "notification channels are not configured")
		return
	}
	channelID, err := uuid.Parse(r.PathValue("channelID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid channel id")
		return
	}
	resolved, err := h.channels.Resolve(r.Context(), token.ProjectID, channelID)
	if errors.Is(err, notificationchannel.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "notification channel not found")
		return
	}
	if err != nil {
		slog.Error("resolve notification channel", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"channel": map[string]any{
			"id":     resolved.ID,
			"kind":   resolved.Kind,
			"config": resolved.Config,
		},
	})
}

func (h *WorkerRemoteHandler) Offline(w http.ResponseWriter, r *http.Request) {
	token, pool, _, ok := h.authorize(w, r)
	if !ok {
		return
	}
	var request workerIDRequest
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	worker, ok := h.workerForToken(w, r, token, pool, request.WorkerID)
	if !ok {
		return
	}
	if err := h.queries.MarkWorkerOffline(r.Context(), worker.ID); err != nil {
		slog.Error("mark remote worker offline", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *WorkerRemoteHandler) authorize(w http.ResponseWriter, r *http.Request) (db.WorkerToken, *db.WorkerPool, db.Project, bool) {
	token, ok := middleware.WorkerTokenFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "worker token required")
		return db.WorkerToken{}, nil, db.Project{}, false
	}

	project, err := h.queries.GetProject(r.Context(), token.ProjectID)
	if err != nil {
		httpx.WriteError(w, http.StatusUnauthorized, "worker project is not active")
		return db.WorkerToken{}, nil, db.Project{}, false
	}

	var pool *db.WorkerPool
	if token.PoolID != nil {
		found, err := h.queries.GetWorkerPool(r.Context(), db.GetWorkerPoolParams{
			ID:        *token.PoolID,
			ProjectID: token.ProjectID,
		})
		if err != nil {
			httpx.WriteError(w, http.StatusForbidden, "worker pool is not active")
			return db.WorkerToken{}, nil, db.Project{}, false
		}
		pool = &found
	}

	if h.config.MTLSHeader != "" && strings.TrimSpace(r.Header.Get(h.config.MTLSHeader)) == "" {
		httpx.WriteError(w, http.StatusForbidden, "client certificate is required")
		return db.WorkerToken{}, nil, db.Project{}, false
	}

	if pool != nil && len(pool.AllowedCidrs) > 0 {
		ip := httpx.ClientIP(r, h.config.TrustProxyHeaders)
		if ip == nil || !cidrAllowed(ip, pool.AllowedCidrs) {
			slog.Warn("rejected worker from disallowed address", "pool_id", pool.ID, "remote_addr", r.RemoteAddr)
			httpx.WriteError(w, http.StatusForbidden, "worker address is not allowed for this pool")
			return db.WorkerToken{}, nil, db.Project{}, false
		}
	}

	return token, pool, project, true
}

func (h *WorkerRemoteHandler) workerForToken(w http.ResponseWriter, r *http.Request, token db.WorkerToken, pool *db.WorkerPool, workerID uuid.UUID) (db.Worker, bool) {
	if workerID == uuid.Nil {
		httpx.WriteError(w, http.StatusBadRequest, "worker_id is required")
		return db.Worker{}, false
	}
	worker, err := h.queries.GetWorkerForProject(r.Context(), db.GetWorkerForProjectParams{
		ID:        workerID,
		ProjectID: token.ProjectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteError(w, http.StatusForbidden, "worker is not registered for this token")
		return db.Worker{}, false
	}
	if err != nil {
		slog.Error("load remote worker", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
		return db.Worker{}, false
	}
	if worker.WorkerTokenID == nil || *worker.WorkerTokenID != token.ID {
		httpx.WriteError(w, http.StatusForbidden, "worker token mismatch")
		return db.Worker{}, false
	}
	if pool != nil && (worker.WorkerPoolID == nil || *worker.WorkerPoolID != pool.ID) {
		httpx.WriteError(w, http.StatusForbidden, "worker pool mismatch")
		return db.Worker{}, false
	}
	return worker, true
}

func (h *WorkerRemoteHandler) policy(pool *db.WorkerPool, project db.Project) ([]string, int32, time.Duration, execution.RetryPolicy, time.Duration) {
	queueNames := []string(nil)
	var maxAttempts int32
	jobTimeout := 5 * time.Minute
	retryPolicy := execution.RetryPolicy{
		Backoff: execution.BackoffExponential,
		Base:    5 * time.Second,
		Max:     5 * time.Minute,
	}

	if project.DefaultJobTimeoutSeconds > 0 {
		jobTimeout = time.Duration(project.DefaultJobTimeoutSeconds) * time.Second
	}
	if pool != nil {
		queueNames = pool.QueueNames
		maxAttempts = pool.MaxAttempts
		if pool.JobTimeoutSeconds > 0 {
			jobTimeout = time.Duration(pool.JobTimeoutSeconds) * time.Second
		}
		if pool.RetryBackoff != "" {
			retryPolicy.Backoff = pool.RetryBackoff
		}
		if pool.RetryBaseDelaySeconds > 0 {
			retryPolicy.Base = time.Duration(pool.RetryBaseDelaySeconds) * time.Second
		}
		if pool.RetryMaxDelaySeconds > 0 {
			retryPolicy.Max = time.Duration(pool.RetryMaxDelaySeconds) * time.Second
		}
	}
	if retryPolicy.Max < retryPolicy.Base {
		retryPolicy.Max = retryPolicy.Base
	}

	return queueNames, maxAttempts, jobTimeout, retryPolicy, jobTimeout + h.config.LeaseGrace
}

func (h *WorkerRemoteHandler) writeExecutionError(w http.ResponseWriter, operation string, err error) {
	switch {
	case errors.Is(err, execution.ErrExecutionNotFound):
		httpx.WriteError(w, http.StatusNotFound, "execution not found")
	case errors.Is(err, execution.ErrExecutionNotRunning):
		httpx.WriteError(w, http.StatusConflict, "execution is no longer running")
	case errors.Is(err, execution.ErrResultNotValidJSON):
		httpx.WriteError(w, http.StatusBadRequest, "result must be valid JSON")
	case errors.Is(err, execution.ErrResultPayloadTooBig):
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "result must be at most 1 MiB")
	default:
		slog.Error(operation, "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
	}
}

func workerIDOf(executionRecord db.Execution) uuid.UUID {
	if executionRecord.WorkerID == nil {
		return uuid.Nil
	}
	return *executionRecord.WorkerID
}

func cidrAllowed(ip net.IP, cidrs []string) bool {
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}
