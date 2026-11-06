# RelayForge

Basic Go project template for the RelayForge API and worker.

## Structure

```
relayforge/
├── cmd/
│   ├── api/              # API entrypoint
│   ├── worker/           # Worker entrypoint
│   └── supervisor/       # Worker pool supervisor entrypoint
├── internal/             # Application packages
│   ├── config/
│   ├── database/
│   ├── auth/
│   ├── project/
│   ├── apikey/
│   ├── job/
│   ├── queue/
│   ├── worker/
│   ├── workerpool/
│   ├── supervisor/
│   ├── workertoken/
│   ├── secret/
│   ├── execution/
│   ├── middleware/
│   └── http/
│       ├── handlers/
│       └── routes/
├── web/                  # React developer dashboard
├── migrations/           # Database migrations
├── sql/queries/          # SQL queries
├── docs/                 # Project documentation
├── deployments/docker/   # Docker deployment files
├── scripts/              # Development and ops scripts
├── go.mod
├── Makefile
├── Dockerfile
└── compose.yaml
```

## Getting started

The API listens on `:8080` by default and connects to PostgreSQL on startup. The worker runs the transactional outbox dispatcher.

```sh
make run-api
make run-worker
```

Run the dashboard in another terminal:

```sh
make web-install
make web-dev
```

The dashboard runs at `http://localhost:5173` and proxies `/api` to the API at `http://localhost:8080`.

Other useful commands:

```sh
make build
make test
make fmt
make tidy
make run-supervisor
make web-build
make web-lint
```

## API

Unless noted otherwise, protected endpoints require a session token:

```http
Authorization: Bearer <session-token>
```

### Authentication

| Method | Endpoint | Description |
| --- | --- | --- |
| `POST` | `/v1/auth/register` | Register with `email` and `password` |
| `POST` | `/v1/auth/login` | Create a session and return a bearer token |
| `GET` | `/v1/auth/me` | Return the current user |
| `POST` | `/v1/auth/logout` | Revoke the current session |

Sessions are stored as SHA-256 hashes of opaque random tokens. Session lifetime defaults to 30 days and can be set with `SESSION_TTL`.

### Projects

| Method | Endpoint | Description |
| --- | --- | --- |
| `POST` | `/v1/projects` | Create a project |
| `GET` | `/v1/projects` | List the current user's projects |
| `GET` | `/v1/projects/{id}` | Get a project |
| `PATCH` | `/v1/projects/{id}` | Update a project's name and/or settings |
| `DELETE` | `/v1/projects/{id}` | Archive a project without deleting its history |

Archived projects are hidden from project and API key operations.

`PATCH` accepts any subset of fields; omitted fields keep their current values.

### Project settings

| Field | Default | Description |
| --- | --- | --- |
| `default_queue_name` | `default` | Queue used when a job omits `queue_name` |
| `default_max_attempts` | `3` | Max attempts used when a job omits `max_attempts` |
| `default_priority` | `0` | Priority used when a job omits `priority` |
| `job_retention_days` | `30` | Delete terminal jobs older than this; `0` keeps history forever |
| `max_concurrency` | `0` | Cap on worker concurrency; `0` uses the worker's configured value |
| `rate_limit_per_minute` | `0` | Maximum job claims started per minute; `0` is unlimited |
| `default_job_timeout_seconds` | `0` | Per-job execution timeout; `0` uses the worker's `JOB_TIMEOUT` |

Job defaults are applied at submission time. `max_concurrency`, `rate_limit_per_minute`, and `default_job_timeout_seconds` are read when a worker starts, so restart the worker to apply changes to those settings. Retention cleanup runs in the API on `API_CLEANUP_INTERVAL` (and in direct workers on `WORKER_CLEANUP_INTERVAL`); both re-read the retention value each run.

### API keys

| Method | Endpoint | Description |
| --- | --- | --- |
| `POST` | `/v1/projects/{projectID}/api-keys` | Generate an API key |
| `GET` | `/v1/projects/{projectID}/api-keys` | List API key metadata |
| `DELETE` | `/v1/projects/{projectID}/api-keys/{keyID}` | Revoke an API key |
| `GET` | `/v1/api-keys/current` | Validate an API key via bearer auth |

### Worker tokens

Workers must authenticate with a project-scoped worker token before registering.

| Method | Endpoint | Description |
| --- | --- | --- |
| `POST` | `/v1/projects/{projectID}/worker-tokens` | Create a worker token (optional `expires_in_days`) |
| `GET` | `/v1/projects/{projectID}/worker-tokens` | List worker token metadata |
| `POST` | `/v1/projects/{projectID}/worker-tokens/{tokenID}/rotate` | Issue a replacement token; the old token keeps working for the grace period |
| `DELETE` | `/v1/projects/{projectID}/worker-tokens/{tokenID}` | Revoke a worker token |

Worker tokens use the `rf_worker_` prefix and are returned only once at creation. They are hashed with HMAC-SHA256 using `WORKER_TOKEN_PEPPER` (falling back to `API_KEY_PEPPER`), track `last_used_at`, and only a short prefix is shown afterward. The API and worker must use the same pepper value.

Token rotation returns a new `rf_worker_` token once, records `rotated_at` and `replaced_by_id`, and caps the old token at the rotation grace period (`TOKEN_ROTATION_GRACE`, default 24h). Update the worker's token file or enrollment token before the grace period ends.

Create a token in the project dashboard or with the API, then start the worker. Remote mode is the default when `DATABASE_URL` is not explicitly set:

```sh
export WORKER_ENROLLMENT_TOKEN=rf_enroll_...
export RELAYFORGE_API_URL=https://relayforge.example.com
make run-worker
```

Worker tokens are application-level auth. In `remote` mode the worker never opens a database connection, so no database credentials are needed on the worker host.

### Worker pools and enrollment

A worker pool is the database record of a desired worker: its project, name, concurrency, active/paused status, and processing policy. Pools are the source of truth for the supervisor and are managed from the project dashboard or API.

| Field | Default | Description |
| --- | --- | --- |
| `name` | required | Pool and worker display name |
| `concurrency` | `4` | Concurrent jobs per worker (1–1000) |
| `queue_names` | `[]` | Queues this pool claims; empty means all queues |
| `job_timeout_seconds` | `0` | Per-job timeout; `0` uses the project default, then `JOB_TIMEOUT` |
| `max_attempts` | `0` | Overrides each claimed job's attempt limit; `0` keeps the job's value |
| `retry_backoff` | `exponential` | `exponential` or `fixed` |
| `retry_base_delay_seconds` | `0` | Base retry delay; `0` uses `RETRY_BASE_DELAY` |
| `retry_max_delay_seconds` | `0` | Retry delay ceiling; `0` uses `RETRY_MAX_DELAY` |
| `allowed_cidrs` | `[]` | IPv4/IPv6 CIDRs allowed to connect; empty allows any address |

Queue selection is enforced in the claim query: a worker only claims jobs whose `queue_name` is in its pool's list. The per-pool retry policy is applied at claim time, so `max_attempts` is written onto the job and retry delays use the pool's backoff settings.

| Method | Endpoint | Description |
| --- | --- | --- |
| `POST` | `/v1/projects/{projectID}/worker-pools` | Create a pool (`name`, optional policy fields) |
| `GET` | `/v1/projects/{projectID}/worker-pools` | List pools |
| `PATCH` | `/v1/projects/{projectID}/worker-pools/{poolID}` | Update name, size, status, queues, timeout, or retry policy |
| `DELETE` | `/v1/projects/{projectID}/worker-pools/{poolID}` | Delete a pool and its tokens |
| `POST` | `/v1/projects/{projectID}/worker-pools/{poolID}/enrollment-tokens` | Issue a single-use enrollment token |
| `POST` | `/v1/workers/enroll` | Redeem an enrollment token (bearer auth, no session) |

Enrollment tokens use the `rf_enroll_` prefix, expire after 15 minutes by default, and are consumed on first use. Redeeming one returns a pool-scoped `rf_worker_` token plus the pool configuration. This lets a worker bootstrap without a long-lived env secret:

```sh
export WORKER_ENROLLMENT_TOKEN=rf_enroll_...
export WORKER_TOKEN_FILE=.relayforge/worker-token.json
export RELAYFORGE_API_URL=http://localhost:8080
make run-worker
```

The worker exchanges the enrollment token, writes the issued token to `WORKER_TOKEN_FILE` with `0600` permissions, and reuses it on restart. Explicit `WORKER_TOKEN` still takes precedence; a cached token file is next; enrollment is the fallback. An invalid cached token is automatically replaced when an enrollment token is also supplied.

Remote workers are the default for supervised pools. The supervisor clears `DATABASE_URL` and sets `WORKER_MODE=remote` on each child process. Direct workers remain available with `WORKER_MODE=direct` and `DATABASE_URL`.

The **supervisor** reads active pools and runs one worker process per pool, restarting workers when policy or configuration changes and stopping them when a pool is paused or deleted:

```sh
make run-supervisor
```

It issues an enrollment token per spawn, sets `WORKER_PROJECT_ID`/`WORKER_NAME`/`WORKER_CONCURRENCY` from the pool, and stores per-pool cached tokens under `SUPERVISOR_TOKEN_DIR`. See [docs/processing-engine.md](docs/processing-engine.md) for the full flow.

### Remote workers and leases

Remote workers talk only to the API:

| Method | Endpoint | Description |
| --- | --- | --- |
| `POST` | `/v1/workers/register` | Register a worker and return the pool/project config |
| `POST` | `/v1/workers/heartbeat` | Update heartbeat and renew execution leases |
| `POST` | `/v1/workers/claim` | Claim one job (`204` when no work is available) |
| `POST` | `/v1/workers/jobs/{jobID}/complete` | Record success with a JSON result |
| `POST` | `/v1/workers/jobs/{jobID}/fail` | Record failure and apply the pool retry policy |
| `POST` | `/v1/workers/offline` | Mark the worker offline during shutdown |

Every claim records a lease of `job_timeout + WORKER_LEASE_GRACE`. Heartbeats renew leases for running jobs. If a worker disappears, the API lease reaper fails the execution with `worker lease expired` and retries the job under the pool policy until attempts are exhausted. Completions and failures are idempotent by execution ID, so a retried HTTP call cannot double-apply. Workers whose last heartbeat is older than `WORKER_STALE_AFTER` are marked offline.

The API also owns the outbox dispatcher (`API_OUTBOX_INTERVAL`) and retention cleanup (`API_CLEANUP_INTERVAL`) so remote workers do not need PostgreSQL.

### Worker visibility and security

| Method | Endpoint | Description |
| --- | --- | --- |
| `GET` | `/v1/projects/{projectID}/workers` | List workers with status, mode, heartbeat, and running jobs |
| `GET` | `/v1/projects/{projectID}/worker-pools/{poolID}/workers` | List workers for one pool |
| `GET` | `/v1/projects/{projectID}/worker-pools/stats` | Online/offline workers, running jobs, queue depth, throughput, success rate |

Pool `allowed_cidrs` are enforced on register, claim, heartbeat, and completion. Set `TRUST_PROXY_HEADERS=true` only behind a trusted reverse proxy so `X-Forwarded-For`/`X-Real-IP` can be used for the client address. Set `WORKER_MTLS_HEADER` to a header injected by a trusted proxy (for example `X-Client-Cert-Fingerprint`) to require a client certificate identity. mTLS itself terminates at the proxy/ingress.

Workers can load pool-local secrets from `WORKER_SECRETS_FILE` (a JSON object of environment variable names to values) and restrict outbound HTTP for handlers that use `execution.NewAllowlistedClient` via `WORKER_EGRESS_ALLOWLIST` (comma-separated hosts, exact or `*.example.com`). Secrets and egress rules stay on the worker host.

API keys use the `rf_live_` prefix and are returned only once at creation. Keys are hashed at rest with HMAC-SHA256 using `API_KEY_PEPPER`, and the dashboard shows only a short `key_prefix`. Key creation accepts an optional `expires_in_days` (1–3650); omitted keys never expire. `last_used_at` is updated at most once per minute during authentication.

Set `API_KEY_PEPPER` to a long random value. A development fallback is used when `APP_ENV` is not `production`; production refuses to start without it. Rotating the pepper invalidates existing keys because their stored hashes can no longer be reproduced.

### Jobs

| Method | Endpoint | Authentication | Description |
| --- | --- | --- | --- |
| `POST` | `/v1/jobs` | API key | Submit a job |
| `GET` | `/v1/jobs/{id}` | API key | Retrieve a job |
| `POST` | `/v1/projects/{projectID}/jobs` | Session | Submit a job from the dashboard |
| `GET` | `/v1/projects/{projectID}/jobs` | Session | List project jobs |
| `GET` | `/v1/projects/{projectID}/jobs/{jobID}` | Session | Retrieve a project job |
| `GET` | `/v1/projects/{projectID}/jobs/{jobID}/executions` | Session | List execution attempts |

```json
{
  "queue_name": "default",
  "job_type": "send_email",
  "payload": {"user_id": "123"},
  "priority": 10,
  "max_attempts": 3,
  "scheduled_at": "2026-11-05T20:00:00Z"
}
```

`queue_name`, `payload`, `priority`, `max_attempts`, and `scheduled_at` are optional. When `queue_name`, `priority`, or `max_attempts` are omitted, the project's defaults are used. Payloads must be valid, non-null JSON and at most 256 KiB.

Job submission is idempotent when the request includes an `Idempotency-Key` header (1–255 printable characters). Replaying the same key and request returns the original job with `200 OK` and `Idempotent-Replayed: true`; reusing the key with a different request returns `409 Conflict`. Keys are scoped per project and follow the job retention window: once the original job is purged, its idempotency key can be reused. Jobs are created with the outbox message in one transaction. The worker dispatches pending outbox messages and emits a `relayforge_jobs` PostgreSQL notification containing the job ID. See [docs/job-lifecycle.md](docs/job-lifecycle.md) for statuses and transitions.

### Rate limits

The API rate-limits requests with in-memory fixed one-minute windows and returns `429 Too Many Requests` with `Retry-After` and `X-RateLimit-*` headers when a limit is exceeded.

| Variable | Default | Scope |
| --- | --- | --- |
| `RATE_LIMIT_AUTH_PER_MINUTE` | `20` | Login/registration, keyed by client IP |
| `RATE_LIMIT_API_PER_MINUTE` | `600` | General authenticated API, keyed by user or API key |
| `RATE_LIMIT_JOB_SUBMIT_PER_MINUTE` | `120` | Job submission, keyed by user or API key |

Set a limit to `0` to disable it. These counters are per API process; running multiple API instances requires a shared limiter for global enforcement.

### Processing engine

The worker resolves credentials in this order: explicit `WORKER_TOKEN`, a cached token file at `WORKER_TOKEN_FILE`, then `WORKER_ENROLLMENT_TOKEN` (which enrolls against `RELAYFORGE_API_URL`). If the token belongs to a worker pool, the pool's concurrency, name, queues, timeout, and retry policy override the env defaults. `WORKER_MODE` selects `direct` (PostgreSQL) or `remote` (API only); when unset, an explicit `DATABASE_URL` selects direct and otherwise remote is used.

```sh
export WORKER_PROJECT_ID=<project-uuid>
export WORKER_TOKEN=rf_worker_...
make run-worker
```

The worker:

- Dispatches outbox messages
- Claims jobs with `FOR UPDATE SKIP LOCKED`
- Runs jobs concurrently with a bounded worker pool
- Records every attempt in `executions`
- Applies context timeouts to handlers
- Retries failures with exponential backoff
- Applies project concurrency, rate-limit, and job-timeout settings
- Deletes terminal jobs beyond the project retention window
- Drains in-flight work during graceful shutdown
- Sends worker heartbeats and marks itself offline on exit

The development handlers are `echo`, `noop`, and `sleep`. Register production handlers in `cmd/worker/main.go`.

With Docker Compose, start a standalone worker with the `worker` profile, or let the supervisor manage all active pools with the `supervisor` profile:

```sh
docker compose --profile worker up -d
docker compose --profile supervisor up -d
```

See [docs/processing-engine.md](docs/processing-engine.md) for configuration and behavior details.

## Developer dashboard

The dashboard in `web/` is built with React, Vite, Tailwind CSS, and shadcn/ui.

- Registration and login screens
- Project creation, rename, and archive flows
- API key creation and revocation
- Job submission with JSON validation
- Job inspection with execution history
- Automatic two-second refresh for active jobs

See [web/README.md](web/README.md) for development instructions.

## PostgreSQL

PostgreSQL runs through Docker Compose using the `postgres:17-alpine` image.

The application reads `DATABASE_URL` and defaults to a local connection:

```text
postgres://relayforge:relayforge@localhost:5435/relayforge?sslmode=disable
```

Inside Docker Compose, the API uses `postgres:5432` as its database host. The host port defaults to `5435` to avoid colliding with a system PostgreSQL instance; change `POSTGRES_PORT` and `DATABASE_URL` together if needed.

```sh
cp .env.example .env
docker compose up -d postgres
```

### Migrations

Migrations are managed with [golang-migrate](https://github.com/golang-migrate/migrate).

Install the CLI:

```sh
make migrate-install
```

Apply all pending migrations:

```sh
make migrate-up
```

Roll back the most recent migration:

```sh
make migrate-down
```

Show the current migration version:

```sh
make migrate-status
```

Create a new sequential migration pair:

```sh
make migrate-create name=create_something
```

The Makefile reads `DATABASE_URL` from `.env` when present. The default is the local Docker Compose database.

### Query generation

Database queries are defined in `sql/queries` and compiled with [sqlc](https://sqlc.dev). The schema is read from the `migrations` directory, and generated Go code is written to `internal/database/db`.

```sh
make sqlc-install
make sqlc-generate
```

Stop the database:

```sh
docker compose down
```

To also delete the local database volume:

```sh
docker compose down -v
```

The credentials in `.env.example` are for local development only.
