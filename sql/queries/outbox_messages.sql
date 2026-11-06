-- name: CreateOutboxMessage :one
INSERT INTO outbox_messages (job_id)
VALUES ($1)
RETURNING *;

-- name: ListPendingOutboxMessages :many
SELECT * FROM outbox_messages
WHERE status IN ('pending', 'processing')
ORDER BY created_at
LIMIT $1;

-- name: ClaimOutboxMessages :many
WITH claimed AS (
    SELECT id
    FROM outbox_messages
    WHERE status = 'pending'
    ORDER BY created_at
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
UPDATE outbox_messages AS message
SET status = 'processing'
FROM claimed
WHERE message.id = claimed.id
RETURNING message.*;

-- name: MarkOutboxMessageProcessed :one
UPDATE outbox_messages
SET status = 'processed', processed_at = now()
WHERE id = $1
RETURNING *;

-- name: CountOutboxBacklog :one
SELECT count(*) FROM outbox_messages
WHERE status IN ('pending', 'processing');
