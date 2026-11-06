-- name: CreateWorkerToken :one
INSERT INTO worker_tokens (project_id, pool_id, kind, token_hash, token_prefix, name, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetWorkerTokenByHash :one
SELECT worker_tokens.* FROM worker_tokens
JOIN projects ON projects.id = worker_tokens.project_id
LEFT JOIN worker_pools ON worker_pools.id = worker_tokens.pool_id
WHERE worker_tokens.token_hash = $1
  AND worker_tokens.kind = 'worker'
  AND worker_tokens.revoked_at IS NULL
  AND (worker_tokens.expires_at IS NULL OR worker_tokens.expires_at > now())
  AND projects.archived_at IS NULL
  AND (worker_pools.id IS NULL OR worker_pools.status = 'active');

-- name: GetWorkerTokenForProject :one
SELECT * FROM worker_tokens
WHERE id = $1 AND project_id = $2 AND kind = 'worker';

-- name: GetEnrollmentTokenByHash :one
SELECT worker_tokens.* FROM worker_tokens
JOIN worker_pools ON worker_pools.id = worker_tokens.pool_id
JOIN projects ON projects.id = worker_tokens.project_id
WHERE worker_tokens.token_hash = $1
  AND worker_tokens.kind = 'enrollment'
  AND worker_tokens.revoked_at IS NULL
  AND worker_tokens.consumed_at IS NULL
  AND (worker_tokens.expires_at IS NULL OR worker_tokens.expires_at > now())
  AND worker_pools.status = 'active'
  AND projects.archived_at IS NULL;

-- name: ConsumeEnrollmentToken :one
UPDATE worker_tokens
SET consumed_at = now()
WHERE id = $1
  AND kind = 'enrollment'
  AND consumed_at IS NULL
RETURNING *;

-- name: ListWorkerTokensByProject :many
SELECT * FROM worker_tokens
WHERE project_id = $1
  AND kind = 'worker'
ORDER BY created_at DESC;

-- name: RevokeWorkerToken :one
UPDATE worker_tokens
SET revoked_at = now()
WHERE id = $1 AND project_id = $2 AND revoked_at IS NULL
RETURNING *;

-- name: RotateWorkerToken :one
UPDATE worker_tokens
SET rotated_at = now(),
    replaced_by_id = sqlc.arg('replaced_by_id'),
    expires_at = LEAST(
        COALESCE(expires_at, now() + make_interval(secs => sqlc.arg('grace_seconds')::int)),
        now() + make_interval(secs => sqlc.arg('grace_seconds')::int)
    )
WHERE id = sqlc.arg('id')
  AND project_id = sqlc.arg('project_id')
  AND kind = 'worker'
  AND revoked_at IS NULL
RETURNING *;

-- name: TouchWorkerToken :exec
UPDATE worker_tokens
SET last_used_at = now()
WHERE id = $1
  AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute');
