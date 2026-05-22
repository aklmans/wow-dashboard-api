-- name: CreateProjectMember :one
INSERT INTO project_members (project_id, user_id, role, created_at, updated_at)
VALUES (@project_id, @user_id, @role, @created_at, @updated_at)
RETURNING project_id, user_id, role, created_at, updated_at;

-- name: ListProjectMembers :many
SELECT m.project_id, m.user_id, m.role, m.created_at, m.updated_at,
       u.email, u.display_name
FROM project_members m
JOIN users u ON u.id = m.user_id
WHERE m.project_id = @project_id
ORDER BY m.created_at ASC, m.user_id ASC;

-- name: GetProjectMember :one
SELECT project_id, user_id, role, created_at, updated_at
FROM project_members
WHERE project_id = @project_id AND user_id = @user_id;

-- name: UpdateProjectMemberRole :one
UPDATE project_members
SET role = @role, updated_at = @updated_at
WHERE project_id = @project_id AND user_id = @user_id
RETURNING project_id, user_id, role, created_at, updated_at;

-- name: DeleteProjectMember :execrows
DELETE FROM project_members
WHERE project_id = @project_id AND user_id = @user_id;
