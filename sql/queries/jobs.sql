-- name: ClaimJob :one
WITH candidate AS (
    SELECT jobs.id
    FROM jobs
    WHERE jobs.project_id = sqlc.arg('project_id')
      AND jobs.status IN ('pending', 'scheduled')
      AND jobs.scheduled_at <= now()
      AND jobs.attempts < CASE
          WHEN sqlc.arg('max_attempts_override')::int > 0
          THEN sqlc.arg('max_attempts_override')::int
          ELSE jobs.max_attempts
      END
      AND (
          cardinality(sqlc.arg('queue_names')::text[]) = 0
          OR jobs.queue_name = ANY(sqlc.arg('queue_names')::text[])
      )
      AND EXISTS (
          SELECT 1
          FROM outbox_messages
          WHERE outbox_messages.job_id = jobs.id
            AND outbox_messages.status = 'processed'
      )
    ORDER BY jobs.priority DESC, jobs.scheduled_at ASC, jobs.created_at ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE jobs
SET status = 'running',
    attempts = attempts + 1,
    max_attempts = CASE
        WHEN sqlc.arg('max_attempts_override')::int > 0
        THEN sqlc.arg('max_attempts_override')::int
        ELSE jobs.max_attempts
    END
FROM candidate
WHERE jobs.id = candidate.id
RETURNING jobs.*;

-- name: MarkJobSucceeded :one
UPDATE jobs
SET status = 'succeeded'
WHERE id = $1 AND status = 'running'
RETURNING *;

-- name: MarkJobScheduled :one
UPDATE jobs
SET status = 'scheduled', scheduled_at = $2
WHERE id = $1 AND status = 'running'
RETURNING *;

-- name: MarkJobFailed :one
UPDATE jobs
SET status = 'failed'
WHERE id = $1 AND status = 'running'
RETURNING *;

-- name: CreateJob :one
INSERT INTO jobs (
    project_id,
    queue_name,
    job_type,
    payload,
    status,
    priority,
    max_attempts,
    scheduled_at,
    idempotency_key,
    idempotency_hash
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (project_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
RETURNING *;

-- name: GetJobByIdempotencyKey :one
SELECT * FROM jobs
WHERE project_id = $1 AND idempotency_key = $2;

-- name: GetJob :one
SELECT * FROM jobs WHERE id = $1;

-- name: GetJobForProject :one
SELECT * FROM jobs
WHERE id = $1 AND project_id = $2;

-- name: ListJobsByProject :many
SELECT * FROM jobs
WHERE project_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: UpdateJobStatus :one
UPDATE jobs
SET status = $2
WHERE id = $1
RETURNING *;

-- name: DeleteExpiredJobs :execrows
DELETE FROM jobs
WHERE project_id = sqlc.arg('project_id')
  AND status IN ('succeeded', 'failed', 'cancelled')
  AND updated_at < now() - make_interval(days => sqlc.arg('retention_days')::int);

-- name: DeleteExpiredJobsAllProjects :execrows
DELETE FROM jobs
USING projects
WHERE jobs.project_id = projects.id
  AND projects.job_retention_days > 0
  AND jobs.status IN ('succeeded', 'failed', 'cancelled')
  AND jobs.updated_at < now() - make_interval(days => projects.job_retention_days);

-- name: CountQueuedJobsByProject :one
SELECT count(*) FROM jobs
WHERE project_id = $1 AND status IN ('pending', 'scheduled');

-- name: QueueDepthByProject :many
SELECT project_id, count(*) AS depth FROM jobs
WHERE status IN ('pending', 'scheduled')
GROUP BY project_id;
