package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"relayforge/internal/apikey"
	"relayforge/internal/database/db"
	"relayforge/internal/http/httpx"
)

type APIKeyAuthenticator interface {
	Authenticate(ctx context.Context, rawKey string) (db.ApiKey, error)
}

type apiKeyContextKey struct{}

func RequireAPIKey(authenticator APIKeyAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawKey, err := httpx.BearerToken(r)
			if err != nil {
				httpx.WriteError(w, http.StatusUnauthorized, "API key required")
				return
			}

			key, err := authenticator.Authenticate(r.Context(), rawKey)
			if err != nil {
				if errors.Is(err, apikey.ErrInvalidKey) {
					httpx.WriteError(w, http.StatusUnauthorized, "invalid API key")
					return
				}
				slog.Error("authenticate API key", "error", err)
				httpx.WriteError(w, http.StatusInternalServerError, "internal server error")
				return
			}

			ctx := context.WithValue(r.Context(), apiKeyContextKey{}, key)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func APIKeyFromContext(ctx context.Context) (db.ApiKey, bool) {
	key, ok := ctx.Value(apiKeyContextKey{}).(db.ApiKey)
	return key, ok
}
