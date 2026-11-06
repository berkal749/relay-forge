package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"relayforge/internal/apikey"
	"relayforge/internal/database/db"
	"relayforge/internal/http/httpx"
	"relayforge/internal/middleware"
	"relayforge/internal/project"
)

type APIKeyHandler struct {
	service *apikey.Service
}

type apiKeyRequest struct {
	Name          string `json:"name"`
	ExpiresInDays *int32 `json:"expires_in_days"`
}

type apiKeyResponse struct {
	ID         uuid.UUID  `json:"id"`
	ProjectID  uuid.UUID  `json:"project_id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"key_prefix"`
	RevokedAt  *time.Time `json:"revoked_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

func NewAPIKeyHandler(service *apikey.Service) *APIKeyHandler {
	return &APIKeyHandler{service: service}
}

func (h *APIKeyHandler) Create(w http.ResponseWriter, r *http.Request) {
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

	var request apiKeyRequest
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	created, err := h.service.Create(r.Context(), session.User.ID, projectID, request.Name, request.ExpiresInDays)
	if err != nil {
		writeAPIKeyError(w, "create API key", err)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"api_key": toAPIKeyResponse(created.Key),
		"key":     created.RawKey,
	})
}

func (h *APIKeyHandler) List(w http.ResponseWriter, r *http.Request) {
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

	keys, err := h.service.List(r.Context(), session.User.ID, projectID)
	if err != nil {
		writeAPIKeyError(w, "list API keys", err)
		return
	}

	response := make([]apiKeyResponse, 0, len(keys))
	for _, key := range keys {
		response = append(response, toAPIKeyResponse(key))
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"api_keys": response,
	})
}

func (h *APIKeyHandler) Revoke(w http.ResponseWriter, r *http.Request) {
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

	keyID, err := uuid.Parse(r.PathValue("keyID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid API key id")
		return
	}

	if err := h.service.Revoke(r.Context(), session.User.ID, projectID, keyID); err != nil {
		writeAPIKeyError(w, "revoke API key", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *APIKeyHandler) Current(w http.ResponseWriter, r *http.Request) {
	key, ok := middleware.APIKeyFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "API key required")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"api_key": toAPIKeyResponse(key),
	})
}

func toAPIKeyResponse(key db.ApiKey) apiKeyResponse {
	return apiKeyResponse{
		ID:         key.ID,
		ProjectID:  key.ProjectID,
		Name:       key.Name,
		KeyPrefix:  key.KeyPrefix,
		RevokedAt:  key.RevokedAt,
		ExpiresAt:  key.ExpiresAt,
		LastUsedAt: key.LastUsedAt,
		CreatedAt:  key.CreatedAt,
	}
}

func writeAPIKeyError(w http.ResponseWriter, operation string, err error) {
	switch {
	case errors.Is(err, apikey.ErrInvalidName):
		httpx.WriteError(w, http.StatusBadRequest, "API key name must be between 1 and 100 characters")
	case errors.Is(err, apikey.ErrInvalidExpiry):
		httpx.WriteError(w, http.StatusBadRequest, "expires_in_days must be between 1 and 3650")
	case errors.Is(err, project.ErrProjectNotFound):
		httpx.WriteError(w, http.StatusNotFound, "project not found")
	case errors.Is(err, apikey.ErrKeyNotFound):
		httpx.WriteError(w, http.StatusNotFound, "API key not found")
	default:
		slog.Error(operation, "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
	}
}
