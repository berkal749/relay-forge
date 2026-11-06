package routes

import (
	"net/http"

	"relayforge/internal/http/handlers"
	"relayforge/internal/http/httpx"
	"relayforge/internal/middleware"
)

type Dependencies struct {
	Auth                *handlers.AuthHandler
	Projects            *handlers.ProjectHandler
	APIKeys             *handlers.APIKeyHandler
	WorkerTokens        *handlers.WorkerTokenHandler
	Jobs                *handlers.JobHandler
	Authenticator       middleware.Authenticator
	APIKeyAuthenticator middleware.APIKeyAuthenticator
	RateLimits          middleware.RateLimiters
}

func New(deps Dependencies) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	limitAuth := middleware.RateLimit(deps.RateLimits.Auth, middleware.RateLimitKeyByIP)
	limitSession := middleware.RateLimit(deps.RateLimits.API, middleware.RateLimitKeyBySession)
	limitAPIKey := middleware.RateLimit(deps.RateLimits.API, middleware.RateLimitKeyByAPIKey)
	limitSubmitSession := middleware.RateLimit(deps.RateLimits.JobSubmit, middleware.RateLimitKeyBySession)
	limitSubmitAPIKey := middleware.RateLimit(deps.RateLimits.JobSubmit, middleware.RateLimitKeyByAPIKey)

	mux.Handle("POST /v1/auth/register", limitAuth(http.HandlerFunc(deps.Auth.Register)))
	mux.Handle("POST /v1/auth/login", limitAuth(http.HandlerFunc(deps.Auth.Login)))

	requireSession := middleware.RequireAuth(deps.Authenticator)
	requireAPIKey := middleware.RequireAPIKey(deps.APIKeyAuthenticator)

	mux.Handle("GET /v1/auth/me", requireSession(limitSession(http.HandlerFunc(deps.Auth.Me))))
	mux.Handle("POST /v1/auth/logout", requireSession(limitSession(http.HandlerFunc(deps.Auth.Logout))))

	mux.Handle("POST /v1/projects", requireSession(limitSession(http.HandlerFunc(deps.Projects.Create))))
	mux.Handle("GET /v1/projects", requireSession(limitSession(http.HandlerFunc(deps.Projects.List))))
	mux.Handle("GET /v1/projects/{id}", requireSession(limitSession(http.HandlerFunc(deps.Projects.Get))))
	mux.Handle("PATCH /v1/projects/{id}", requireSession(limitSession(http.HandlerFunc(deps.Projects.Update))))
	mux.Handle("DELETE /v1/projects/{id}", requireSession(limitSession(http.HandlerFunc(deps.Projects.Delete))))

	mux.Handle("POST /v1/projects/{projectID}/api-keys", requireSession(limitSession(http.HandlerFunc(deps.APIKeys.Create))))
	mux.Handle("GET /v1/projects/{projectID}/api-keys", requireSession(limitSession(http.HandlerFunc(deps.APIKeys.List))))
	mux.Handle("DELETE /v1/projects/{projectID}/api-keys/{keyID}", requireSession(limitSession(http.HandlerFunc(deps.APIKeys.Revoke))))

	mux.Handle("POST /v1/projects/{projectID}/worker-tokens", requireSession(limitSession(http.HandlerFunc(deps.WorkerTokens.Create))))
	mux.Handle("GET /v1/projects/{projectID}/worker-tokens", requireSession(limitSession(http.HandlerFunc(deps.WorkerTokens.List))))
	mux.Handle("DELETE /v1/projects/{projectID}/worker-tokens/{tokenID}", requireSession(limitSession(http.HandlerFunc(deps.WorkerTokens.Revoke))))

	mux.Handle("GET /v1/api-keys/current", requireAPIKey(limitAPIKey(http.HandlerFunc(deps.APIKeys.Current))))
	mux.Handle("POST /v1/jobs", requireAPIKey(limitSubmitAPIKey(http.HandlerFunc(deps.Jobs.Submit))))
	mux.Handle("GET /v1/jobs/{id}", requireAPIKey(limitAPIKey(http.HandlerFunc(deps.Jobs.Get))))

	mux.Handle("POST /v1/projects/{projectID}/jobs", requireSession(limitSubmitSession(http.HandlerFunc(deps.Jobs.SubmitForProject))))
	mux.Handle("GET /v1/projects/{projectID}/jobs", requireSession(limitSession(http.HandlerFunc(deps.Jobs.ListForProject))))
	mux.Handle("GET /v1/projects/{projectID}/jobs/{jobID}", requireSession(limitSession(http.HandlerFunc(deps.Jobs.GetForProject))))
	mux.Handle("GET /v1/projects/{projectID}/jobs/{jobID}/executions", requireSession(limitSession(http.HandlerFunc(deps.Jobs.ExecutionsForProject))))

	return mux
}
