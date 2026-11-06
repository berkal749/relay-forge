# AGENTS.md

Guidance for AI coding agents working in the RelayForge repository.

## What this project is

RelayForge is a PostgreSQL-backed background job / task queue service with four parts:

- **API** (`cmd/api`) — session-authenticated dashboard API plus API-key-authenticated job submission API, with invite-only registration, roles, quotas, and Prometheus metrics.
- **Worker** (`cmd/worker`) — project-scoped worker pool that dispatches the outbox, claims jobs with `FOR UPDATE SKIP LOCKED`, executes registered handlers, and records execution attempts/retries.
- **Supervisor** (`cmd/supervisor`) — reconciles active `worker_pools` rows into worker child processes.
- **Dashboard** (`web/`) — React + Vite + Tailwind + shadcn/ui SPA for auth, projects, project settings, API keys, worker pools, worker tokens, quotas, admin invites, job submission, and job inspection.

Stack: Go 1.26, PostgreSQL 17, pgx/v5, sqlc, golang-migrate, React 19, TypeScript, TanStack Query, pnpm.

> Repo status: history begins with the initial scaffold commit. Inspect `git log` rather than assuming earlier history. The latest schema migration is `000011_beta_hardening`.

## Prerequisites

- Go 1.26+
- Node + pnpm
- Docker (for PostgreSQL)
- Optional CLIs installed by Make targets: `migrate` (v4.20.1), `sqlc` (v1.31.1)

## Commands

Backend (from repo root):

```sh
make run-api            # go run ./cmd/api (listens on :8080)
make run-worker         # standalone worker; requires credentials (see below)
make run-supervisor     # reconciles active worker pools into worker processes
make build              # go build ./...
make test               # go test ./...
make fmt                # gofmt -w .
make tidy               # go mod tidy
```

Database:

```sh
cp .env.example .env
docker compose up -d postgres
make migrate-install    # one-time
make migrate-up
make migrate-down
make migrate-status
make migrate-create name=create_something
make sqlc-install       # one-time
make sqlc-generate      # regenerate internal/database/db from sql/queries
```

Web (from repo root):

```sh
make web-install
make web-dev            # Vite on :5173, proxies /api -> :8080
make web-build          # tsc -b && vite build
make web-lint           # oxlint
```

Run a worker directly. Credentials resolve in order: `WORKER_TOKEN` → `WORKER_TOKEN_FILE` → `WORKER_ENROLLMENT_TOKEN`:

```sh
# Option A: explicit long-lived token
export WORKER_PROJECT_ID=<project-uuid>
export WORKER_TOKEN=rf_worker_...
make run-worker

# Option B: zero-touch enrollment from a pool's rf_enroll_ token
export WORKER_ENROLLMENT_TOKEN=rf_enroll_...
export WORKER_TOKEN_FILE=.relayforge/worker-token.json
export RELAYFORGE_API_URL=http://localhost:8080
make run-worker
```

Create `rf_worker_` tokens via `POST /v1/projects/{projectID}/worker-tokens` or the project dashboard, and `rf_enroll_` tokens via `POST /v1/projects/{projectID}/worker-pools/{poolID}/enrollment-tokens`. Tokens are HMAC-hashed with `WORKER_TOKEN_PEPPER` (falling back to `API_KEY_PEPPER`); an explicit token must belong to `WORKER_PROJECT_ID`, and a pool-scoped token requires the pool to be active.

Run the supervisor (builds `bin/relayforge-worker` first and points the supervisor at it):

```sh
make run-supervisor
```

Docker Compose profiles:

```sh
docker compose --profile worker up -d       # one standalone worker
docker compose --profile supervisor up -d   # one worker per active pool
```

Default database URL: `postgres://relayforge:relayforge@localhost:5435/relayforge?sslmode=disable` (host port 5435 on purpose).

## Architecture and layering

```
cmd/api ──► internal/http/routes ──► internal/http/handlers ──► internal/{auth,project,apikey,job}
                  │                        │
                  └── internal/middleware ─┘
                           │
internal/database/db (sqlc-generated) ◄── internal/database (pgxpool)
                           ▲
cmd/worker ──► internal/workertoken (startup auth)
          ├──► internal/worker (credentials, pool, registration) ──► internal/execution
          └──► internal/outbox (dispatcher)

cmd/supervisor ──► internal/supervisor ──► worker_pools table
                                            └── spawns cmd/worker processes
```

Key packages:

| Package | Responsibility |
| --- | --- |
| `internal/config` | Env-var loading/validation. All configuration is here. |
| `internal/database` | pgxpool connection helper. |
| `internal/database/db` | **Generated** by sqlc. Do not edit by hand; change `sql/queries` then `make sqlc-generate`. |
| `internal/auth` | Registration modes, login, opaque session tokens (SHA-256 hashed at rest), bcrypt passwords, bootstrap admin promotion. |
| `internal/invite` | Single-use `rf_invite_` tokens for invite-only registration; hashed at rest with roles and expiry. |
| `internal/apikey` | `rf_live_` API keys, HMAC-SHA256 hashed at rest, expiry/last-used, project ownership checks. |
| `internal/workertoken` | `rf_worker_` and single-use `rf_enroll_` tokens, HMAC-hashed, enrollment exchange. |
| `internal/workerpool` | Worker pool CRUD, validation, ownership, enrollment-token issuance. |
| `internal/supervisor` | Reconciles active `worker_pools` with worker child processes. |
| `internal/secret` | Shared HMAC/SHA-256/prefix helpers for bearer credentials. |
| `internal/project` | Project CRUD and settings; delete = archive (`archived_at`), never hard delete. |
| `internal/job` | Job submission, validation, status model/transitions, reads. |
| `internal/execution` | Claim/succeed/fail transactions, retry backoff, handler registry, dev handlers. |
| `internal/outbox` | Transactional outbox dispatcher + `pg_notify` notifier. |
| `internal/worker` | Worker registration/heartbeat/offline and the concurrent pool. |
| `internal/http/handlers` | HTTP request decoding, response shaping, domain-error → status mapping. |
| `internal/http/routes` | `http.ServeMux` route table and auth middleware wiring. |
| `internal/http/httpx` | JSON encode/decode, error responses, bearer token parsing. |
| `internal/middleware` | Session/API-key auth, context getters, and HTTP rate limiting. |
| `web/src` | Dashboard: `lib/api.ts` is the typed API client, `lib/auth.tsx` holds auth state. |

### Data model (PostgreSQL)

`users` → `projects` (archive via `archived_at`) → `api_keys`, `worker_pools`, `worker_tokens`, `workers`, `jobs` → `executions`, `outbox_messages`; `sessions` belongs to `users`. `workers.worker_token_id` and `workers.worker_pool_id` record which token and pool registered each worker. `worker_tokens.kind` is `worker` or `enrollment`; enrollment tokens carry `pool_id`, `consumed_at`, and a short expiry.

Important invariants:

- Job lifecycle: `pending → scheduled|running|cancelled`, `scheduled → running|cancelled`, `running → succeeded|failed|scheduled|cancelled`; terminal states cannot change. See `internal/job/status.go` and `docs/job-lifecycle.md`.
- Submitting a job and creating its outbox row happen in one transaction.
- Claiming a job increments `attempts`, sets `running`, and creates a `running` execution in one transaction.
- Finishing an attempt updates execution + job in one transaction.
- Project deletion archives; API-key auth and project reads filter out archived projects.
- Revoked API keys and archived projects cannot authenticate. API keys are HMAC-SHA256 hashed with `API_KEY_PEPPER`, may expire, track `last_used_at`, and expose only `key_prefix`. Legacy unsalted hashes are upgraded on successful authentication.
- Job submission supports per-project idempotency via the `Idempotency-Key` header. The key and a request fingerprint are stored on `jobs`; replays return the original job, and a mismatched fingerprint is a conflict.
- Users have a `role` (`user` or `admin`). Admin-only routes use `middleware.RequireAdmin` after session auth.
- `REGISTRATION_MODE` is `open`, `invite`, or `closed`. Production requires an explicit non-open mode. Invite registration consumes the invite and creates the user in one transaction; invite tokens are single-use, optionally email-bound, optionally admin-granting, and expire after `INVITE_TTL`.
- Quotas: `MAX_PROJECTS_PER_USER`, `MAX_WORKER_POOLS_PER_PROJECT`, `MAX_ACTIVE_POOLS_PER_PROJECT`, `MAX_WORKER_CONCURRENCY_PER_PROJECT`, `MAX_QUEUED_JOBS_PER_PROJECT`. `0` disables a limit. The supervisor re-applies active-pool and concurrency caps.
- Projects carry settings: `default_queue_name`, `default_max_attempts`, `default_priority`, `job_retention_days`, `max_concurrency`, `rate_limit_per_minute`, `default_job_timeout_seconds`. Submission defaults are applied by `job.Service.Submit`; worker limits are read at startup; retention is re-read on each cleanup tick.
- `PATCH /v1/projects/{id}` is a partial update: omitted fields stay unchanged, and an empty update is rejected.
- Worker pools are per-project desired config (`name`, `concurrency`, `active`/`paused`, `queue_names`, `job_timeout_seconds`, `max_attempts`, `retry_backoff`, retry delays). Names are unique per project; concurrency is 1–1000 (default 4). Empty `queue_names` means all queues.
- Claiming filters on the pool's queue list and applies the pool's `max_attempts` override to the job in the same transaction. Pool retry policy controls the backoff when an attempt fails.
- Enrollment tokens are single-use, default to a 15-minute TTL, and are consumed atomically via `ConsumeEnrollmentToken`. Redeeming one issues a `worker` token bound to the same pool.
- A worker token bound to a paused pool, a revoked/expired token, or an archived project cannot authenticate.
- Remote workers authenticate with the same token but never connect to PostgreSQL. Claims create an execution with `lease_expires_at`; heartbeats renew leases; the API reaper fails expired executions with `worker lease expired`.
- Token rotation creates a replacement token, records `rotated_at`/`replaced_by_id`, and caps the old token at `TOKEN_ROTATION_GRACE` (default 24h).
- `worker_pools.allowed_cidrs` is enforced on remote register/claim/heartbeat/complete. `TRUST_PROXY_HEADERS` and `WORKER_MTLS_HEADER` provide proxy-based identity controls.

### Worker processing flow

0. `WORKER_MODE` selects `direct` (PostgreSQL) or `remote` (API only). When unset, an explicit `DATABASE_URL` selects direct and otherwise remote. Credentials resolve as `WORKER_TOKEN` → `WORKER_TOKEN_FILE` → `WORKER_ENROLLMENT_TOKEN` (`POST /v1/workers/enroll`, then cache). Pool membership overrides concurrency, name, queues, job timeout, and retry policy.
1. The API outbox dispatcher polls on `API_OUTBOX_INTERVAL`, claims pending messages with `FOR UPDATE SKIP LOCKED`, calls `pg_notify('relayforge_jobs', job_id)`, and marks messages processed in the same transaction. Direct workers also run a dispatcher.
2. Each pool goroutine calls its `Backend.Claim`; direct mode uses `execution.Service.Claim`, remote mode calls `POST /v1/workers/claim`. Claimability requires matching project, `pending`/`scheduled`, `scheduled_at <= now()`, an attempt below the pool override (or the job's `max_attempts`), a queue in the pool's `queue_names`, and a processed outbox message. The claim writes the pool's `max_attempts` override onto the job and records a lease.
3. Handler runs under the pool's `job_timeout_seconds`, then the project default, then `JOB_TIMEOUT`.
4. Failure retries use the pool's `retry_backoff` (fixed or exponential) with its base/max delays (falling back to `RETRY_BASE_DELAY`/`RETRY_MAX_DELAY`); exhausted attempts become terminal `failed`.
5. Project `max_concurrency` caps the pool, `rate_limit_per_minute` throttles claims in fixed one-minute windows, and `default_job_timeout_seconds` is the project-level timeout fallback. These are read at worker startup.
6. The API purges terminal jobs past `job_retention_days` on `API_CLEANUP_INTERVAL`; direct workers also run cleanup on `WORKER_CLEANUP_INTERVAL`. `0` disables cleanup.
6a. The API reaper fails executions whose lease elapsed and retries them under the pool policy. Completions/failures are idempotent by execution ID; stale workers are swept offline on `API_WORKER_SWEEP_INTERVAL`.
7. Shutdown stops claiming, drains up to `WORKER_SHUTDOWN_TIMEOUT`, cancels remaining handlers, records cancelled executions, marks the worker offline.
8. The **supervisor** reconciles `worker_pools` every `SUPERVISOR_POLL_INTERVAL`: it issues an enrollment token, spawns one remote `relayforge-worker` per active pool (clearing `DATABASE_URL`), restarts on name/concurrency/queue/timeout/retry changes, and stops workers when a pool is paused/deleted.
9. Note: the `pg_notify` message is currently **not consumed** by `LISTEN`; workers poll. Treat notifications as forward-looking.

Built-in handler types: `echo`, `noop`, `sleep` (payload `{"duration_ms": N}`). Register real handlers in `cmd/worker/main.go`:

```go
registry.Register("send_email", sendEmailHandler)
```

## Conventions

### Go

- Keep dependency wiring in `cmd/`, business logic in `internal/<domain>`, HTTP concerns in `internal/http/handlers`.
- Services depend on `*db.Queries` for simple reads/writes and on `*pgxpool.Pool` when they need explicit transactions (job submit, execution claim/finish).
- Domain errors are package-level sentinels (`ErrProjectNotFound`, `ErrInvalidPayload`, …). Handlers map them to HTTP statuses in `write<Domain>Error` helpers; unknown errors are logged with `slog` and returned as opaque `500`.
- Always `defer tx.Rollback(ctx)` (ignoring the returned error) and commit explicitly; follow the existing transaction pattern.
- Use `errors.Is` for comparisons, `fmt.Errorf("...: %w", err)` for wrapping.
- JSON responses are wrapped in a named key: `{"job": …}`, `{"jobs": […]}` (always a non-nil slice), `{"project": …}`, `{"api_keys": […]}`. Errors are `{"error": "message"}`.
- Request decoding uses `httpx.DecodeJSON` (1 MiB cap, unknown fields rejected, single object only).
- New routes go in `internal/http/routes/routes.go` with method-prefixed patterns (e.g. `POST /v1/jobs`). Protected routes use `middleware.RequireAuth` (sessions) or `middleware.RequireAPIKey` (project API keys).
- Use `slog` for logging; do not add a logging framework.
- HTTP rate limiting lives in `internal/middleware/ratelimit.go`. Limits are in-memory fixed windows keyed by IP, session user, or API key; they are per process, so multi-instance deployments need a shared store.
- Secret-bearing responses (login, API key creation, worker token and enrollment token creation) must set `Cache-Control: no-store`. Raw credentials are only returned once, at creation.
- Production config validation lives in `internal/config`: independent 32+ character peppers, explicit non-open registration mode, explicit `TRUST_PROXY_HEADERS`, and `RELAYFORGE_API_URL` for remote workers. Compose has no secret defaults.
- Prometheus instrumentation lives in `internal/metrics` and is served on `METRICS_ADDR` (default `:9090`). `/healthz` is liveness and `/readyz` pings PostgreSQL.
- Worker credentials are managed in `internal/workertoken`; enrollment is resolved in `internal/worker/credentials.go`; remote identity and HTTP transport live in `internal/worker/remote.go`; `internal/supervisor` reconciles pools. Remote workers are API-only; direct workers use `DATABASE_URL`.
- `cmd/supervisor` spawns the binary at `SUPERVISOR_WORKER_BINARY` (default `relayforge-worker`) and passes pool config through the environment. Cached tokens live under `SUPERVISOR_TOKEN_DIR` with `0600` permissions.

### Database / sqlc

- Change schema only via a new sequential migration pair in `migrations/` (`make migrate-create name=...`); write both `.up.sql` and `.down.sql`.
- Change queries in `sql/queries/*.sql` with `-- name: QueryName :one|:many|:exec`, then run `make sqlc-generate`. Never hand-edit `internal/database/db/*.go`.
- Prefer `FOR UPDATE SKIP LOCKED` for claiming work; keep claim/dispatch operations transactional.
- Timestamps are `TIMESTAMPTZ`; UUIDs use `github.com/google/uuid`; nullable columns are pointers (see `sqlc.yaml`).

### Web

- API calls belong in `web/src/lib/api.ts`; add types alongside existing exported types.
- Data fetching/mutations use TanStack Query; UI primitives live in `web/src/components/ui` (shadcn/base-ui).
- The project page composes settings plus `api-key-panel`, `worker-fleet-panel`, `worker-pool-panel`, and `worker-token-panel`; keep new project-scoped admin UI as a panel component.
- Path alias `@/` maps to `web/src`.
- Job status polling refreshes every 2s while active (see `pages/job.tsx` / `components/job-table.tsx`).
- Styling is Tailwind v4 + `cn()` helper; keep components consistent with existing ones.
- `web/dist` is generated build output ignored by `web/.gitignore`; run `make web-build` only when you need a local bundle.

## Testing

- `make test` / `go test ./...` is the baseline. Unit tests cover auth validation, API key hashing, worker tokens/enrollment helpers, worker pools, job defaults/idempotency, backoff/status, project settings, the worker limiter, and HTTP rate limiting.
- Tests live in `internal/<domain>/*_test.go` and are table-driven where practical.
- Remote protocol, CIDR, egress-allowlist, and worker-credential helpers have unit tests. `internal/integration` has database-backed invite, quota, job lifecycle, and lease-recovery tests gated on `TEST_DATABASE_URL`. Keep new unit tests free of external dependencies; integration tests must skip cleanly when the database URL is absent.
- Run `make web-lint` and `make web-build` after frontend changes.
- `internal/integration` contains database integration tests gated on `TEST_DATABASE_URL`; `make test-integration` runs them against `DATABASE_URL`.
- `.github/workflows/ci.yml` runs build, vet, tests, race tests, sqlc drift, migration round-trip, web lint/build, and Docker builds.
- `scripts/backup.sh` writes a custom-format `pg_dump` with retention.

## Roadmap / known gaps

Planned work, roughly in order:

1. **Structured logging and metrics** — consistent `slog` attributes and request IDs. Prometheus counters/histograms for HTTP, jobs, workers, queues, outbox, and lease reaping are implemented.
2. **Integration and failure-recovery tests** — API/database integration coverage plus worker crash, retry exhaustion, timeout, shutdown, and outbox failure scenarios.
3. **GitHub Actions CI** — implemented in `.github/workflows/ci.yml`.
4. **Production Docker deployment** — non-root runtime, resource limits, private networking, and a Caddy TLS/dashboard image. Kubernetes manifests remain future work.
5. **Documentation and demo** — `docs/operations.md` covers beta operations; a recorded walkthrough remains.

Other known gaps:

- No job cancellation endpoint even though the status model and docs support `cancelled`; `UpdateJobStatus` is unused.
- Invite delivery is manual (copy the link/token); there is no email sending.
- Quotas are global env defaults; per-project or per-plan overrides are not implemented.
- No `LISTEN` consumer for `relayforge_jobs`; the worker relies on polling.
- `outbox_messages` has a `failed` status but no retry/dead-letter handling.
- Worker is single-project per process; the supervisor runs one process per active pool. There is no replicas-per-pool field, multi-project host, or autoscaling.
- No per-queue concurrency or job priority feedback beyond SQL ordering.
- API and worker rate limiting are per process; multi-instance deployments need a shared store.
- Metrics and rate limits are single-instance; no distributed tracing or OpenTelemetry.
- API key rotation and worker token scopes are not implemented (worker token expiry, revocation, last-used tracking, rotation with grace, and enrollment-based rotation are).
- Remote workers no longer need database access. Direct mode still uses a database role; worker token auth is application-level and does not restrict that role.
- Token rotation is manual; workers reload the new token on restart (or when the cached token file is replaced). There is no push-based config or token delivery.

## Before finishing a task

1. `gofmt`/`make fmt` for Go changes.
2. `go build ./...`, `go vet ./...`, and `go test ./...`.
3. `make web-lint` and `make web-build` for web changes.
4. Run `make sqlc-generate` if queries/schema changed, and verify the migration down/up round-trip when adding a migration.
5. Update `README.md`, `docs/`, and this file if architecture, commands, or behavior changed.
6. Never commit `.env` files, raw tokens, peppers, or database volumes; only `.env.example` belongs in git.
