-- name: GetRoleByName :one
SELECT id, name, description, is_system, created_at, updated_at
FROM roles
WHERE name = @name;

-- name: CountRolesByIDs :one
SELECT count(*)::bigint
FROM roles
WHERE id = ANY (@ids::uuid[]);
