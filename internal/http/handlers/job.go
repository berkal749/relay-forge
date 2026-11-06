package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"relayforge/internal/database/db"
	"relayforge/internal/http/httpx"
	"relayforge/internal/job"
	"relayforge/internal/middleware"
	"relayforge/internal/project"
)

type JobHandler struct {
	service  *job.Service
	projects *project.Service
}

type submitJobRequest struct {
	QueueName   string          `json:"queue_name"`
	JobType     string          `json:"job_type"`
	Payload     json.RawMessage `json:"payload"`
	Priority    *int32          `json:"priority"`
	MaxAttempts *int32          `json:"max_attempts"`
	ScheduledAt *time.Time      `json:"scheduled_at"`
}

type jobResponse struct {
	ID          uuid.UUID       `json:"id"`
	ProjectID   uuid.UUID       `json:"project_id"`
	QueueName   string          `json:"queue_name"`
	JobType     string          `json:"job_type"`
	Payload     json.RawMessage `json:"payload"`
	Status      string          `json:"status"`
	Priority    int32           `json:"priority"`
	Attempts    int32           `json:"attempts"`
	MaxAttempts int32           `json:"max_attempts"`
	ScheduledAt time.Time       `json:"scheduled_at"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type executionResponse struct {
	ID            uuid.UUID       `json:"id"`
	JobID         uuid.UUID       `json:"job_id"`
	WorkerID      *uuid.UUID      `json:"worker_id"`
	AttemptNumber int32           `json:"attempt_number"`
	Status        string          `json:"status"`
	StartedAt     time.Time       `json:"started_at"`
	FinishedAt    *time.Time      `json:"finished_at"`
	ErrorMessage  *string         `json:"error_message"`
	Result        json.RawMessage `json:"result"`
}

func NewJobHandler(service *job.Service, projects *project.Service) *JobHandler {
	return &JobHandler{service: service, projects: projects}
}

func (h *JobHandler) Submit(w http.ResponseWriter, r *http.Request) {
	apiKey, ok := middleware.APIKeyFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "API key required")
		return
	}
	h.submit(w, r, apiKey.ProjectID)
}

func (h *JobHandler) SubmitForProject(w http.ResponseWriter, r *http.Request) {
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

	if _, err := h.projects.Get(r.Context(), session.User.ID, projectID); err != nil {
		writeProjectError(w, "get project", err)
		return
	}

	h.submit(w, r, projectID)
}

func (h *JobHandler) submit(w http.ResponseWriter, r *http.Request, projectID uuid.UUID) {
	var request submitJobRequest
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var scheduledAt time.Time
	if request.ScheduledAt != nil {
		scheduledAt = *request.ScheduledAt
	}

	result, err := h.service.Submit(r.Context(), job.SubmitInput{
		ProjectID:      projectID,
		QueueName:      request.QueueName,
		JobType:        request.JobType,
		Payload:        request.Payload,
		Priority:       request.Priority,
		MaxAttempts:    request.MaxAttempts,
		ScheduledAt:    scheduledAt,
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		writeJobError(w, "submit job", err)
		return
	}

	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
		w.Header().Set("Idempotent-Replayed", "true")
	}

	httpx.WriteJSON(w, status, map[string]any{
		"job": toJobResponse(result.Job),
	})
}

func (h *JobHandler) Get(w http.ResponseWriter, r *http.Request) {
	apiKey, ok := middleware.APIKeyFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "API key required")
		return
	}

	jobID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid job id")
		return
	}

	h.get(w, r, apiKey.ProjectID, jobID)
}

func (h *JobHandler) GetForProject(w http.ResponseWriter, r *http.Request) {
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
	jobID, err := uuid.Parse(r.PathValue("jobID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid job id")
		return
	}

	if _, err := h.projects.Get(r.Context(), session.User.ID, projectID); err != nil {
		writeProjectError(w, "get project", err)
		return
	}

	h.get(w, r, projectID, jobID)
}

func (h *JobHandler) get(w http.ResponseWriter, r *http.Request, projectID, jobID uuid.UUID) {
	found, err := h.service.Get(r.Context(), projectID, jobID)
	if err != nil {
		writeJobError(w, "get job", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"job": toJobResponse(found),
	})
}

func (h *JobHandler) ListForProject(w http.ResponseWriter, r *http.Request) {
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

	if _, err := h.projects.Get(r.Context(), session.User.ID, projectID); err != nil {
		writeProjectError(w, "get project", err)
		return
	}

	limit := int32(50)
	offset := int32(0)
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 32)
		if err != nil || parsed < 1 {
			httpx.WriteError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = int32(parsed)
	}
	if value := r.URL.Query().Get("offset"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 32)
		if err != nil || parsed < 0 {
			httpx.WriteError(w, http.StatusBadRequest, "invalid offset")
			return
		}
		offset = int32(parsed)
	}

	jobs, err := h.service.List(r.Context(), projectID, limit, offset)
	if err != nil {
		writeJobError(w, "list jobs", err)
		return
	}

	response := make([]jobResponse, 0, len(jobs))
	for _, item := range jobs {
		response = append(response, toJobResponse(item))
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"jobs": response,
	})
}

func (h *JobHandler) ExecutionsForProject(w http.ResponseWriter, r *http.Request) {
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
	jobID, err := uuid.Parse(r.PathValue("jobID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid job id")
		return
	}

	if _, err := h.projects.Get(r.Context(), session.User.ID, projectID); err != nil {
		writeProjectError(w, "get project", err)
		return
	}

	executions, err := h.service.Executions(r.Context(), projectID, jobID)
	if err != nil {
		writeJobError(w, "list executions", err)
		return
	}

	response := make([]executionResponse, 0, len(executions))
	for _, item := range executions {
		response = append(response, toExecutionResponse(item))
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"executions": response,
	})
}

func toJobResponse(item db.Job) jobResponse {
	payload := item.Payload
	if len(payload) == 0 {
		payload = []byte(`{}`)
	}

	return jobResponse{
		ID:          item.ID,
		ProjectID:   item.ProjectID,
		QueueName:   item.QueueName,
		JobType:     item.JobType,
		Payload:     payload,
		Status:      item.Status,
		Priority:    item.Priority,
		Attempts:    item.Attempts,
		MaxAttempts: item.MaxAttempts,
		ScheduledAt: item.ScheduledAt,
		CreatedAt:   item.CreatedAt,
		UpdatedAt:   item.UpdatedAt,
	}
}

func toExecutionResponse(item db.Execution) executionResponse {
	return executionResponse{
		ID:            item.ID,
		JobID:         item.JobID,
		WorkerID:      item.WorkerID,
		AttemptNumber: item.AttemptNumber,
		Status:        item.Status,
		StartedAt:     item.StartedAt,
		FinishedAt:    item.FinishedAt,
		ErrorMessage:  item.ErrorMessage,
		Result:        item.Result,
	}
}

func writeJobError(w http.ResponseWriter, operation string, err error) {
	switch {
	case errors.Is(err, job.ErrInvalidQueueName):
		httpx.WriteError(w, http.StatusBadRequest, "queue_name must be between 1 and 100 characters")
	case errors.Is(err, job.ErrInvalidJobType):
		httpx.WriteError(w, http.StatusBadRequest, "job_type must be between 1 and 100 characters")
	case errors.Is(err, job.ErrInvalidPayload):
		httpx.WriteError(w, http.StatusBadRequest, "payload must be a non-null JSON value")
	case errors.Is(err, job.ErrPayloadTooLarge):
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "payload must be at most 256 KiB")
	case errors.Is(err, job.ErrInvalidMaxAttempts):
		httpx.WriteError(w, http.StatusBadRequest, "max_attempts must be between 1 and 100")
	case errors.Is(err, job.ErrJobNotFound):
		httpx.WriteError(w, http.StatusNotFound, "job not found")
	case errors.Is(err, job.ErrProjectNotFound):
		httpx.WriteError(w, http.StatusNotFound, "project not found")
	case errors.Is(err, job.ErrInvalidIdempotencyKey):
		httpx.WriteError(w, http.StatusBadRequest, "Idempotency-Key must be between 1 and 255 printable characters")
	case errors.Is(err, job.ErrIdempotencyConflict):
		httpx.WriteError(w, http.StatusConflict, "idempotency key was already used with a different request")
	default:
		slog.Error(operation, "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
	}
}
