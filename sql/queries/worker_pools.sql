-- name: CreateWorkerPool :one
INSERT INTO worker_pools (
    project_id,
    name,
    concurrency,
    status,
    queue_names,
    job_timeout_seconds,
    max_attempts,
    retry_backoff,
    retry_base_delay_seconds,
    retry_max_delay_seconds,
    allowed_cidrs
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetWorkerPool :one
SELECT * FROM worker_pools
WHERE id = $1 AND project_id = $2;

-- name: ListWorkerPoolsByProject :many
SELECT * FROM worker_pools
WHERE project_id = $1
ORDER BY created_at DESC;

-- name: ListActiveWorkerPools :many
SELECT worker_pools.* FROM worker_pools
JOIN projects ON projects.id = worker_pools.project_id
WHERE worker_pools.status = 'active'
  AND projects.archived_at IS NULL
ORDER BY worker_pools.created_at ASC;

-- name: UpdateWorkerPool :one
UPDATE worker_pools
SET name = COALESCE(sqlc.narg('name'), name),
    concurrency = COALESCE(sqlc.narg('concurrency'), concurrency),
    status = COALESCE(sqlc.narg('status'), status),
    queue_names = COALESCE(sqlc.narg('queue_names')::text[], queue_names),
    job_timeout_seconds = COALESCE(sqlc.narg('job_timeout_seconds'), job_timeout_seconds),
    max_attempts = COALESCE(sqlc.narg('max_attempts'), max_attempts),
    retry_backoff = COALESCE(sqlc.narg('retry_backoff'), retry_backoff),
    retry_base_delay_seconds = COALESCE(sqlc.narg('retry_base_delay_seconds'), retry_base_delay_seconds),
    retry_max_delay_seconds = COALESCE(sqlc.narg('retry_max_delay_seconds'), retry_max_delay_seconds),
    allowed_cidrs = COALESCE(sqlc.narg('allowed_cidrs')::text[], allowed_cidrs),
    updated_at = now()
WHERE id = $1 AND project_id = $2
RETURNING *;

-- name: DeleteWorkerPool :execrows
DELETE FROM worker_pools
WHERE id = $1 AND project_id = $2;

-- name: CountWorkerPoolsByProject :one
SELECT count(*) FROM worker_pools
WHERE project_id = $1;

-- name: CountActiveWorkerPoolsByProject :one
SELECT count(*) FROM worker_pools
WHERE project_id = $1 AND status = 'active';

-- name: CountActiveWorkerPoolsByProjectExcluding :one
SELECT count(*) FROM worker_pools
WHERE project_id = $1 AND status = 'active' AND id <> $2;

-- name: SumWorkerConcurrencyByProject :one
SELECT COALESCE(SUM(concurrency), 0)::bigint FROM worker_pools
WHERE project_id = $1 AND status = 'active';

-- name: SumWorkerConcurrencyByProjectExcluding :one
SELECT COALESCE(SUM(concurrency), 0)::bigint FROM worker_pools
WHERE project_id = $1 AND status = 'active' AND id <> $2;
