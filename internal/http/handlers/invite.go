package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"relayforge/internal/database/db"
	"relayforge/internal/http/httpx"
	"relayforge/internal/invite"
	"relayforge/internal/middleware"
)

type InviteHandler struct {
	service *invite.Service
}

type createInviteRequest struct {
	Email          string `json:"email"`
	Role           string `json:"role"`
	ExpiresInHours int32  `json:"expires_in_hours"`
}

type inviteResponse struct {
	ID         uuid.UUID  `json:"id"`
	Email      *string    `json:"email"`
	Role       string     `json:"role"`
	CreatedBy  *uuid.UUID `json:"created_by"`
	ConsumedBy *uuid.UUID `json:"consumed_by"`
	ExpiresAt  time.Time  `json:"expires_at"`
	ConsumedAt *time.Time `json:"consumed_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

type createdInviteResponse struct {
	Invite inviteResponse `json:"invite"`
	Token  string         `json:"token"`
}

func NewInviteHandler(service *invite.Service) *InviteHandler {
	return &InviteHandler{service: service}
}

func (h *InviteHandler) Create(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var request createInviteRequest
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var ttl time.Duration
	if request.ExpiresInHours > 0 {
		ttl = time.Duration(request.ExpiresInHours) * time.Hour
	}

	created, err := h.service.Create(r.Context(), invite.CreateInput{
		Email:     request.Email,
		Role:      request.Role,
		CreatedBy: &session.User.ID,
		TTL:       ttl,
	})
	if err != nil {
		writeInviteError(w, "create invite", err)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusCreated, createdInviteResponse{
		Invite: toInviteResponse(created.Invite),
		Token:  created.Token,
	})
}

func (h *InviteHandler) List(w http.ResponseWriter, r *http.Request) {
	invites, err := h.service.List(r.Context(), 200)
	if err != nil {
		writeInviteError(w, "list invites", err)
		return
	}

	response := make([]inviteResponse, 0, len(invites))
	for _, item := range invites {
		response = append(response, toInviteResponse(item))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"invites": response})
}

func (h *InviteHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid invite id")
		return
	}
	if err := h.service.Revoke(r.Context(), id); err != nil {
		writeInviteError(w, "revoke invite", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toInviteResponse(item db.Invite) inviteResponse {
	return inviteResponse{
		ID:         item.ID,
		Email:      item.Email,
		Role:       item.Role,
		CreatedBy:  item.CreatedBy,
		ConsumedBy: item.ConsumedBy,
		ExpiresAt:  item.ExpiresAt,
		ConsumedAt: item.ConsumedAt,
		CreatedAt:  item.CreatedAt,
	}
}

func writeInviteError(w http.ResponseWriter, operation string, err error) {
	switch {
	case errors.Is(err, invite.ErrInvalidEmail):
		httpx.WriteError(w, http.StatusBadRequest, "invalid invite email")
	case errors.Is(err, invite.ErrInvalidRole):
		httpx.WriteError(w, http.StatusBadRequest, "invite role must be user or admin")
	case errors.Is(err, invite.ErrInvalidTTL):
		httpx.WriteError(w, http.StatusBadRequest, "invite expiry must be between 1 hour and 30 days")
	case errors.Is(err, invite.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "invite not found")
	default:
		slog.Error(operation, "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
	}
}
