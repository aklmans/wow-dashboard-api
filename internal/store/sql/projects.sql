-- name: CreateProject :one
INSERT INTO projects (id, name, description, status, owner_user_id, created_at, updated_at)
VALUES (@id, @name, @description, @status, @owner_user_id, @created_at, @updated_at)
RETURNING id, name, description, status, owner_user_id, created_at, updated_at;

-- name: GetProjectWithAccess :one
-- Returns the project together with the requesting user's effective access
-- role ('owner', 'editor', or 'viewer'). No row is returned when the user is
-- neither the owner nor a member, so callers cannot probe foreign projects.
SELECT
    p.id, p.name, p.description, p.status, p.owner_user_id, p.created_at, p.updated_at,
    CAST(CASE WHEN p.owner_user_id = @user_id THEN 'owner' ELSE m.role END AS text) AS access_role
FROM projects p
LEFT JOIN project_members m ON m.project_id = p.id AND m.user_id = @user_id
WHERE p.id = @id
  AND (p.owner_user_id = @user_id OR m.user_id IS NOT NULL);

-- name: ListProjectsPage :many
SELECT id, name, description, status, owner_user_id, created_at, updated_at
FROM projects
WHERE (
    owner_user_id = @user_id
    OR id IN (SELECT project_id FROM project_members WHERE user_id = @user_id)
)
AND (
    sqlc.narg('search')::text IS NULL
    OR name ILIKE '%' || sqlc.narg('search')::text || '%'
    OR description ILIKE '%' || sqlc.narg('search')::text || '%'
)
AND (
    sqlc.narg('status')::text IS NULL
    OR status = sqlc.narg('status')::text
)
ORDER BY created_at DESC, id DESC
LIMIT @limit_val OFFSET @offset_val;

-- name: CountProjectsPage :one
SELECT count(*)::bigint
FROM projects
WHERE (
    owner_user_id = @user_id
    OR id IN (SELECT project_id FROM project_members WHERE user_id = @user_id)
)
AND (
    sqlc.narg('search')::text IS NULL
    OR name ILIKE '%' || sqlc.narg('search')::text || '%'
    OR description ILIKE '%' || sqlc.narg('search')::text || '%'
)
AND (
    sqlc.narg('status')::text IS NULL
    OR status = sqlc.narg('status')::text
);

-- name: UpdateProject :one
-- Scoped by id only: the service authorizes the caller (owner or editor) via
-- GetProjectWithAccess before invoking this query.
UPDATE projects
SET
    name = COALESCE(sqlc.narg('name')::text, name),
    description = COALESCE(sqlc.narg('description')::text, description),
    status = COALESCE(sqlc.narg('status')::text, status),
    updated_at = @updated_at
WHERE id = @id
RETURNING id, name, description, status, owner_user_id, created_at, updated_at;

-- name: ArchiveProject :one
UPDATE projects
SET
    status = 'archived',
    updated_at = @updated_at
WHERE id = @id AND owner_user_id = @owner_user_id
RETURNING id, name, description, status, owner_user_id, created_at, updated_at;
