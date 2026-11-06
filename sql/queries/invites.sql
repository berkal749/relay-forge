-- name: CreateInvite :one
INSERT INTO invites (token_hash, email, role, created_by, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetInviteByTokenHash :one
SELECT * FROM invites WHERE token_hash = $1;

-- name: GetInviteByTokenHashForUpdate :one
SELECT * FROM invites WHERE token_hash = $1 FOR UPDATE;

-- name: ConsumeInvite :one
UPDATE invites
SET consumed_at = now(), consumed_by = $2
WHERE id = $1 AND consumed_at IS NULL AND expires_at > now()
RETURNING *;

-- name: ListInvites :many
SELECT * FROM invites ORDER BY created_at DESC LIMIT $1;

-- name: GetInvite :one
SELECT * FROM invites WHERE id = $1;

-- name: DeleteInvite :execrows
DELETE FROM invites WHERE id = $1;
