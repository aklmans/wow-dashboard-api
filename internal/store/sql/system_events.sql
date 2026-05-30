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

-- name: DeleteSystemEventsBefore :execrows
-- Retention purge: drop audit events older than the cutoff.
DELETE FROM system_events
WHERE created_at < @cutoff;
