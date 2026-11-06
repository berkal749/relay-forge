package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"relayforge/internal/database/db"
	"relayforge/internal/http/httpx"
	"relayforge/internal/middleware"
	"relayforge/internal/schedule"
)

type ScheduleHandler struct {
	service *schedule.Service
}

type scheduleRequest struct {
	Name        *string         `json:"name"`
	Cron        *string         `json:"cron"`
	Timezone    *string         `json:"timezone"`
	Enabled     *bool           `json:"enabled"`
	JobType     *string         `json:"job_type"`
	QueueName   *string         `json:"queue_name"`
	Payload     json.RawMessage `json:"payload"`
	Priority    *int32          `json:"priority"`
	MaxAttempts *int32          `json:"max_attempts"`
}

type scheduleResponse struct {
	ID          uuid.UUID       `json:"id"`
	ProjectID   uuid.UUID       `json:"project_id"`
	Name        string          `json:"name"`
	Cron        string          `json:"cron"`
	Timezone    string          `json:"timezone"`
	Enabled     bool            `json:"enabled"`
	JobType     string          `json:"job_type"`
	QueueName   string          `json:"queue_name"`
	Payload     json.RawMessage `json:"payload"`
	Priority    int32           `json:"priority"`
	MaxAttempts int32           `json:"max_attempts"`
	NextRunAt   time.Time       `json:"next_run_at"`
	LastRunAt   *time.Time      `json:"last_run_at"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

func NewScheduleHandler(service *schedule.Service) *ScheduleHandler {
	return &ScheduleHandler{service: service}
}

func (h *ScheduleHandler) Create(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	projectID, ok := parseProjectID(w, r)
	if !ok {
		return
	}

	var request scheduleRequest
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	input := schedule.CreateInput{
		Timezone: derefString(request.Timezone),
		JobType:  derefString(request.JobType),
		Payload:  request.Payload,
		Enabled:  request.Enabled == nil || *request.Enabled,
	}
	if request.Name != nil {
		input.Name = *request.Name
	}
	if request.Cron != nil {
		input.Cron = *request.Cron
	}
	if request.QueueName != nil {
		input.QueueName = *request.QueueName
	}
	if request.Priority != nil {
		input.Priority = *request.Priority
	}
	if request.MaxAttempts != nil {
		input.MaxAttempts = *request.MaxAttempts
	}

	created, err := h.service.Create(r.Context(), session.User.ID, projectID, input)
	if err != nil {
		writeScheduleError(w, "create schedule", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"schedule": toScheduleResponse(created)})
}

func (h *ScheduleHandler) List(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	projectID, ok := parseProjectID(w, r)
	if !ok {
		return
	}

	schedules, err := h.service.List(r.Context(), session.User.ID, projectID)
	if err != nil {
		writeScheduleError(w, "list schedules", err)
		return
	}
	response := make([]scheduleResponse, 0, len(schedules))
	for _, item := range schedules {
		response = append(response, toScheduleResponse(item))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"schedules": response})
}

func (h *ScheduleHandler) Update(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	projectID, ok := parseProjectID(w, r)
	if !ok {
		return
	}
	scheduleID, ok := parseScheduleID(w, r)
	if !ok {
		return
	}

	var request scheduleRequest
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	updated, err := h.service.Update(r.Context(), session.User.ID, projectID, scheduleID, schedule.UpdateInput{
		Name:        request.Name,
		Cron:        request.Cron,
		Timezone:    request.Timezone,
		Enabled:     request.Enabled,
		JobType:     request.JobType,
		QueueName:   request.QueueName,
		Payload:     request.Payload,
		Priority:    request.Priority,
		MaxAttempts: request.MaxAttempts,
	})
	if err != nil {
		writeScheduleError(w, "update schedule", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"schedule": toScheduleResponse(updated)})
}

func (h *ScheduleHandler) Delete(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	projectID, ok := parseProjectID(w, r)
	if !ok {
		return
	}
	scheduleID, ok := parseScheduleID(w, r)
	if !ok {
		return
	}

	if err := h.service.Delete(r.Context(), session.User.ID, projectID, scheduleID); err != nil {
		writeScheduleError(w, "delete schedule", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseProjectID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	projectID, err := uuid.Parse(r.PathValue("projectID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid project id")
		return uuid.Nil, false
	}
	return projectID, true
}

func parseScheduleID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	scheduleID, err := uuid.Parse(r.PathValue("scheduleID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid schedule id")
		return uuid.Nil, false
	}
	return scheduleID, true
}

func toScheduleResponse(item db.Schedule) scheduleResponse {
	payload := item.Payload
	if len(payload) == 0 {
		payload = []byte(`{}`)
	}
	return scheduleResponse{
		ID:          item.ID,
		ProjectID:   item.ProjectID,
		Name:        item.Name,
		Cron:        item.Cron,
		Timezone:    item.Timezone,
		Enabled:     item.Enabled,
		JobType:     item.JobType,
		QueueName:   item.QueueName,
		Payload:     payload,
		Priority:    item.Priority,
		MaxAttempts: item.MaxAttempts,
		NextRunAt:   item.NextRunAt,
		LastRunAt:   item.LastRunAt,
		CreatedAt:   item.CreatedAt,
		UpdatedAt:   item.UpdatedAt,
	}
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func writeScheduleError(w http.ResponseWriter, operation string, err error) {
	switch {
	case errors.Is(err, schedule.ErrInvalidName):
		httpx.WriteError(w, http.StatusBadRequest, "schedule name must be between 1 and 100 characters")
	case errors.Is(err, schedule.ErrInvalidCron):
		httpx.WriteError(w, http.StatusBadRequest, "cron expression is invalid")
	case errors.Is(err, schedule.ErrInvalidTimezone):
		httpx.WriteError(w, http.StatusBadRequest, "timezone is invalid")
	case errors.Is(err, schedule.ErrInvalidJobType):
		httpx.WriteError(w, http.StatusBadRequest, "schedule job type must be between 1 and 100 characters")
	case errors.Is(err, schedule.ErrInvalidQueueName):
		httpx.WriteError(w, http.StatusBadRequest, "schedule queue name must be at most 100 characters")
	case errors.Is(err, schedule.ErrInvalidPayload):
		httpx.WriteError(w, http.StatusBadRequest, "schedule payload must be a JSON object")
	case errors.Is(err, schedule.ErrPayloadTooLarge):
		httpx.WriteError(w, http.StatusBadRequest, "schedule payload must be at most 256 KiB")
	case errors.Is(err, schedule.ErrInvalidPriority):
		httpx.WriteError(w, http.StatusBadRequest, "schedule priority must be between 0 and 1000")
	case errors.Is(err, schedule.ErrInvalidAttempts):
		httpx.WriteError(w, http.StatusBadRequest, "schedule max attempts must be between 0 and 100")
	case errors.Is(err, schedule.ErrScheduleNotFound):
		httpx.WriteError(w, http.StatusNotFound, "schedule not found")
	case errors.Is(err, schedule.ErrNameTaken):
		httpx.WriteError(w, http.StatusConflict, "a schedule with that name already exists")
	case errors.Is(err, schedule.ErrScheduleLimit):
		httpx.WriteError(w, http.StatusForbidden, "schedule limit reached for this project")
	case errors.Is(err, schedule.ErrEmptyUpdate):
		httpx.WriteError(w, http.StatusBadRequest, "update must contain at least one field")
	default:
		writeProjectError(w, operation, err)
	}
}
