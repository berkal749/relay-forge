# Processing Engine

The worker is a project-scoped processing engine backed by PostgreSQL.

## Worker authentication

Each worker must present a project worker token at startup:

```sh
export WORKER_PROJECT_ID=<project-uuid>
export WORKER_TOKEN=rf_worker_...
```

The token is created through the dashboard or `POST /v1/projects/{projectID}/worker-tokens`. It is stored as an HMAC-SHA256 hash, may expire, and must belong to `WORKER_PROJECT_ID`. Successful startup records the token on the `workers` row and updates the token's `last_used_at`.

This is application-level authorization. Workers still connect directly to PostgreSQL with `DATABASE_URL`; use per-worker database roles or network controls when a hard boundary is required.

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
- `attempts < max_attempts`
- Its outbox message has been processed

## Project limits

The worker reads these project settings at startup:

- `max_concurrency` caps `WORKER_CONCURRENCY` when greater than zero.
- `rate_limit_per_minute` limits how many job claims may start in each one-minute window. `0` is unlimited.
- `default_job_timeout_seconds` overrides `JOB_TIMEOUT` for this project when greater than zero.

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
| `WORKER_PROJECT_ID` | required | Project UUID claimed by this worker |
| `WORKER_NAME` | generated | Worker display name |
| `WORKER_CONCURRENCY` | `4` | Number of concurrent job workers |
| `WORKER_POLL_INTERVAL` | `1s` | Delay when no jobs are available |
| `JOB_TIMEOUT` | `5m` | Maximum handler execution time |
| `WORKER_SHUTDOWN_TIMEOUT` | `30s` | Grace period for in-flight jobs |
| `WORKER_HEARTBEAT_INTERVAL` | `15s` | Worker heartbeat frequency |
| `WORKER_CLEANUP_INTERVAL` | `1h` | How often expired jobs are purged |
| `RETRY_BASE_DELAY` | `5s` | Initial retry delay |
| `RETRY_MAX_DELAY` | `5m` | Maximum retry delay |
