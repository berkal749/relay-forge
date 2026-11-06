-- name: CreateWorker :one
INSERT INTO workers (project_id, worker_token_id, worker_pool_id, name, status, mode)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetWorker :one
SELECT * FROM workers WHERE id = $1;

-- name: GetWorkerForProject :one
SELECT * FROM workers WHERE id = $1 AND project_id = $2;

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

-- name: MarkTokenWorkersOffline :exec
UPDATE workers
SET status = 'offline'
WHERE worker_token_id = $1 AND status = 'online';

-- name: MarkStaleWorkersOffline :execrows
UPDATE workers
SET status = 'offline'
WHERE status = 'online'
  AND (
      last_heartbeat_at IS NULL
      OR last_heartbeat_at < now() - make_interval(secs => sqlc.arg('stale_seconds')::int)
  );

-- name: OnlineWorkersByProject :many
SELECT project_id, count(*) AS online FROM workers
WHERE status = 'online'
GROUP BY project_id;
