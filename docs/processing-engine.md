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

This is application-level authorization. Workers still connect directly to PostgreSQL with `DATABASE_URL`; use per-worker database roles or network controls when a hard boundary is required.

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
                       register + process jobs
```

The supervisor:

- Starts one worker process per active pool and passes `WORKER_PROJECT_ID`, `WORKER_NAME`, and `WORKER_CONCURRENCY` from the pool row.
- Restarts a worker when the pool name, concurrency, queues, timeout, or retry policy changes.
- Stops the worker when the pool is paused or deleted.
- Reissues an enrollment token on every spawn; cached tokens are reused when still valid.
- Shuts down children with `SIGTERM` and waits up to 60 seconds before killing them.

Set `SUPERVISOR_WORKER_BINARY` to the worker binary path and `SUPERVISOR_TOKEN_DIR` to a writable persistent directory. With Docker Compose:

```sh
docker compose --profile supervisor up -d
```

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

On every `WORKER_CLEANUP_INTERVAL`, the worker re-reads `job_retention_days`. When it is greater than zero, terminal jobs (`succeeded`, `failed`, `cancelled`) older than the window are deleted, cascading to their executions and outbox messages. `0` disables cleanup.

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
| `WORKER_PROJECT_ID` | required with `WORKER_TOKEN` | Project UUID claimed by this worker |
| `WORKER_TOKEN` | — | Explicit long-lived worker token |
| `WORKER_TOKEN_FILE` | `.relayforge/worker-token.json` | Cached token written after enrollment |
| `WORKER_ENROLLMENT_TOKEN` | — | Single-use `rf_enroll_` bootstrap token |
| `RELAYFORGE_API_URL` | `http://localhost:8080` | API used for enrollment |
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

Supervisor variables:

| Variable | Default | Description |
| --- | --- | --- |
| `SUPERVISOR_POLL_INTERVAL` | `5s` | How often active pools are reconciled |
| `SUPERVISOR_WORKER_BINARY` | `relayforge-worker` | Worker executable to spawn |
| `SUPERVISOR_TOKEN_DIR` | `.relayforge/worker-tokens` | Per-pool cached token directory |
