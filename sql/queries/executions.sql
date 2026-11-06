-- name: CreateExecution :one
INSERT INTO executions (job_id, worker_id, attempt_number, status, lease_expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetExecution :one
SELECT * FROM executions WHERE id = $1;

-- name: GetExecutionForWorker :one
SELECT * FROM executions
WHERE id = $1 AND worker_id = $2;

-- name: ListExecutionsByJob :many
SELECT * FROM executions
WHERE job_id = $1
ORDER BY attempt_number;

-- name: FinishExecution :one
UPDATE executions
SET
    status = $2,
    finished_at = now(),
    error_message = $3,
    result = $4
WHERE id = $1 AND status = 'running'
RETURNING *;

-- name: ListExpiredExecutions :many
SELECT * FROM executions
WHERE status = 'running'
  AND lease_expires_at IS NOT NULL
  AND lease_expires_at < now()
ORDER BY lease_expires_at
LIMIT $1;

-- name: RenewWorkerLeases :execrows
UPDATE executions
SET lease_expires_at = now() + make_interval(secs => sqlc.arg('lease_seconds')::int)
WHERE worker_id = sqlc.arg('worker_id')
  AND status = 'running';

-- name: ListWorkersWithLoad :many
SELECT
    workers.*,
    (
        SELECT count(*)::int
        FROM executions
        WHERE executions.worker_id = workers.id
          AND executions.status = 'running'
    ) AS running_jobs

FROM workers
WHERE workers.project_id = sqlc.arg('project_id')
  AND (sqlc.narg('pool_id')::uuid IS NULL OR workers.worker_pool_id = sqlc.narg('pool_id')::uuid)
ORDER BY workers.created_at DESC;

-- name: GetWorkerPoolStats :many
SELECT
    worker_pools.id AS pool_id,
    (
        SELECT count(*)::int
        FROM workers
        WHERE workers.worker_pool_id = worker_pools.id
          AND workers.status = 'online'
    ) AS online_workers,
    (
        SELECT count(*)::int
        FROM workers
        WHERE workers.worker_pool_id = worker_pools.id
          AND workers.status = 'offline'
    ) AS offline_workers,
    (
        SELECT count(*)::int
        FROM executions
        JOIN workers ON workers.id = executions.worker_id
        WHERE workers.worker_pool_id = worker_pools.id
          AND executions.status = 'running'
    ) AS running_jobs,
    (
        SELECT count(*)::int
        FROM jobs
        WHERE jobs.project_id = worker_pools.project_id
          AND jobs.status IN ('pending', 'scheduled')
          AND (
              cardinality(worker_pools.queue_names) = 0
              OR jobs.queue_name = ANY(worker_pools.queue_names)
          )
    ) AS queue_depth,
    (
        SELECT count(*)::int
        FROM jobs
        WHERE jobs.project_id = worker_pools.project_id
          AND jobs.status = 'succeeded'
          AND jobs.updated_at > now() - interval '1 hour'
          AND (
              cardinality(worker_pools.queue_names) = 0
              OR jobs.queue_name = ANY(worker_pools.queue_names)
          )
    ) AS succeeded_last_hour,
    (
        SELECT count(*)::int
        FROM jobs
        WHERE jobs.project_id = worker_pools.project_id
          AND jobs.status = 'failed'
          AND jobs.updated_at > now() - interval '1 hour'
          AND (
              cardinality(worker_pools.queue_names) = 0
              OR jobs.queue_name = ANY(worker_pools.queue_names)
          )
    ) AS failed_last_hour,
    (
        SELECT COALESCE(avg(extract(epoch FROM (executions.finished_at - executions.started_at))), 0)::float8
        FROM executions
        JOIN jobs ON jobs.id = executions.job_id
        WHERE jobs.project_id = worker_pools.project_id
          AND executions.status = 'succeeded'
          AND executions.finished_at > now() - interval '1 hour'
          AND (
              cardinality(worker_pools.queue_names) = 0
              OR jobs.queue_name = ANY(worker_pools.queue_names)
          )
    ) AS avg_duration_seconds
FROM worker_pools
WHERE worker_pools.project_id = $1
ORDER BY worker_pools.created_at ASC;
