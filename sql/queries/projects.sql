-- name: CreateProject :one
INSERT INTO projects (user_id, name)
VALUES ($1, $2)
RETURNING *;

-- name: GetProject :one
SELECT * FROM projects
WHERE id = $1 AND archived_at IS NULL;

-- name: ListProjectsByUser :many
SELECT * FROM projects
WHERE user_id = $1 AND archived_at IS NULL
ORDER BY created_at DESC;

-- name: UpdateProject :one
UPDATE projects
SET
    name = COALESCE(sqlc.narg('name'), name),
    default_queue_name = COALESCE(sqlc.narg('default_queue_name'), default_queue_name),
    default_max_attempts = COALESCE(sqlc.narg('default_max_attempts'), default_max_attempts),
    default_priority = COALESCE(sqlc.narg('default_priority'), default_priority),
    job_retention_days = COALESCE(sqlc.narg('job_retention_days'), job_retention_days),
    max_concurrency = COALESCE(sqlc.narg('max_concurrency'), max_concurrency),
    rate_limit_per_minute = COALESCE(sqlc.narg('rate_limit_per_minute'), rate_limit_per_minute),
    default_job_timeout_seconds = COALESCE(sqlc.narg('default_job_timeout_seconds'), default_job_timeout_seconds)
WHERE id = sqlc.arg('id') AND user_id = sqlc.arg('user_id') AND archived_at IS NULL
RETURNING *;

-- name: ArchiveProject :one
UPDATE projects
SET archived_at = now()
WHERE id = $1 AND user_id = $2 AND archived_at IS NULL
RETURNING *;

-- name: DeleteProject :exec
DELETE FROM projects WHERE id = $1;
