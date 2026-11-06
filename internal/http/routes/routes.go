package routes

import (
	"context"
	"net/http"
	"time"

	"relayforge/internal/http/handlers"
	"relayforge/internal/http/httpx"
	"relayforge/internal/middleware"
)

type Dependencies struct {
	Auth                     *handlers.AuthHandler
	Invites                  *handlers.InviteHandler
	Projects                 *handlers.ProjectHandler
	APIKeys                  *handlers.APIKeyHandler
	WorkerTokens             *handlers.WorkerTokenHandler
	WorkerPools              *handlers.WorkerPoolHandler
	Enrollment               *handlers.EnrollmentHandler
	WorkerRemote             *handlers.WorkerRemoteHandler
	Schedules                *handlers.ScheduleHandler
	Channels                 *handlers.NotificationChannelHandler
	Jobs                     *handlers.JobHandler
	Authenticator            middleware.Authenticator
	APIKeyAuthenticator      middleware.APIKeyAuthenticator
	WorkerTokenAuthenticator middleware.WorkerTokenAuthenticator
	RateLimits               middleware.RateLimiters
	Ready                    func(ctx context.Context) error
}

func New(deps Dependencies) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if deps.Ready == nil {
			httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := deps.Ready(ctx); err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not ready"})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	limitAuth := middleware.RateLimit(deps.RateLimits.Auth, middleware.RateLimitKeyByIP)
	limitSession := middleware.RateLimit(deps.RateLimits.API, middleware.RateLimitKeyBySession)
	limitAPIKey := middleware.RateLimit(deps.RateLimits.API, middleware.RateLimitKeyByAPIKey)
	limitSubmitSession := middleware.RateLimit(deps.RateLimits.JobSubmit, middleware.RateLimitKeyBySession)
	limitSubmitAPIKey := middleware.RateLimit(deps.RateLimits.JobSubmit, middleware.RateLimitKeyByAPIKey)

	mux.Handle("GET /v1/auth/config", limitAuth(http.HandlerFunc(deps.Auth.Config)))
	mux.Handle("POST /v1/auth/register", limitAuth(http.HandlerFunc(deps.Auth.Register)))
	mux.Handle("POST /v1/auth/login", limitAuth(http.HandlerFunc(deps.Auth.Login)))

	requireSession := middleware.RequireAuth(deps.Authenticator)
	requireAdmin := middleware.RequireAdmin
	requireAPIKey := middleware.RequireAPIKey(deps.APIKeyAuthenticator)

	mux.Handle("GET /v1/auth/me", requireSession(limitSession(http.HandlerFunc(deps.Auth.Me))))
	mux.Handle("POST /v1/auth/logout", requireSession(limitSession(http.HandlerFunc(deps.Auth.Logout))))

	if deps.Invites != nil {
		mux.Handle("POST /v1/admin/invites", requireSession(limitSession(requireAdmin(http.HandlerFunc(deps.Invites.Create)))))
		mux.Handle("GET /v1/admin/invites", requireSession(limitSession(requireAdmin(http.HandlerFunc(deps.Invites.List)))))
		mux.Handle("DELETE /v1/admin/invites/{id}", requireSession(limitSession(requireAdmin(http.HandlerFunc(deps.Invites.Revoke)))))
	}

	mux.Handle("POST /v1/projects", requireSession(limitSession(http.HandlerFunc(deps.Projects.Create))))
	mux.Handle("GET /v1/projects", requireSession(limitSession(http.HandlerFunc(deps.Projects.List))))
	mux.Handle("GET /v1/projects/{id}", requireSession(limitSession(http.HandlerFunc(deps.Projects.Get))))
	mux.Handle("PATCH /v1/projects/{id}", requireSession(limitSession(http.HandlerFunc(deps.Projects.Update))))
	mux.Handle("DELETE /v1/projects/{id}", requireSession(limitSession(http.HandlerFunc(deps.Projects.Delete))))

	mux.Handle("POST /v1/projects/{projectID}/api-keys", requireSession(limitSession(http.HandlerFunc(deps.APIKeys.Create))))
	mux.Handle("GET /v1/projects/{projectID}/api-keys", requireSession(limitSession(http.HandlerFunc(deps.APIKeys.List))))
	mux.Handle("DELETE /v1/projects/{projectID}/api-keys/{keyID}", requireSession(limitSession(http.HandlerFunc(deps.APIKeys.Revoke))))

	mux.Handle("POST /v1/projects/{projectID}/worker-tokens", requireSession(limitSession(http.HandlerFunc(deps.WorkerTokens.Create))))
	mux.Handle("POST /v1/projects/{projectID}/worker-tokens/{tokenID}/rotate", requireSession(limitSession(http.HandlerFunc(deps.WorkerTokens.Rotate))))
	mux.Handle("GET /v1/projects/{projectID}/worker-tokens", requireSession(limitSession(http.HandlerFunc(deps.WorkerTokens.List))))
	mux.Handle("DELETE /v1/projects/{projectID}/worker-tokens/{tokenID}", requireSession(limitSession(http.HandlerFunc(deps.WorkerTokens.Revoke))))

	mux.Handle("POST /v1/projects/{projectID}/worker-pools", requireSession(limitSession(http.HandlerFunc(deps.WorkerPools.Create))))
	mux.Handle("GET /v1/projects/{projectID}/worker-pools", requireSession(limitSession(http.HandlerFunc(deps.WorkerPools.List))))
	mux.Handle("GET /v1/projects/{projectID}/worker-pools/stats", requireSession(limitSession(http.HandlerFunc(deps.WorkerPools.Stats))))
	mux.Handle("GET /v1/projects/{projectID}/worker-pools/usage", requireSession(limitSession(http.HandlerFunc(deps.WorkerPools.Usage))))
	mux.Handle("PATCH /v1/projects/{projectID}/worker-pools/{poolID}", requireSession(limitSession(http.HandlerFunc(deps.WorkerPools.Update))))
	mux.Handle("DELETE /v1/projects/{projectID}/worker-pools/{poolID}", requireSession(limitSession(http.HandlerFunc(deps.WorkerPools.Delete))))
	mux.Handle("POST /v1/projects/{projectID}/worker-pools/{poolID}/enrollment-tokens", requireSession(limitSession(http.HandlerFunc(deps.WorkerPools.CreateEnrollmentToken))))

	mux.Handle("POST /v1/projects/{projectID}/notification-channels", requireSession(limitSession(http.HandlerFunc(deps.Channels.Create))))
	mux.Handle("GET /v1/projects/{projectID}/notification-channels", requireSession(limitSession(http.HandlerFunc(deps.Channels.List))))
	mux.Handle("DELETE /v1/projects/{projectID}/notification-channels/{channelID}", requireSession(limitSession(http.HandlerFunc(deps.Channels.Delete))))

	mux.Handle("POST /v1/projects/{projectID}/schedules", requireSession(limitSession(http.HandlerFunc(deps.Schedules.Create))))
	mux.Handle("GET /v1/projects/{projectID}/schedules", requireSession(limitSession(http.HandlerFunc(deps.Schedules.List))))
	mux.Handle("PATCH /v1/projects/{projectID}/schedules/{scheduleID}", requireSession(limitSession(http.HandlerFunc(deps.Schedules.Update))))
	mux.Handle("DELETE /v1/projects/{projectID}/schedules/{scheduleID}", requireSession(limitSession(http.HandlerFunc(deps.Schedules.Delete))))

	mux.Handle("GET /v1/projects/{projectID}/workers", requireSession(limitSession(http.HandlerFunc(deps.WorkerPools.Workers))))
	mux.Handle("GET /v1/projects/{projectID}/worker-pools/{poolID}/workers", requireSession(limitSession(http.HandlerFunc(deps.WorkerPools.PoolWorkers))))

	mux.Handle("POST /v1/workers/enroll", limitAuth(http.HandlerFunc(deps.Enrollment.Enroll)))

	requireWorker := middleware.RequireWorkerToken(deps.WorkerTokenAuthenticator)
	mux.Handle("POST /v1/workers/register", requireWorker(http.HandlerFunc(deps.WorkerRemote.Register)))
	mux.Handle("POST /v1/workers/heartbeat", requireWorker(http.HandlerFunc(deps.WorkerRemote.Heartbeat)))
	mux.Handle("POST /v1/workers/claim", requireWorker(http.HandlerFunc(deps.WorkerRemote.Claim)))
	mux.Handle("POST /v1/workers/jobs", requireWorker(http.HandlerFunc(deps.WorkerRemote.Submit)))
	mux.Handle("GET /v1/workers/notification-channels/{channelID}", requireWorker(http.HandlerFunc(deps.WorkerRemote.ResolveNotificationChannel)))
	mux.Handle("POST /v1/workers/jobs/{jobID}/complete", requireWorker(http.HandlerFunc(deps.WorkerRemote.Complete)))
	mux.Handle("POST /v1/workers/jobs/{jobID}/fail", requireWorker(http.HandlerFunc(deps.WorkerRemote.Fail)))
	mux.Handle("POST /v1/workers/offline", requireWorker(http.HandlerFunc(deps.WorkerRemote.Offline)))

	mux.Handle("GET /v1/api-keys/current", requireAPIKey(limitAPIKey(http.HandlerFunc(deps.APIKeys.Current))))
	mux.Handle("POST /v1/jobs", requireAPIKey(limitSubmitAPIKey(http.HandlerFunc(deps.Jobs.Submit))))
	mux.Handle("GET /v1/jobs/{id}", requireAPIKey(limitAPIKey(http.HandlerFunc(deps.Jobs.Get))))

	mux.Handle("POST /v1/projects/{projectID}/jobs", requireSession(limitSubmitSession(http.HandlerFunc(deps.Jobs.SubmitForProject))))
	mux.Handle("GET /v1/projects/{projectID}/jobs", requireSession(limitSession(http.HandlerFunc(deps.Jobs.ListForProject))))
	mux.Handle("GET /v1/projects/{projectID}/jobs/{jobID}", requireSession(limitSession(http.HandlerFunc(deps.Jobs.GetForProject))))
	mux.Handle("GET /v1/projects/{projectID}/jobs/{jobID}/executions", requireSession(limitSession(http.HandlerFunc(deps.Jobs.ExecutionsForProject))))

	return mux
}
