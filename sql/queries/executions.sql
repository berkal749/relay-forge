-- name: CreateExecution :one
INSERT INTO executions (job_id, worker_id, attempt_number, status)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetExecution :one
SELECT * FROM executions WHERE id = $1;

-- name: ListExecutionsByJob :many
SELECT * FROM executions
WHERE job_id = $1
ORDER BY attempt_number;

-- name: FinishExecution :one
UPDATE executions
SET
    status = $2,
    finished_at = now(),
    error_message = $3,
    result = $4
WHERE id = $1
RETURNING *;
