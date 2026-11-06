-- name: CreateNotificationChannel :one
INSERT INTO notification_channels (project_id, name, kind, config)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetNotificationChannel :one
SELECT * FROM notification_channels
WHERE id = $1 AND project_id = $2;

-- name: ListNotificationChannelsByProject :many
SELECT * FROM notification_channels
WHERE project_id = $1
ORDER BY created_at DESC;

-- name: CountNotificationChannelsByProject :one
SELECT count(*) FROM notification_channels
WHERE project_id = $1;

-- name: DeleteNotificationChannel :execrows
DELETE FROM notification_channels
WHERE id = $1 AND project_id = $2;
