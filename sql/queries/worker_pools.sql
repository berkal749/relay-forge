-- name: CreateWorkerPool :one
INSERT INTO worker_pools (project_id, name, concurrency, status)
VALUES ($1, $2, $3, $4)
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
    updated_at = now()
WHERE id = $1 AND project_id = $2
RETURNING *;

-- name: DeleteWorkerPool :execrows
DELETE FROM worker_pools
WHERE id = $1 AND project_id = $2;
