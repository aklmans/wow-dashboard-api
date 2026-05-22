-- name: ListUsersPage :many
SELECT id, email, display_name, status, role, created_at, updated_at
FROM users
WHERE (
    sqlc.narg('search')::text IS NULL
    OR email ILIKE '%' || sqlc.narg('search')::text || '%'
    OR display_name ILIKE '%' || sqlc.narg('search')::text || '%'
)
AND (
    sqlc.narg('role')::text IS NULL
    OR role = sqlc.narg('role')::text
)
AND (
    sqlc.narg('status')::text IS NULL
    OR status = sqlc.narg('status')::text
)
ORDER BY created_at DESC, id DESC
LIMIT @limit_val OFFSET @offset_val;

-- name: CountUsersPage :one
SELECT count(*)::bigint
FROM users
WHERE (
    sqlc.narg('search')::text IS NULL
    OR email ILIKE '%' || sqlc.narg('search')::text || '%'
    OR display_name ILIKE '%' || sqlc.narg('search')::text || '%'
)
AND (
    sqlc.narg('role')::text IS NULL
    OR role = sqlc.narg('role')::text
)
AND (
    sqlc.narg('status')::text IS NULL
    OR status = sqlc.narg('status')::text
);
