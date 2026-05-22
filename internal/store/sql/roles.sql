-- name: GetRoleByName :one
SELECT id, name, description, is_system, created_at, updated_at
FROM roles
WHERE name = @name;

-- name: CountRolesByIDs :one
SELECT count(*)::bigint
FROM roles
WHERE id = ANY (@ids::uuid[]);

-- name: ListRoles :many
SELECT
    r.id, r.name, r.description, r.is_system, r.created_at, r.updated_at,
    COALESCE(
        array_agg(DISTINCT rp.permission) FILTER (WHERE rp.permission IS NOT NULL),
        ARRAY[]::text[]
    )::text[] AS permissions,
    (SELECT count(*) FROM user_roles ur WHERE ur.role_id = r.id)::bigint AS user_count
FROM roles r
LEFT JOIN role_permissions rp ON rp.role_id = r.id
GROUP BY r.id
ORDER BY r.is_system DESC, r.name ASC;

-- name: GetRoleByID :one
SELECT
    r.id, r.name, r.description, r.is_system, r.created_at, r.updated_at,
    COALESCE(
        array_agg(DISTINCT rp.permission) FILTER (WHERE rp.permission IS NOT NULL),
        ARRAY[]::text[]
    )::text[] AS permissions,
    (SELECT count(*) FROM user_roles ur WHERE ur.role_id = r.id)::bigint AS user_count
FROM roles r
LEFT JOIN role_permissions rp ON rp.role_id = r.id
WHERE r.id = @id
GROUP BY r.id;

-- name: CreateRole :one
INSERT INTO roles (id, name, description, is_system, created_at, updated_at)
VALUES (@id, @name, @description, false, @created_at, @updated_at)
RETURNING id, name, description, is_system, created_at, updated_at;

-- name: AddRolePermissions :exec
INSERT INTO role_permissions (role_id, permission)
SELECT @role_id, p
FROM unnest(@permissions::text[]) AS p;

-- name: UpdateRoleDetails :exec
UPDATE roles
SET name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    updated_at = @updated_at
WHERE id = @id;

-- name: ReplaceRolePermissions :exec
WITH cleared AS (
    DELETE FROM role_permissions WHERE role_id = @role_id
)
INSERT INTO role_permissions (role_id, permission)
SELECT @role_id, p
FROM unnest(@permissions::text[]) AS p;

-- name: DeleteRole :execrows
DELETE FROM roles
WHERE id = @id
  AND is_system = false
  AND NOT EXISTS (SELECT 1 FROM user_roles ur WHERE ur.role_id = @id);
