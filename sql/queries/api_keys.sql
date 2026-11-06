-- name: CreateAPIKey :one
INSERT INTO api_keys (project_id, key_hash, key_prefix, name, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetAPIKeyByHash :one
SELECT api_keys.* FROM api_keys
JOIN projects ON projects.id = api_keys.project_id
WHERE api_keys.key_hash = $1
  AND api_keys.revoked_at IS NULL
  AND projects.archived_at IS NULL
  AND (api_keys.expires_at IS NULL OR api_keys.expires_at > now());

-- name: ListAPIKeysByProject :many
SELECT * FROM api_keys
WHERE project_id = $1
ORDER BY created_at DESC;

-- name: RevokeAPIKey :one
UPDATE api_keys
SET revoked_at = now()
WHERE id = $1 AND project_id = $2 AND revoked_at IS NULL
RETURNING *;

-- name: UpdateAPIKeyHash :exec
UPDATE api_keys
SET key_hash = $2
WHERE id = $1;

-- name: TouchAPIKey :exec
UPDATE api_keys
SET last_used_at = now()
WHERE id = $1
  AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute');
