# Processing Engine

The worker is a project-scoped processing engine backed by PostgreSQL.

## Worker authentication

A worker resolves its identity in this order:

1. `WORKER_TOKEN` with `WORKER_PROJECT_ID` (long-lived explicit token).
2. `WORKER_TOKEN_FILE` (token cached by a previous enrollment).
3. `WORKER_ENROLLMENT_TOKEN`, exchanged at `POST /v1/workers/enroll` for a pool-scoped token, then cached to `WORKER_TOKEN_FILE`.

```sh
export WORKER_ENROLLMENT_TOKEN=rf_enroll_...
export WORKER_TOKEN_FILE=.relayforge/worker-token.json
export RELAYFORGE_API_URL=http://localhost:8080
```

Tokens are stored as HMAC-SHA256 hashes and may expire. An authenticated token must belong to the worker's project; if it belongs to a pool, the pool must be active. Successful startup records the token and pool on the `workers` row and updates `last_used_at`.

Enrollment tokens use the `rf_enroll_` prefix, are single-use, and expire after 15 minutes by default. The raw worker token is written to the token file with `0600` permissions.

`WORKER_MODE` selects the transport:

| Mode | Requires | Talks to |
| --- | --- | --- |
| `direct` | `DATABASE_URL` | PostgreSQL for claims, completions, outbox, and cleanup |
| `remote` | `RELAYFORGE_API_URL` + worker token | The API only; no database connection |

When `WORKER_MODE` is unset, an explicit `DATABASE_URL` selects `direct`; otherwise `remote` is used. Supervised workers run in `remote` mode. Application-level token authorization still applies in both modes; direct mode additionally needs a database role.

## Worker pools and supervisor

`worker_pools` stores the desired worker configuration per project: name, concurrency, `active`/`paused` status, queue selection, job timeout, and retry policy. Workers are not manually indexed against pools; the supervisor is the reconciler.

| Field | Meaning |
| --- | --- |
| `queue_names` | Queues this pool may claim; empty means all queues |
| `job_timeout_seconds` | Per-job timeout; `0` falls back to project default then `JOB_TIMEOUT` |
| `max_attempts` | Claim-time override of the job's attempt limit; `0` keeps the job value |
| `retry_backoff` | `fixed` or `exponential` |
| `retry_base_delay_seconds` | Base delay; `0` falls back to `RETRY_BASE_DELAY` |
| `retry_max_delay_seconds` | Delay ceiling; `0` falls back to `RETRY_MAX_DELAY` |
| `allowed_cidrs` | Client address ranges allowed to use the pool; empty allows any |

```text
worker_pools (active)
       |
       | reconcile every SUPERVISOR_POLL_INTERVAL
       v
issue rf_enroll_ token --> spawn relayforge-worker
                              |
                              | POST /v1/workers/enroll
                              v
                       pool-scoped rf_worker_ token
                              |
                              | cache to SUPERVISOR_TOKEN_DIR/pool-<id>.json
                              v
                  WORKER_MODE=remote, no DATABASE_URL
                              |
                              v
                  POST /v1/workers/register + process jobs
```

The supervisor:

- Starts one worker process per active pool and passes `WORKER_PROJECT_ID`, `WORKER_NAME`, and `WORKER_CONCURRENCY` from the pool row.
- Clears `DATABASE_URL` and sets `WORKER_MODE=remote` on the child.
- Restarts a worker when the pool name, concurrency, queues, timeout, or retry policy changes.
- Stops the worker when the pool is paused or deleted.
- Reissues an enrollment token on every spawn; cached tokens are reused when still valid.
- Shuts down children with `SIGTERM` and waits up to 60 seconds before killing them.

Set `SUPERVISOR_WORKER_BINARY` to the worker binary path and `SUPERVISOR_TOKEN_DIR` to a writable persistent directory. With Docker Compose:

```sh
docker compose --profile supervisor up -d
```

## Remote worker protocol

Remote workers authenticate every request with their `rf_worker_` token and never open a database connection.

| Endpoint | Purpose |
| --- | --- |
| `POST /v1/workers/register` | Create a `workers` row and return the pool policy and project limits |
| `POST /v1/workers/heartbeat` | Update `last_heartbeat_at` and renew leases for running jobs |
| `POST /v1/workers/claim` | Claim one job for the token's project/pool (`204` when idle) |
| `POST /v1/workers/jobs/{jobID}/complete` | Finish an execution successfully |
| `POST /v1/workers/jobs/{jobID}/fail` | Finish an execution as failed and apply retry policy |
| `POST /v1/workers/offline` | Mark the worker offline on shutdown |

Each claim creates the execution with `lease_expires_at = now() + job_timeout + WORKER_LEASE_GRACE`. Heartbeats extend leases for all running executions owned by that worker. The API lease reaper (`API_REAPER_INTERVAL`) fails executions whose lease elapsed with `worker lease expired`, then retries the job under the pool policy. Completions and failures are idempotent by execution ID.

The API also runs the outbox dispatcher and retention cleanup, so remote workers do not need the outbox or cleanup loops. Workers whose heartbeat is older than `WORKER_STALE_AFTER` are marked offline by the worker sweep.

## Visibility

`GET /v1/projects/{projectID}/workers` lists worker status, mode, last heartbeat, and running job count. `GET /v1/projects/{projectID}/worker-pools/stats` returns online/offline workers, running jobs, queue depth, jobs/min, success rate, and average duration per pool. The dashboard shows both.

## Security

- `worker_pools.allowed_cidrs` rejects requests from other addresses on register, claim, heartbeat, and completion.
- `TRUST_PROXY_HEADERS=true` enables `X-Forwarded-For`/`X-Real-IP`; only use it behind a trusted proxy.
- `WORKER_MTLS_HEADER` requires a non-empty identity header injected by a trusted proxy after mTLS termination.
- `WORKER_SECRETS_FILE` loads a local JSON map of environment variables.
- `WORKER_EGRESS_ALLOWLIST` restricts handlers that use `execution.NewAllowlistedClient` to exact hosts or `*.example.com` wildcards.
- Token rotation issues a replacement and caps the old token at `TOKEN_ROTATION_GRACE` (default 24h).

## Processing flow

```text
outbox dispatcher
      |
      v
pending/scheduled job
      |
      | ClaimJob: FOR UPDATE SKIP LOCKED
      v
running job + running execution
      |
      | handler with timeout
      v
succeeded job           failed attempt
                        /            \
             retries left          no retries left
                  |                     |
                  v                     v
             scheduled job           failed job
```

Every claim increments `jobs.attempts` and creates an `executions` row in the same transaction. Finishing an attempt updates both the execution and job in the same transaction.

## Concurrency

The pool starts `WORKER_CONCURRENCY` goroutines. Each goroutine claims one job at a time. PostgreSQL row locking ensures multiple workers and multiple processes never claim the same job.

A job is claimable when:

- Its project matches the worker's project
- Its status is `pending` or `scheduled`
- `scheduled_at <= now()`
- `attempts <` the pool's `max_attempts` override when set, otherwise the job's `max_attempts`
- Its `queue_name` is in the pool's `queue_names` (an empty list means all queues)
- Its outbox message has been processed

The pool's job timeout controls the handler deadline. On failure, the pool's `retry_backoff` and delay settings decide when the next attempt is scheduled; the claim updates `jobs.max_attempts` to the pool override so the job record reflects the effective policy.

## Project limits

The worker reads these project settings at startup:

- `max_concurrency` caps `WORKER_CONCURRENCY` when greater than zero.
- `rate_limit_per_minute` limits how many job claims may start in each one-minute window. `0` is unlimited.
- `default_job_timeout_seconds` overrides `JOB_TIMEOUT` for this project when greater than zero, unless the worker pool sets its own `job_timeout_seconds`.

Restart the worker after changing these settings. Job submission defaults (`default_queue_name`, `default_priority`, `default_max_attempts`) are applied by the API and need no restart.

## Retention

The API runs retention cleanup every `API_CLEANUP_INTERVAL` using each project's `job_retention_days`; direct workers also run it every `WORKER_CLEANUP_INTERVAL`. When the value is greater than zero, terminal jobs (`succeeded`, `failed`, `cancelled`) older than the window are deleted, cascading to their executions and outbox messages. `0` disables cleanup.

## Timeouts

Each handler runs with a context deadline of `JOB_TIMEOUT`. A handler that exceeds the deadline is recorded as a failed attempt with a timeout error. The job is retried when attempts remain.

Handlers must respect `ctx.Done()` to release resources promptly.

## Retries and backoff

Failed attempts are rescheduled with exponential backoff:

```text
delay = RETRY_BASE_DELAY * 2^(attempt - 1)
```

The delay is capped at `RETRY_MAX_DELAY`. When `attempts >= max_attempts`, the job becomes terminal `failed`.

## Cancellation and shutdown

On `SIGINT` or `SIGTERM`:

1. The pool stops claiming new jobs.
2. In-flight jobs continue running for up to `WORKER_SHUTDOWN_TIMEOUT`.
3. If the timeout is reached, handler contexts are cancelled.
4. Cancelled attempts are recorded as `cancelled` executions.
5. Jobs with retries remaining are scheduled again; otherwise they become `failed`.
6. The worker row is marked `offline`.

The outbox dispatcher and heartbeat stop with the worker.

## Handlers

Handlers implement:

```go
func(ctx context.Context, job db.Job) (json.RawMessage, error)
```

Register handlers in `cmd/worker/main.go`:

```go
registry.Register("send_email", sendEmailHandler)
```

The built-in `echo`, `noop`, and `sleep` handlers exist for development and testing.

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `WORKER_MODE` | auto | `direct`, `remote`, or auto by `DATABASE_URL` |
| `WORKER_LEASE_GRACE` | `60s` | Extra time beyond the job timeout before a lease expires |
| `WORKER_PROJECT_ID` | required with `WORKER_TOKEN` in direct mode | Project UUID claimed by this worker |
| `WORKER_TOKEN` | — | Explicit long-lived worker token |
| `WORKER_TOKEN_FILE` | `.relayforge/worker-token.json` | Cached token written after enrollment |
| `WORKER_ENROLLMENT_TOKEN` | — | Single-use `rf_enroll_` bootstrap token |
| `RELAYFORGE_API_URL` | `http://localhost:8080` | API used for enrollment and remote processing |
| `WORKER_ENROLLMENT_TTL` | `15m` | Enrollment token lifetime (1m–24h) |
| `WORKER_NAME` | generated | Worker display name (pool name overrides) |
| `WORKER_CONCURRENCY` | `4` | Number of concurrent job workers (pool overrides) |
| `WORKER_POLL_INTERVAL` | `1s` | Delay when no jobs are available |
| `JOB_TIMEOUT` | `5m` | Maximum handler execution time |
| `WORKER_SHUTDOWN_TIMEOUT` | `30s` | Grace period for in-flight jobs |
| `WORKER_HEARTBEAT_INTERVAL` | `15s` | Worker heartbeat frequency |
| `WORKER_CLEANUP_INTERVAL` | `1h` | How often expired jobs are purged |
| `RETRY_BASE_DELAY` | `5s` | Initial retry delay |
| `RETRY_MAX_DELAY` | `5m` | Maximum retry delay |
| `WORKER_SECRETS_FILE` | — | JSON map of local environment secrets |
| `WORKER_EGRESS_ALLOWLIST` | — | Comma-separated outbound host allowlist |
| `WORKER_MTLS_HEADER` | — | Required proxy header proving client-certificate identity |

Supervisor variables:

| Variable | Default | Description |
| --- | --- | --- |
| `SUPERVISOR_POLL_INTERVAL` | `5s` | How often active pools are reconciled |
| `SUPERVISOR_WORKER_BINARY` | `relayforge-worker` | Worker executable to spawn |
| `SUPERVISOR_TOKEN_DIR` | `.relayforge/worker-tokens` | Per-pool cached token directory |

API background and remote-worker variables:

| Variable | Default | Description |
| --- | --- | --- |
| `TOKEN_ROTATION_GRACE` | `24h` | How long an old token remains valid after rotation |
| `API_OUTBOX_INTERVAL` | `1s` | Outbox dispatcher cadence |
| `API_CLEANUP_INTERVAL` | `1h` | Retention cleanup cadence |
| `API_REAPER_INTERVAL` | `15s` | Expired-lease reaper cadence |
| `API_WORKER_SWEEP_INTERVAL` | `30s` | Stale-worker sweep cadence |
| `WORKER_STALE_AFTER` | `90s` | Heartbeat age that marks a worker offline |
| `TRUST_PROXY_HEADERS` | `false` | Trust forwarded IP headers from a proxy |
