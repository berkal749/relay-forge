# AGENTS.md

Guidance for AI coding agents working in the RelayForge repository.

## What this project is

RelayForge is a PostgreSQL-backed background job / task queue service with three parts:

- **API** (`cmd/api`) — session-authenticated dashboard API plus API-key-authenticated job submission API.
- **Worker** (`cmd/worker`) — project-scoped worker pool that dispatches the outbox, claims jobs with `FOR UPDATE SKIP LOCKED`, executes registered handlers, and records execution attempts/retries.
- **Dashboard** (`web/`) — React + Vite + Tailwind + shadcn/ui SPA for auth, projects, project settings, API keys, worker tokens, job submission, and job inspection.

Stack: Go 1.26, PostgreSQL 17, pgx/v5, sqlc, golang-migrate, React 19, TypeScript, TanStack Query, pnpm.

> Repo status: history begins with the initial scaffold commit. Inspect `git log` rather than assuming earlier history. The latest schema migration is `000007_worker_tokens`.

## Prerequisites

- Go 1.26+
- Node + pnpm
- Docker (for PostgreSQL)
- Optional CLIs installed by Make targets: `migrate` (v4.20.1), `sqlc` (v1.31.1)

## Commands

Backend (from repo root):

```sh
make run-api            # go run ./cmd/api (listens on :8080)
make run-worker         # requires WORKER_PROJECT_ID and WORKER_TOKEN
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

Run the worker (must set a project UUID from the API):

```sh
export WORKER_PROJECT_ID=<project-uuid>
export WORKER_TOKEN=rf_worker_...
make run-worker
```

Create the `rf_worker_` token via `POST /v1/projects/{projectID}/worker-tokens` or the project dashboard. Tokens are HMAC-hashed with `WORKER_TOKEN_PEPPER` (falling back to `API_KEY_PEPPER`) and must belong to `WORKER_PROJECT_ID`.

Docker Compose worker profile:

```sh
docker compose --profile worker up -d
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
          ├──► internal/worker (pool, registration) ──► internal/execution
          └──► internal/outbox (dispatcher)
```

Key packages:

| Package | Responsibility |
| --- | --- |
| `internal/config` | Env-var loading/validation. All configuration is here. |
| `internal/database` | pgxpool connection helper. |
| `internal/database/db` | **Generated** by sqlc. Do not edit by hand; change `sql/queries` then `make sqlc-generate`. |
| `internal/auth` | Registration, login, opaque session tokens (SHA-256 hashed at rest), bcrypt passwords. |
| `internal/apikey` | `rf_live_` API keys, HMAC-SHA256 hashed at rest, expiry/last-used, project ownership checks. |
| `internal/workertoken` | `rf_worker_` worker tokens, HMAC-hashed, authenticated at worker startup. |
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

`users` → `projects` (archive via `archived_at`) → `api_keys`, `worker_tokens`, `workers`, `jobs` → `executions`, `outbox_messages`; `sessions` belongs to `users`. `workers.worker_token_id` records which token authenticated each worker.

Important invariants:

- Job lifecycle: `pending → scheduled|running|cancelled`, `scheduled → running|cancelled`, `running → succeeded|failed|scheduled|cancelled`; terminal states cannot change. See `internal/job/status.go` and `docs/job-lifecycle.md`.
- Submitting a job and creating its outbox row happen in one transaction.
- Claiming a job increments `attempts`, sets `running`, and creates a `running` execution in one transaction.
- Finishing an attempt updates execution + job in one transaction.
- Project deletion archives; API-key auth and project reads filter out archived projects.
- Revoked API keys and archived projects cannot authenticate. API keys are HMAC-SHA256 hashed with `API_KEY_PEPPER`, may expire, track `last_used_at`, and expose only `key_prefix`. Legacy unsalted hashes are upgraded on successful authentication.
- Job submission supports per-project idempotency via the `Idempotency-Key` header. The key and a request fingerprint are stored on `jobs`; replays return the original job, and a mismatched fingerprint is a conflict.
- Projects carry settings: `default_queue_name`, `default_max_attempts`, `default_priority`, `job_retention_days`, `max_concurrency`, `rate_limit_per_minute`, `default_job_timeout_seconds`. Submission defaults are applied by `job.Service.Submit`; worker limits are read at startup; retention is re-read on each cleanup tick.
- `PATCH /v1/projects/{id}` is a partial update: omitted fields stay unchanged, and an empty update is rejected.

### Worker processing flow

1. Outbox dispatcher polls every second, claims pending messages with `FOR UPDATE SKIP LOCKED`, calls `pg_notify('relayforge_jobs', job_id)`, and marks messages processed in the same transaction.
2. Each pool goroutine calls `execution.Service.Claim`; claimability requires matching project, `pending`/`scheduled`, `scheduled_at <= now()`, `attempts < max_attempts`, and a processed outbox message.
3. Handler runs under `JOB_TIMEOUT`.
4. Failure retries with exponential backoff `RETRY_BASE_DELAY * 2^(attempt-1)` capped at `RETRY_MAX_DELAY`; exhausted attempts become terminal `failed`.
5. Project `max_concurrency` caps the pool, `rate_limit_per_minute` throttles claims in fixed one-minute windows, and `default_job_timeout_seconds` overrides `JOB_TIMEOUT`. These are read at worker startup.
6. `worker.RunCleanup` periodically purges terminal jobs past `job_retention_days` (re-read each tick; `0` disables).
7. Shutdown stops claiming, drains up to `WORKER_SHUTDOWN_TIMEOUT`, cancels remaining handlers, records cancelled executions, marks the worker offline.
8. Note: the `pg_notify` message is currently **not consumed** by `LISTEN`; workers poll. Treat notifications as forward-looking.

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
- Secret-bearing responses (login, API key creation, worker token creation) must set `Cache-Control: no-store`. Raw credentials are only returned once, at creation.
- Worker tokens are managed in `internal/workertoken` and enforced in `cmd/worker` before `worker.Register`. This is app-level auth; workers still use `DATABASE_URL` directly.

### Database / sqlc

- Change schema only via a new sequential migration pair in `migrations/` (`make migrate-create name=...`); write both `.up.sql` and `.down.sql`.
- Change queries in `sql/queries/*.sql` with `-- name: QueryName :one|:many|:exec`, then run `make sqlc-generate`. Never hand-edit `internal/database/db/*.go`.
- Prefer `FOR UPDATE SKIP LOCKED` for claiming work; keep claim/dispatch operations transactional.
- Timestamps are `TIMESTAMPTZ`; UUIDs use `github.com/google/uuid`; nullable columns are pointers (see `sqlc.yaml`).

### Web

- API calls belong in `web/src/lib/api.ts`; add types alongside existing exported types.
- Data fetching/mutations use TanStack Query; UI primitives live in `web/src/components/ui` (shadcn/base-ui).
- Path alias `@/` maps to `web/src`.
- Job status polling refreshes every 2s while active (see `pages/job.tsx` / `components/job-table.tsx`).
- Styling is Tailwind v4 + `cn()` helper; keep components consistent with existing ones.
- `web/dist` is generated build output ignored by `web/.gitignore`; run `make web-build` only when you need a local bundle.

## Testing

- `make test` / `go test ./...` is the baseline. Unit tests cover auth validation, API key hashing, worker tokens, job defaults/idempotency, backoff/status, project settings, the worker limiter, and HTTP rate limiting.
- Tests live in `internal/<domain>/*_test.go` and are table-driven where practical.
- There are still no HTTP handler or database integration tests. Keep new unit tests free of external dependencies; integration tests should be isolated and clearly marked.
- Run `make web-lint` and `make web-build` after frontend changes.

## Roadmap / known gaps

Planned work, roughly in order:

1. **Structured logging and metrics** — consistent `slog` attributes, request IDs, and counters/histograms for jobs, executions, retries, API errors, and rate-limit rejections.
2. **Integration and failure-recovery tests** — API/database integration coverage plus worker crash, retry exhaustion, timeout, shutdown, and outbox failure scenarios.
3. **GitHub Actions CI** — build, vet, test, sqlc drift check, migration check, frontend lint/build.
4. **Production Docker deployment** — non-root runtime, health checks, resource limits, and hardened Compose/Kubernetes manifests. Current Dockerfile/Compose are development-oriented.
5. **Documentation and demo** — finish operational docs and record a walkthrough.

Other known gaps:

- No job cancellation endpoint even though the status model and docs support `cancelled`; `UpdateJobStatus` is unused.
- No `LISTEN` consumer for `relayforge_jobs`; the worker relies on polling.
- `outbox_messages` has a `failed` status but no retry/dead-letter handling.
- Worker is single-project (`WORKER_PROJECT_ID`); no multi-project or queue subscription model.
- No per-queue concurrency or job priority feedback beyond SQL ordering.
- API and worker rate limiting are per process; multi-instance deployments need a shared store.
- API key and worker token scopes/rotation endpoints are not implemented (expiry, revocation, and last-used tracking are).
- Worker token auth is application-level; it does not restrict direct database access.

## Before finishing a task

1. `gofmt`/`make fmt` for Go changes.
2. `go build ./...`, `go vet ./...`, and `go test ./...`.
3. `make web-lint` and `make web-build` for web changes.
4. Run `make sqlc-generate` if queries/schema changed, and verify the migration down/up round-trip when adding a migration.
5. Update `README.md`, `docs/`, and this file if architecture, commands, or behavior changed.
6. Never commit `.env` files, raw tokens, peppers, or database volumes; only `.env.example` belongs in git.
