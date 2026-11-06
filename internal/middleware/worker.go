package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"relayforge/internal/database/db"
	"relayforge/internal/http/httpx"
	"relayforge/internal/workertoken"
)

type WorkerTokenAuthenticator interface {
	AuthenticateAny(ctx context.Context, rawToken string) (db.WorkerToken, error)
}

type workerTokenContextKey struct{}

// RequireWorkerToken authenticates an rf_worker_ token. The token resolves its
// own project and pool, so remote workers do not need to send a project ID.
func RequireWorkerToken(authenticator WorkerTokenAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawToken, err := httpx.BearerToken(r)
			if err != nil {
				httpx.WriteError(w, http.StatusUnauthorized, "worker token required")
				return
			}

			token, err := authenticator.AuthenticateAny(r.Context(), rawToken)
			if err != nil {
				if errors.Is(err, workertoken.ErrInvalidToken) {
					httpx.WriteError(w, http.StatusUnauthorized, "invalid worker token")
					return
				}
				slog.Error("authenticate worker token", "error", err)
				httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
				return
			}

			ctx := context.WithValue(r.Context(), workerTokenContextKey{}, token)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func WorkerTokenFromContext(ctx context.Context) (db.WorkerToken, bool) {
	token, ok := ctx.Value(workerTokenContextKey{}).(db.WorkerToken)
	return token, ok
}
