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
	Name        string `json:"name"`
	Concurrency *int32 `json:"concurrency"`
}

type workerPoolUpdateRequest struct {
	Name        *string `json:"name"`
	Concurrency *int32  `json:"concurrency"`
	Status      *string `json:"status"`
}

type enrollmentTokenRequest struct {
	Name             string `json:"name"`
	ExpiresInMinutes *int32 `json:"expires_in_minutes"`
}

type workerPoolResponse struct {
	ID          uuid.UUID `json:"id"`
	ProjectID   uuid.UUID `json:"project_id"`
	Name        string    `json:"name"`
	Concurrency int32     `json:"concurrency"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
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

	pool, err := h.service.Create(r.Context(), session.User.ID, projectID, request.Name, concurrency)
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
		Name:        request.Name,
		Concurrency: request.Concurrency,
		Status:      request.Status,
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

func toWorkerPoolResponse(pool db.WorkerPool) workerPoolResponse {
	return workerPoolResponse{
		ID:          pool.ID,
		ProjectID:   pool.ProjectID,
		Name:        pool.Name,
		Concurrency: pool.Concurrency,
		Status:      pool.Status,
		CreatedAt:   pool.CreatedAt,
		UpdatedAt:   pool.UpdatedAt,
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
	case errors.Is(err, workerpool.ErrInvalidStatus):
		httpx.WriteError(w, http.StatusBadRequest, "worker pool status must be active or paused")
	case errors.Is(err, workerpool.ErrEmptyUpdate):
		httpx.WriteError(w, http.StatusBadRequest, "update must contain at least one field")
	case errors.Is(err, workerpool.ErrNameTaken):
		httpx.WriteError(w, http.StatusConflict, "a worker pool with that name already exists")
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
