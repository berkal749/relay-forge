package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"relayforge/internal/database/db"
	"relayforge/internal/http/httpx"
	"relayforge/internal/middleware"
	"relayforge/internal/notificationchannel"
)

type NotificationChannelHandler struct {
	service *notificationchannel.Service
}

type notificationChannelRequest struct {
	Name   string            `json:"name"`
	Kind   string            `json:"kind"`
	Config map[string]string `json:"config"`
}

type notificationChannelResponse struct {
	ID        uuid.UUID `json:"id"`
	ProjectID uuid.UUID `json:"project_id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func NewNotificationChannelHandler(service *notificationchannel.Service) *NotificationChannelHandler {
	return &NotificationChannelHandler{service: service}
}

func (h *NotificationChannelHandler) Create(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	projectID, ok := parseProjectID(w, r)
	if !ok {
		return
	}

	var request notificationChannelRequest
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	created, err := h.service.Create(r.Context(), session.User.ID, projectID, notificationchannel.CreateInput{
		Name:   request.Name,
		Kind:   request.Kind,
		Config: request.Config,
	})
	if err != nil {
		writeNotificationChannelError(w, "create notification channel", err)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"notification_channel": toNotificationChannelResponse(created),
	})
}

func (h *NotificationChannelHandler) List(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	projectID, ok := parseProjectID(w, r)
	if !ok {
		return
	}

	channels, err := h.service.List(r.Context(), session.User.ID, projectID)
	if err != nil {
		writeNotificationChannelError(w, "list notification channels", err)
		return
	}
	response := make([]notificationChannelResponse, 0, len(channels))
	for _, channel := range channels {
		response = append(response, toNotificationChannelResponse(channel))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"notification_channels": response})
}

func (h *NotificationChannelHandler) Delete(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	projectID, ok := parseProjectID(w, r)
	if !ok {
		return
	}
	channelID, err := uuid.Parse(r.PathValue("channelID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid channel id")
		return
	}

	if err := h.service.Delete(r.Context(), session.User.ID, projectID, channelID); err != nil {
		writeNotificationChannelError(w, "delete notification channel", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toNotificationChannelResponse(channel db.NotificationChannel) notificationChannelResponse {
	return notificationChannelResponse{
		ID:        channel.ID,
		ProjectID: channel.ProjectID,
		Name:      channel.Name,
		Kind:      channel.Kind,
		CreatedAt: channel.CreatedAt,
		UpdatedAt: channel.UpdatedAt,
	}
}

func writeNotificationChannelError(w http.ResponseWriter, operation string, err error) {
	switch {
	case errors.Is(err, notificationchannel.ErrInvalidName):
		httpx.WriteError(w, http.StatusBadRequest, "channel name must be between 1 and 100 characters")
	case errors.Is(err, notificationchannel.ErrInvalidKind):
		httpx.WriteError(w, http.StatusBadRequest, "channel kind must be slack, discord, telegram, sms, or email")
	case errors.Is(err, notificationchannel.ErrInvalidConfig):
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, notificationchannel.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "notification channel not found")
	case errors.Is(err, notificationchannel.ErrNameTaken):
		httpx.WriteError(w, http.StatusConflict, "a notification channel with that name already exists")
	case errors.Is(err, notificationchannel.ErrLimit):
		httpx.WriteError(w, http.StatusForbidden, "notification channel limit reached for this project")
	default:
		writeProjectError(w, operation, err)
	}
}
