-- name: CreateWorkerToken :one
INSERT INTO worker_tokens (project_id, token_hash, token_prefix, name, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetWorkerTokenByHash :one
SELECT * FROM worker_tokens
WHERE token_hash = $1
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > now());

-- name: ListWorkerTokensByProject :many
SELECT * FROM worker_tokens
WHERE project_id = $1
ORDER BY created_at DESC;

-- name: RevokeWorkerToken :one
UPDATE worker_tokens
SET revoked_at = now()
WHERE id = $1 AND project_id = $2 AND revoked_at IS NULL
RETURNING *;

-- name: TouchWorkerToken :exec
UPDATE worker_tokens
SET last_used_at = now()
WHERE id = $1
  AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute');
