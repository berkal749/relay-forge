package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"relayforge/internal/auth"
	"relayforge/internal/database/db"
	"relayforge/internal/http/httpx"
	"relayforge/internal/middleware"
)

type AuthHandler struct {
	service *auth.Service
}

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	InviteToken string `json:"invite_token"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userResponse struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type loginResponse struct {
	Token     string       `json:"token"`
	TokenType string       `json:"token_type"`
	ExpiresAt time.Time    `json:"expires_at"`
	User      userResponse `json:"user"`
}

func NewAuthHandler(service *auth.Service) *AuthHandler {
	return &AuthHandler{service: service}
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var request registerRequest
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := h.service.Register(r.Context(), request.Email, request.Password, request.InviteToken)
	if err != nil {
		writeAuthError(w, "register user", err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"user": toUserResponse(user),
	})
}

// Config exposes non-sensitive registration settings so the dashboard can show
// the invite field and disable the form when registration is closed.
func (h *AuthHandler) Config(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{
		"registration_mode": h.service.RegistrationMode(),
	})
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var request loginRequest
	if err := httpx.DecodeJSON(w, r, &request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	session, err := h.service.Login(r.Context(), request.Email, request.Password)
	if err != nil {
		writeAuthError(w, "login user", err)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, loginResponse{
		Token:     session.Token,
		TokenType: "Bearer",
		ExpiresAt: session.ExpiresAt,
		User:      toUserResponse(session.User),
	})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"user": toUserResponse(session.User),
	})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	if err := h.service.Logout(r.Context(), session.Token); err != nil {
		slog.Error("logout user", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func toUserResponse(user db.User) userResponse {
	return userResponse{
		ID:        user.ID.String(),
		Email:     user.Email,
		Role:      user.Role,
		CreatedAt: user.CreatedAt,
	}
}

func writeAuthError(w http.ResponseWriter, operation string, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidEmail):
		httpx.WriteError(w, http.StatusBadRequest, "invalid email address")
	case errors.Is(err, auth.ErrWeakPassword):
		httpx.WriteError(w, http.StatusBadRequest, "password must be between 8 and 72 characters")
	case errors.Is(err, auth.ErrEmailTaken):
		httpx.WriteError(w, http.StatusConflict, "email is already registered")
	case errors.Is(err, auth.ErrInvalidCredentials):
		httpx.WriteError(w, http.StatusUnauthorized, "invalid email or password")
	case errors.Is(err, auth.ErrInvalidSession):
		httpx.WriteError(w, http.StatusUnauthorized, "invalid session")
	case errors.Is(err, auth.ErrSessionExpired):
		httpx.WriteError(w, http.StatusUnauthorized, "session expired")
	case errors.Is(err, auth.ErrRegistrationClosed), errors.Is(err, auth.ErrRegistrationDisabled):
		httpx.WriteError(w, http.StatusForbidden, "registration is closed")
	case errors.Is(err, auth.ErrInviteRequired):
		httpx.WriteError(w, http.StatusForbidden, "an invite token is required to register")
	case errors.Is(err, auth.ErrInviteInvalid), errors.Is(err, auth.ErrInviteExpired):
		httpx.WriteError(w, http.StatusForbidden, "invite token is invalid or expired")
	case errors.Is(err, auth.ErrInviteEmailMismatch):
		httpx.WriteError(w, http.StatusForbidden, "invite token was issued for a different email")
	default:
		slog.Error(operation, "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
	}
}
