-- name: CreateSchedule :one
INSERT INTO schedules (
    project_id,
    name,
    cron,
    timezone,
    enabled,
    job_type,
    queue_name,
    payload,
    priority,
    max_attempts,
    next_run_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetSchedule :one
SELECT * FROM schedules
WHERE id = $1 AND project_id = $2;

-- name: ListSchedulesByProject :many
SELECT * FROM schedules
WHERE project_id = $1
ORDER BY created_at DESC;

-- name: CountSchedulesByProject :one
SELECT count(*) FROM schedules
WHERE project_id = $1;

-- name: UpdateSchedule :one
UPDATE schedules
SET name = COALESCE(sqlc.narg('name'), name),
    cron = COALESCE(sqlc.narg('cron'), cron),
    timezone = COALESCE(sqlc.narg('timezone'), timezone),
    enabled = COALESCE(sqlc.narg('enabled'), enabled),
    job_type = COALESCE(sqlc.narg('job_type'), job_type),
    queue_name = COALESCE(sqlc.narg('queue_name'), queue_name),
    payload = COALESCE(sqlc.narg('payload'), payload),
    priority = COALESCE(sqlc.narg('priority'), priority),
    max_attempts = COALESCE(sqlc.narg('max_attempts'), max_attempts),
    next_run_at = COALESCE(sqlc.narg('next_run_at'), next_run_at),
    updated_at = now()
WHERE id = sqlc.arg('id') AND project_id = sqlc.arg('project_id')
RETURNING *;

-- name: DeleteSchedule :execrows
DELETE FROM schedules
WHERE id = $1 AND project_id = $2;

-- name: ListDueSchedules :many
SELECT schedules.* FROM schedules
JOIN projects ON projects.id = schedules.project_id
WHERE schedules.enabled
  AND schedules.next_run_at <= now()
  AND projects.archived_at IS NULL
ORDER BY schedules.next_run_at ASC
LIMIT $1;

-- name: AdvanceSchedule :execrows
UPDATE schedules
SET next_run_at = $2,
    last_run_at = now(),
    updated_at = now()
WHERE id = $1
  AND next_run_at = $3;
