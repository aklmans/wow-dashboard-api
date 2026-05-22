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
-- Insert the new assignments first, then delete the ones no longer in the
-- set. A data-modifying CTE shares one snapshot, so a DELETE-then-INSERT would
-- not see its own deletes and would collide on rows common to both sets.
WITH added AS (
    INSERT INTO user_roles (user_id, role_id)
    SELECT @user_id, rid
    FROM unnest(@role_ids::uuid[]) AS rid
    ON CONFLICT (user_id, role_id) DO NOTHING
)
DELETE FROM user_roles
WHERE user_roles.user_id = @user_id
  AND user_roles.role_id <> ALL (@role_ids::uuid[]);
