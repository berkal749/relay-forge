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
)

type ProjectHandler struct {
	service *project.Service
}

type projectRequest struct {
	Name                     *string `json:"name"`
	DefaultQueueName         *string `json:"default_queue_name"`
	DefaultMaxAttempts       *int32  `json:"default_max_attempts"`
	DefaultPriority          *int32  `json:"default_priority"`
	JobRetentionDays         *int32  `json:"job_retention_days"`
	MaxConcurrency           *int32  `json:"max_concurrency"`
	RateLimitPerMinute       *int32  `json:"rate_limit_per_minute"`
	DefaultJobTimeoutSeconds *int32  `json:"default_job_timeout_seconds"`
}

func (r projectRequest) updateInput() project.UpdateInput {
	return project.UpdateInput{
		Name:                     r.Name,
		DefaultQueueName:         r.DefaultQueueName,
		DefaultMaxAttempts:       r.DefaultMaxAttempts,
		DefaultPriority:          r.DefaultPriority,
		JobRetentionDays:         r.JobRetentionDays,
		MaxConcurrency:           r.MaxConcurrency,
		RateLimitPerMinute:       r.RateLimitPerMinute,
		DefaultJobTimeoutSeconds: r.DefaultJobTimeoutSeconds,
	}
}

type projectResponse struct {
	ID                       uuid.UUID `json:"id"`
	UserID                   uuid.UUID `json:"user_id"`
	Name                     string    `json:"name"`
	CreatedAt                time.Time `json:"created_at"`
	UpdatedAt                time.Time `json:"updated_at"`
	DefaultQueueName         string    `json:"default_queue_name"`
	DefaultMaxAttempts       int32     `json:"default_max_attempts"`
	DefaultPriority          int32     `json:"default_priority"`
	JobRetentionDays         int32     `json:"job_retention_days"`
	MaxConcurrency           int32     `json:"max_concurrency"`
	RateLimitPerMinute       int32     `json:"rate_limit_per_minute"`
	DefaultJobTimeoutSeconds int32     `json:"default_job_timeout_seconds"`
}

func NewProjectHandler(service *project.Service) *ProjectHandler {
	return &ProjectHandler{service: service}
}

func (h *ProjectHandler) Create(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var request projectRequest
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if request.Name == nil {
		httpx.WriteError(w, http.StatusBadRequest, "project name must be between 1 and 100 characters")
		return
	}

	created, err := h.service.Create(r.Context(), session.User.ID, *request.Name)
	if err != nil {
		writeProjectError(w, "create project", err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"project": toProjectResponse(created),
	})
}

func (h *ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	projects, err := h.service.List(r.Context(), session.User.ID)
	if err != nil {
		writeProjectError(w, "list projects", err)
		return
	}

	response := make([]projectResponse, 0, len(projects))
	for _, item := range projects {
		response = append(response, toProjectResponse(item))
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"projects": response,
	})
}

func (h *ProjectHandler) Get(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	projectID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid project id")
		return
	}

	found, err := h.service.Get(r.Context(), session.User.ID, projectID)
	if err != nil {
		writeProjectError(w, "get project", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"project": toProjectResponse(found),
	})
}

func (h *ProjectHandler) Update(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	projectID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid project id")
		return
	}

	var request projectRequest
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	updated, err := h.service.Update(r.Context(), session.User.ID, projectID, request.updateInput())
	if err != nil {
		writeProjectError(w, "update project", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"project": toProjectResponse(updated),
	})
}

func (h *ProjectHandler) Delete(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	projectID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid project id")
		return
	}

	if err := h.service.Delete(r.Context(), session.User.ID, projectID); err != nil {
		writeProjectError(w, "archive project", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func toProjectResponse(item db.Project) projectResponse {
	return projectResponse{
		ID:                       item.ID,
		UserID:                   item.UserID,
		Name:                     item.Name,
		CreatedAt:                item.CreatedAt,
		UpdatedAt:                item.UpdatedAt,
		DefaultQueueName:         item.DefaultQueueName,
		DefaultMaxAttempts:       item.DefaultMaxAttempts,
		DefaultPriority:          item.DefaultPriority,
		JobRetentionDays:         item.JobRetentionDays,
		MaxConcurrency:           item.MaxConcurrency,
		RateLimitPerMinute:       item.RateLimitPerMinute,
		DefaultJobTimeoutSeconds: item.DefaultJobTimeoutSeconds,
	}
}

func writeProjectError(w http.ResponseWriter, operation string, err error) {
	switch {
	case errors.Is(err, project.ErrInvalidName):
		httpx.WriteError(w, http.StatusBadRequest, "project name must be between 1 and 100 characters")
	case errors.Is(err, project.ErrEmptyUpdate):
		httpx.WriteError(w, http.StatusBadRequest, "update must contain at least one field")
	case errors.Is(err, project.ErrInvalidDefaultQueueName):
		httpx.WriteError(w, http.StatusBadRequest, "default_queue_name must be between 1 and 100 characters")
	case errors.Is(err, project.ErrInvalidDefaultAttempts):
		httpx.WriteError(w, http.StatusBadRequest, "default_max_attempts must be between 1 and 100")
	case errors.Is(err, project.ErrInvalidRetentionDays):
		httpx.WriteError(w, http.StatusBadRequest, "job_retention_days must be between 0 and 3650")
	case errors.Is(err, project.ErrInvalidMaxConcurrency):
		httpx.WriteError(w, http.StatusBadRequest, "max_concurrency must be between 0 and 1000")
	case errors.Is(err, project.ErrInvalidRateLimit):
		httpx.WriteError(w, http.StatusBadRequest, "rate_limit_per_minute must be between 0 and 1000000")
	case errors.Is(err, project.ErrInvalidJobTimeout):
		httpx.WriteError(w, http.StatusBadRequest, "default_job_timeout_seconds must be between 0 and 86400")
	case errors.Is(err, project.ErrProjectLimit):
		httpx.WriteError(w, http.StatusForbidden, "project limit reached for this account")
	case errors.Is(err, project.ErrProjectNotFound):
		httpx.WriteError(w, http.StatusNotFound, "project not found")
	default:
		slog.Error(operation, "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
	}
}
