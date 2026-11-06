package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"relayforge/internal/auth"
	"relayforge/internal/database/db"
	"relayforge/internal/http/httpx"
)

type Authenticator interface {
	Authenticate(ctx context.Context, token string) (db.User, error)
}

type Session struct {
	User  db.User
	Token string
}

type sessionContextKey struct{}

func RequireAuth(authenticator Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, err := httpx.BearerToken(r)
			if err != nil {
				httpx.WriteError(w, http.StatusUnauthorized, "authentication required")
				return
			}

			user, err := authenticator.Authenticate(r.Context(), token)
			if err != nil {
				switch {
				case errors.Is(err, auth.ErrInvalidSession), errors.Is(err, auth.ErrSessionExpired):
					httpx.WriteError(w, http.StatusUnauthorized, "invalid or expired session")
				default:
					slog.Error("authenticate session", "error", err)
					httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
				}
				return
			}

			session := Session{User: user, Token: token}
			ctx := context.WithValue(r.Context(), sessionContextKey{}, session)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func SessionFromContext(ctx context.Context) (Session, bool) {
	session, ok := ctx.Value(sessionContextKey{}).(Session)
	return session, ok
}
