-- name: CreateWorker :one
INSERT INTO workers (project_id, worker_token_id, name, status)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetWorker :one
SELECT * FROM workers WHERE id = $1;

-- name: ListWorkersByProject :many
SELECT * FROM workers
WHERE project_id = $1
ORDER BY created_at DESC;

-- name: UpdateWorkerHeartbeat :one
UPDATE workers
SET status = 'online', last_heartbeat_at = now()
WHERE id = $1
RETURNING *;

-- name: MarkWorkerOffline :exec
UPDATE workers
SET status = 'offline'
WHERE id = $1;
