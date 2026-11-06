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
	"relayforge/internal/workertoken"
)

type WorkerTokenHandler struct {
	service       *workertoken.Service
	rotationGrace time.Duration
}

type workerTokenRequest struct {
	Name          string `json:"name"`
	ExpiresInDays *int32 `json:"expires_in_days"`
}

type workerTokenResponse struct {
	ID           uuid.UUID  `json:"id"`
	ProjectID    uuid.UUID  `json:"project_id"`
	Name         string     `json:"name"`
	TokenPrefix  string     `json:"token_prefix"`
	RevokedAt    *time.Time `json:"revoked_at"`
	ExpiresAt    *time.Time `json:"expires_at"`
	LastUsedAt   *time.Time `json:"last_used_at"`
	RotatedAt    *time.Time `json:"rotated_at"`
	ReplacedByID *uuid.UUID `json:"replaced_by_id"`
	CreatedAt    time.Time  `json:"created_at"`
}

func NewWorkerTokenHandler(service *workertoken.Service, rotationGrace time.Duration) *WorkerTokenHandler {
	if rotationGrace <= 0 {
		rotationGrace = 24 * time.Hour
	}
	return &WorkerTokenHandler{service: service, rotationGrace: rotationGrace}
}

func (h *WorkerTokenHandler) Create(w http.ResponseWriter, r *http.Request) {
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

	var request workerTokenRequest
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	created, err := h.service.Create(r.Context(), session.User.ID, projectID, request.Name, request.ExpiresInDays)
	if err != nil {
		writeWorkerTokenError(w, "create worker token", err)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"worker_token": toWorkerTokenResponse(created.Token),
		"token":        created.RawToken,
	})
}

func (h *WorkerTokenHandler) List(w http.ResponseWriter, r *http.Request) {
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

	tokens, err := h.service.List(r.Context(), session.User.ID, projectID)
	if err != nil {
		writeWorkerTokenError(w, "list worker tokens", err)
		return
	}

	response := make([]workerTokenResponse, 0, len(tokens))
	for _, token := range tokens {
		response = append(response, toWorkerTokenResponse(token))
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"worker_tokens": response,
	})
}

func (h *WorkerTokenHandler) Rotate(w http.ResponseWriter, r *http.Request) {
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
	tokenID, err := uuid.Parse(r.PathValue("tokenID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid worker token id")
		return
	}

	created, err := h.service.Rotate(r.Context(), session.User.ID, projectID, tokenID, h.rotationGrace)
	if err != nil {
		writeWorkerTokenError(w, "rotate worker token", err)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"worker_token":  toWorkerTokenResponse(created.Token),
		"token":         created.RawToken,
		"grace_seconds": int64(h.rotationGrace / time.Second),
	})
}

func (h *WorkerTokenHandler) Revoke(w http.ResponseWriter, r *http.Request) {
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

	tokenID, err := uuid.Parse(r.PathValue("tokenID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid worker token id")
		return
	}

	if err := h.service.Revoke(r.Context(), session.User.ID, projectID, tokenID); err != nil {
		writeWorkerTokenError(w, "revoke worker token", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func toWorkerTokenResponse(token db.WorkerToken) workerTokenResponse {
	return workerTokenResponse{
		ID:           token.ID,
		ProjectID:    token.ProjectID,
		Name:         token.Name,
		TokenPrefix:  token.TokenPrefix,
		RevokedAt:    token.RevokedAt,
		ExpiresAt:    token.ExpiresAt,
		LastUsedAt:   token.LastUsedAt,
		RotatedAt:    token.RotatedAt,
		ReplacedByID: token.ReplacedByID,
		CreatedAt:    token.CreatedAt,
	}
}

func writeWorkerTokenError(w http.ResponseWriter, operation string, err error) {
	switch {
	case errors.Is(err, workertoken.ErrInvalidName):
		httpx.WriteError(w, http.StatusBadRequest, "worker token name must be between 1 and 100 characters")
	case errors.Is(err, workertoken.ErrInvalidExpiry):
		httpx.WriteError(w, http.StatusBadRequest, "expires_in_days must be between 1 and 3650")
	case errors.Is(err, project.ErrProjectNotFound):
		httpx.WriteError(w, http.StatusNotFound, "project not found")
	case errors.Is(err, workertoken.ErrInvalidGrace):
		httpx.WriteError(w, http.StatusBadRequest, "token rotation grace must be between 1 hour and 30 days")
	case errors.Is(err, workertoken.ErrTokenNotFound):
		httpx.WriteError(w, http.StatusNotFound, "worker token not found")
	default:
		slog.Error(operation, "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
	}
}
