package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"relayforge/internal/http/httpx"
	"relayforge/internal/workertoken"
)

type EnrollmentHandler struct {
	service *workertoken.Service
}

func NewEnrollmentHandler(service *workertoken.Service) *EnrollmentHandler {
	return &EnrollmentHandler{service: service}
}

// Enroll exchanges a single-use enrollment token for a pool-scoped worker token.
// Enrollment tokens are presented as the bearer credential.
func (h *EnrollmentHandler) Enroll(w http.ResponseWriter, r *http.Request) {
	rawToken, err := httpx.BearerToken(r)
	if err != nil {
		httpx.WriteError(w, http.StatusUnauthorized, "enrollment token required")
		return
	}

	enrollment, err := h.service.Enroll(r.Context(), rawToken)
	if errors.Is(err, workertoken.ErrInvalidToken) {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid enrollment token")
		return
	}
	if err != nil {
		slog.Error("enroll worker", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"project_id":   enrollment.ProjectID,
		"worker_pool":  toWorkerPoolResponse(enrollment.Pool),
		"worker_token": toWorkerTokenResponse(enrollment.Token.Token),
		"token":        enrollment.Token.RawToken,
	})
}
