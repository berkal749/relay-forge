package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"relayforge/internal/database/db"
	"relayforge/internal/http/httpx"
	"relayforge/internal/middleware"
	"relayforge/internal/project"
	"relayforge/internal/workerpool"
	"relayforge/internal/workertoken"
)

type WorkerPoolHandler struct {
	service *workerpool.Service
}

type workerPoolRequest struct {
	Name                  string   `json:"name"`
	Concurrency           *int32   `json:"concurrency"`
	QueueNames            []string `json:"queue_names"`
	JobTimeoutSeconds     int32    `json:"job_timeout_seconds"`
	MaxAttempts           int32    `json:"max_attempts"`
	RetryBackoff          string   `json:"retry_backoff"`
	RetryBaseDelaySeconds int32    `json:"retry_base_delay_seconds"`
	RetryMaxDelaySeconds  int32    `json:"retry_max_delay_seconds"`
	AllowedCIDRs          []string `json:"allowed_cidrs"`
}

type workerPoolUpdateRequest struct {
	Name                  *string   `json:"name"`
	Concurrency           *int32    `json:"concurrency"`
	Status                *string   `json:"status"`
	QueueNames            *[]string `json:"queue_names"`
	JobTimeoutSeconds     *int32    `json:"job_timeout_seconds"`
	MaxAttempts           *int32    `json:"max_attempts"`
	RetryBackoff          *string   `json:"retry_backoff"`
	RetryBaseDelaySeconds *int32    `json:"retry_base_delay_seconds"`
	RetryMaxDelaySeconds  *int32    `json:"retry_max_delay_seconds"`
	AllowedCIDRs          *[]string `json:"allowed_cidrs"`
}

type enrollmentTokenRequest struct {
	Name             string `json:"name"`
	ExpiresInMinutes *int32 `json:"expires_in_minutes"`
}

type workerPoolResponse struct {
	ID                    uuid.UUID `json:"id"`
	ProjectID             uuid.UUID `json:"project_id"`
	Name                  string    `json:"name"`
	Concurrency           int32     `json:"concurrency"`
	Status                string    `json:"status"`
	QueueNames            []string  `json:"queue_names"`
	JobTimeoutSeconds     int32     `json:"job_timeout_seconds"`
	MaxAttempts           int32     `json:"max_attempts"`
	RetryBackoff          string    `json:"retry_backoff"`
	RetryBaseDelaySeconds int32     `json:"retry_base_delay_seconds"`
	RetryMaxDelaySeconds  int32     `json:"retry_max_delay_seconds"`
	AllowedCIDRs          []string  `json:"allowed_cidrs"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

func NewWorkerPoolHandler(service *workerpool.Service) *WorkerPoolHandler {
	return &WorkerPoolHandler{service: service}
}

func (h *WorkerPoolHandler) Create(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	projectID, err := uuid.Parse(r.PathValue("projectID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid project id")
		return
	}

	var request workerPoolRequest
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var concurrency int32 = workerpool.DefaultConcurrency
	if request.Concurrency != nil {
		concurrency = *request.Concurrency
	}

	pool, err := h.service.Create(r.Context(), session.User.ID, projectID, workerpool.CreateInput{
		Name:                  request.Name,
		Concurrency:           concurrency,
		QueueNames:            request.QueueNames,
		JobTimeoutSeconds:     request.JobTimeoutSeconds,
		MaxAttempts:           request.MaxAttempts,
		RetryBackoff:          request.RetryBackoff,
		RetryBaseDelaySeconds: request.RetryBaseDelaySeconds,
		RetryMaxDelaySeconds:  request.RetryMaxDelaySeconds,
		AllowedCIDRs:          request.AllowedCIDRs,
	})
	if err != nil {
		writeWorkerPoolError(w, "create worker pool", err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"worker_pool": toWorkerPoolResponse(pool)})
}

func (h *WorkerPoolHandler) List(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	projectID, err := uuid.Parse(r.PathValue("projectID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid project id")
		return
	}

	pools, err := h.service.List(r.Context(), session.User.ID, projectID)
	if err != nil {
		writeWorkerPoolError(w, "list worker pools", err)
		return
	}

	response := make([]workerPoolResponse, 0, len(pools))
	for _, pool := range pools {
		response = append(response, toWorkerPoolResponse(pool))
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{"worker_pools": response})
}

func (h *WorkerPoolHandler) Usage(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	projectID, err := uuid.Parse(r.PathValue("projectID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid project id")
		return
	}

	usage, err := h.service.Usage(r.Context(), session.User.ID, projectID)
	if err != nil {
		writeWorkerPoolError(w, "worker pool usage", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{"usage": usage})
}

func (h *WorkerPoolHandler) Update(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	projectID, err := uuid.Parse(r.PathValue("projectID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	poolID, err := uuid.Parse(r.PathValue("poolID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid worker pool id")
		return
	}

	var request workerPoolUpdateRequest
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	pool, err := h.service.Update(r.Context(), session.User.ID, projectID, poolID, workerpool.UpdateInput{
		Name:                  request.Name,
		Concurrency:           request.Concurrency,
		Status:                request.Status,
		QueueNames:            request.QueueNames,
		JobTimeoutSeconds:     request.JobTimeoutSeconds,
		MaxAttempts:           request.MaxAttempts,
		RetryBackoff:          request.RetryBackoff,
		RetryBaseDelaySeconds: request.RetryBaseDelaySeconds,
		RetryMaxDelaySeconds:  request.RetryMaxDelaySeconds,
		AllowedCIDRs:          request.AllowedCIDRs,
	})
	if err != nil {
		writeWorkerPoolError(w, "update worker pool", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{"worker_pool": toWorkerPoolResponse(pool)})
}

func (h *WorkerPoolHandler) Delete(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	projectID, err := uuid.Parse(r.PathValue("projectID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	poolID, err := uuid.Parse(r.PathValue("poolID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid worker pool id")
		return
	}

	if err := h.service.Delete(r.Context(), session.User.ID, projectID, poolID); err != nil {
		writeWorkerPoolError(w, "delete worker pool", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *WorkerPoolHandler) CreateEnrollmentToken(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	projectID, err := uuid.Parse(r.PathValue("projectID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	poolID, err := uuid.Parse(r.PathValue("poolID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid worker pool id")
		return
	}

	var request enrollmentTokenRequest
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var ttl time.Duration
	if request.ExpiresInMinutes != nil {
		if *request.ExpiresInMinutes < 1 || *request.ExpiresInMinutes > 1440 {
			httpx.WriteError(w, http.StatusBadRequest, "expires_in_minutes must be between 1 and 1440")
			return
		}
		ttl = time.Duration(*request.ExpiresInMinutes) * time.Minute
	}

	token, err := h.service.CreateEnrollmentToken(r.Context(), session.User.ID, projectID, poolID, request.Name, ttl)
	if err != nil {
		writeWorkerPoolError(w, "create enrollment token", err)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"enrollment_token": toEnrollmentTokenResponse(token.Token),
		"token":            token.RawToken,
	})
}

type workerPoolStatResponse struct {
	PoolID             uuid.UUID `json:"pool_id"`
	OnlineWorkers      int32     `json:"online_workers"`
	OfflineWorkers     int32     `json:"offline_workers"`
	RunningJobs        int32     `json:"running_jobs"`
	QueueDepth         int32     `json:"queue_depth"`
	JobsPerMinute      float64   `json:"jobs_per_minute"`
	SuccessRate        float64   `json:"success_rate"`
	AvgDurationSeconds float64   `json:"avg_duration_seconds"`
}

type workerResponse struct {
	ID              uuid.UUID  `json:"id"`
	ProjectID       uuid.UUID  `json:"project_id"`
	WorkerPoolID    *uuid.UUID `json:"worker_pool_id"`
	Name            string     `json:"name"`
	Status          string     `json:"status"`
	Mode            string     `json:"mode"`
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at"`
	CreatedAt       time.Time  `json:"created_at"`
	RunningJobs     int32      `json:"running_jobs"`
}

func (h *WorkerPoolHandler) Stats(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	projectID, err := uuid.Parse(r.PathValue("projectID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	stats, err := h.service.Stats(r.Context(), session.User.ID, projectID)
	if err != nil {
		writeWorkerPoolError(w, "worker pool stats", err)
		return
	}
	response := make([]workerPoolStatResponse, 0, len(stats))
	for _, item := range stats {
		completed := item.SucceededLastHour + item.FailedLastHour
		rate := 0.0
		if completed > 0 {
			rate = float64(item.SucceededLastHour) / float64(completed) * 100
		}
		response = append(response, workerPoolStatResponse{
			PoolID:             item.PoolID,
			OnlineWorkers:      item.OnlineWorkers,
			OfflineWorkers:     item.OfflineWorkers,
			RunningJobs:        item.RunningJobs,
			QueueDepth:         item.QueueDepth,
			JobsPerMinute:      float64(completed) / 60,
			SuccessRate:        rate,
			AvgDurationSeconds: item.AvgDurationSeconds,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"stats": response})
}

func (h *WorkerPoolHandler) Workers(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	projectID, err := uuid.Parse(r.PathValue("projectID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	var poolID *uuid.UUID
	if value := r.URL.Query().Get("pool_id"); value != "" {
		parsed, err := uuid.Parse(value)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid pool id")
			return
		}
		poolID = &parsed
	}
	workers, err := h.service.Workers(r.Context(), session.User.ID, projectID, poolID)
	if err != nil {
		writeWorkerPoolError(w, "list workers", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"workers": toWorkerResponses(workers)})
}

func (h *WorkerPoolHandler) PoolWorkers(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	projectID, err := uuid.Parse(r.PathValue("projectID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	poolID, err := uuid.Parse(r.PathValue("poolID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid pool id")
		return
	}
	workers, err := h.service.Workers(r.Context(), session.User.ID, projectID, &poolID)
	if err != nil {
		writeWorkerPoolError(w, "list pool workers", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"workers": toWorkerResponses(workers)})
}

func toWorkerResponses(workers []db.ListWorkersWithLoadRow) []workerResponse {
	response := make([]workerResponse, 0, len(workers))
	for _, item := range workers {
		response = append(response, workerResponse{
			ID:              item.ID,
			ProjectID:       item.ProjectID,
			WorkerPoolID:    item.WorkerPoolID,
			Name:            item.Name,
			Status:          item.Status,
			Mode:            item.Mode,
			LastHeartbeatAt: item.LastHeartbeatAt,
			CreatedAt:       item.CreatedAt,
			RunningJobs:     item.RunningJobs,
		})
	}
	return response
}

func toWorkerPoolResponse(pool db.WorkerPool) workerPoolResponse {
	queueNames := pool.QueueNames
	if queueNames == nil {
		queueNames = []string{}
	}
	allowedCIDRs := pool.AllowedCidrs
	if allowedCIDRs == nil {
		allowedCIDRs = []string{}
	}

	return workerPoolResponse{
		ID:                    pool.ID,
		ProjectID:             pool.ProjectID,
		Name:                  pool.Name,
		Concurrency:           pool.Concurrency,
		Status:                pool.Status,
		QueueNames:            queueNames,
		JobTimeoutSeconds:     pool.JobTimeoutSeconds,
		MaxAttempts:           pool.MaxAttempts,
		RetryBackoff:          pool.RetryBackoff,
		RetryBaseDelaySeconds: pool.RetryBaseDelaySeconds,
		RetryMaxDelaySeconds:  pool.RetryMaxDelaySeconds,
		AllowedCIDRs:          allowedCIDRs,
		CreatedAt:             pool.CreatedAt,
		UpdatedAt:             pool.UpdatedAt,
	}
}

func toEnrollmentTokenResponse(token db.WorkerToken) workerTokenResponse {
	return toWorkerTokenResponse(token)
}

func writeWorkerPoolError(w http.ResponseWriter, operation string, err error) {
	switch {
	case errors.Is(err, workerpool.ErrInvalidName):
		httpx.WriteError(w, http.StatusBadRequest, "worker pool name must be between 1 and 100 characters")
	case errors.Is(err, workerpool.ErrInvalidConcurrency):
		httpx.WriteError(w, http.StatusBadRequest, "worker pool concurrency must be between 1 and 1000")
	case errors.Is(err, workerpool.ErrInvalidQueueNames):
		httpx.WriteError(w, http.StatusBadRequest, "worker pool queues must be 1-100 characters each and contain at most 50 queues")
	case errors.Is(err, workerpool.ErrInvalidJobTimeout):
		httpx.WriteError(w, http.StatusBadRequest, "worker pool job timeout must be between 0 and 86400 seconds")
	case errors.Is(err, workerpool.ErrInvalidMaxAttempts):
		httpx.WriteError(w, http.StatusBadRequest, "worker pool max attempts must be between 0 and 100")
	case errors.Is(err, workerpool.ErrInvalidRetryBackoff):
		httpx.WriteError(w, http.StatusBadRequest, "worker pool retry backoff must be fixed or exponential")
	case errors.Is(err, workerpool.ErrInvalidRetryDelay):
		httpx.WriteError(w, http.StatusBadRequest, "worker pool retry delays must be between 0 and 86400 seconds, with max >= base")
	case errors.Is(err, workerpool.ErrInvalidAllowedCIDRs):
		httpx.WriteError(w, http.StatusBadRequest, "worker pool allowed CIDRs must be valid IPv4/IPv6 CIDRs and contain at most 50 entries")
	case errors.Is(err, workerpool.ErrInvalidStatus):
		httpx.WriteError(w, http.StatusBadRequest, "worker pool status must be active or paused")
	case errors.Is(err, workerpool.ErrEmptyUpdate):
		httpx.WriteError(w, http.StatusBadRequest, "update must contain at least one field")
	case errors.Is(err, workerpool.ErrNameTaken):
		httpx.WriteError(w, http.StatusConflict, "a worker pool with that name already exists")
	case errors.Is(err, workerpool.ErrPoolLimit):
		httpx.WriteError(w, http.StatusForbidden, "worker pool limit reached for this project")
	case errors.Is(err, workerpool.ErrActivePoolLimit):
		httpx.WriteError(w, http.StatusForbidden, "active worker pool limit reached for this project")
	case errors.Is(err, workerpool.ErrConcurrencyLimit):
		httpx.WriteError(w, http.StatusForbidden, "worker concurrency limit reached for this project")
	case errors.Is(err, project.ErrProjectNotFound):
		httpx.WriteError(w, http.StatusNotFound, "project not found")
	case errors.Is(err, workerpool.ErrPoolNotFound):
		httpx.WriteError(w, http.StatusNotFound, "worker pool not found")
	case errors.Is(err, workertoken.ErrInvalidName):
		httpx.WriteError(w, http.StatusBadRequest, "enrollment token name must be between 1 and 100 characters")
	case errors.Is(err, workertoken.ErrInvalidTTL):
		httpx.WriteError(w, http.StatusBadRequest, "enrollment token lifetime must be between 1 minute and 24 hours")
	default:
		slog.Error(operation, "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
	}
}
