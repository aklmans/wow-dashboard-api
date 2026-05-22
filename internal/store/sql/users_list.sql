-- name: ListUsersPage :many
SELECT
    u.id, u.email, u.display_name, u.status, u.created_at, u.updated_at,
    COALESCE(
        array_agg(DISTINCT r.name) FILTER (WHERE r.name IS NOT NULL),
        ARRAY[]::text[]
    )::text[] AS roles
FROM users u
LEFT JOIN user_roles ur ON ur.user_id = u.id
LEFT JOIN roles r ON r.id = ur.role_id
WHERE (
    sqlc.narg('search')::text IS NULL
    OR u.email ILIKE '%' || sqlc.narg('search')::text || '%'
    OR u.display_name ILIKE '%' || sqlc.narg('search')::text || '%'
)
AND (
    sqlc.narg('status')::text IS NULL
    OR u.status = sqlc.narg('status')::text
)
AND (
    sqlc.narg('role')::text IS NULL
    OR EXISTS (
        SELECT 1 FROM user_roles ur2
        JOIN roles r2 ON r2.id = ur2.role_id
        WHERE ur2.user_id = u.id AND r2.name = sqlc.narg('role')::text
    )
)
GROUP BY u.id
ORDER BY u.created_at DESC, u.id DESC
LIMIT @limit_val OFFSET @offset_val;

-- name: CountUsersPage :one
SELECT count(DISTINCT u.id)::bigint
FROM users u
WHERE (
    sqlc.narg('search')::text IS NULL
    OR u.email ILIKE '%' || sqlc.narg('search')::text || '%'
    OR u.display_name ILIKE '%' || sqlc.narg('search')::text || '%'
)
AND (
    sqlc.narg('status')::text IS NULL
    OR u.status = sqlc.narg('status')::text
)
AND (
    sqlc.narg('role')::text IS NULL
    OR EXISTS (
        SELECT 1 FROM user_roles ur2
        JOIN roles r2 ON r2.id = ur2.role_id
        WHERE ur2.user_id = u.id AND r2.name = sqlc.narg('role')::text
    )
);
