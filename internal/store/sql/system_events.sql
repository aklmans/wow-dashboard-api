-- name: CreateSystemEvent :one
INSERT INTO system_events (id, event_type, message, metadata, created_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, event_type, message, metadata, created_at;

-- name: GetSystemEvent :one
SELECT id, event_type, message, metadata, created_at
FROM system_events
WHERE id = $1;

-- name: ListSystemEvents :many
SELECT id, event_type, message, metadata, created_at
FROM system_events
ORDER BY created_at DESC
LIMIT $1;

-- name: ListSystemEventsPage :many
-- Keyset pagination over the audit log, newest first. Optional filters narrow
-- by event type and created_at range; the (cursor_created_at, cursor_id) pair
-- pages strictly past the previous page's last row. Callers fetch one extra row
-- to detect whether a further page exists.
SELECT id, event_type, message, metadata, created_at
FROM system_events
WHERE (sqlc.narg('event_type')::text IS NULL OR event_type = sqlc.narg('event_type'))
  AND (sqlc.narg('created_after')::timestamptz IS NULL OR created_at >= sqlc.narg('created_after'))
  AND (sqlc.narg('created_before')::timestamptz IS NULL OR created_at < sqlc.narg('created_before'))
  AND (
    sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR created_at < sqlc.narg('cursor_created_at')
    OR (created_at = sqlc.narg('cursor_created_at') AND id < sqlc.narg('cursor_id'))
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('row_limit');

-- name: ListUserSecurityActivityPage :many
-- Per-user "security activity": the user's own auth audit events (event_type
-- auth.*, scoped by metadata->>'user_id'), keyset-paginated newest first. One
-- extra row is fetched to detect whether a further page exists.
SELECT id, event_type, message, metadata, created_at
FROM system_events
WHERE event_type LIKE 'auth.%'
  AND metadata ->> 'user_id' = sqlc.arg('user_id')::text
  AND (
    sqlc.narg('cursor_created_at')::timestamptz IS NULL
    OR created_at < sqlc.narg('cursor_created_at')
    OR (created_at = sqlc.narg('cursor_created_at') AND id < sqlc.narg('cursor_id'))
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('row_limit');

-- name: DeleteSystemEventsBefore :execrows
-- Retention purge: drop audit events older than the cutoff.
DELETE FROM system_events
WHERE created_at < @cutoff;
