-- name: ListUserPermissions :many
SELECT DISTINCT rp.permission
FROM user_roles ur
JOIN role_permissions rp ON rp.role_id = ur.role_id
WHERE ur.user_id = @user_id;

-- name: ListUserRoles :many
SELECT r.id, r.name
FROM user_roles ur
JOIN roles r ON r.id = ur.role_id
WHERE ur.user_id = @user_id
ORDER BY r.name ASC;

-- name: AddUserRole :exec
INSERT INTO user_roles (user_id, role_id)
VALUES (@user_id, @role_id)
ON CONFLICT (user_id, role_id) DO NOTHING;

-- name: ReplaceUserRoles :exec
WITH cleared AS (
    DELETE FROM user_roles WHERE user_id = @user_id
)
INSERT INTO user_roles (user_id, role_id)
SELECT @user_id, rid
FROM unnest(@role_ids::uuid[]) AS rid;
