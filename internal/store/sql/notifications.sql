-- name: CreateNotification :one
INSERT INTO notifications (id, user_id, type, title, body, metadata, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, user_id, type, title, body, metadata, read_at, created_at;

-- name: ListNotificationsPage :many
-- Keyset pagination over a user's notifications, newest first. unread_only
-- restricts to unread rows; the (cursor_created_at, cursor_id) pair pages
-- strictly past the previous page's last row. Callers fetch one extra row to
-- detect whether a further page exists.
SELECT id, user_id, type, title, body, metadata, read_at, created_at
FROM notifications
WHERE user_id = sqlc.arg('user_id')
  AND (NOT sqlc.arg('unread_only')::bool OR read_at IS NULL)
  AND (
    sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR created_at < sqlc.narg('cursor_created_at')
    OR (created_at = sqlc.narg('cursor_created_at') AND id < sqlc.narg('cursor_id'))
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('row_limit');

-- name: CountUnreadNotifications :one
SELECT count(*)
FROM notifications
WHERE user_id = $1 AND read_at IS NULL;

-- name: MarkNotificationRead :execrows
-- Marks one notification read, scoped to its owner so a user can never touch
-- another user's rows. Idempotent: an already-read or non-owned row matches
-- nothing and affects zero rows.
UPDATE notifications
SET read_at = now()
WHERE id = sqlc.arg('id') AND user_id = sqlc.arg('user_id') AND read_at IS NULL;

-- name: MarkAllNotificationsRead :execrows
UPDATE notifications
SET read_at = now()
WHERE user_id = $1 AND read_at IS NULL;
