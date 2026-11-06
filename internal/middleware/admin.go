package middleware

import (
	"net/http"

	"relayforge/internal/http/httpx"
)

// RequireAdmin rejects authenticated users who are not administrators. It must
// run inside RequireAuth so the session is already present in the context.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := SessionFromContext(r.Context())
		if !ok {
			httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if session.User.Role != "admin" {
			httpx.WriteError(w, http.StatusForbidden, "administrator access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
