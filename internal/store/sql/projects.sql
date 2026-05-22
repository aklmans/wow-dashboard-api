-- name: CreateProject :one
INSERT INTO projects (id, name, description, status, owner_user_id, created_at, updated_at)
VALUES (@id, @name, @description, @status, @owner_user_id, @created_at, @updated_at)
RETURNING id, name, description, status, owner_user_id, created_at, updated_at;

-- name: GetProjectByID :one
SELECT id, name, description, status, owner_user_id, created_at, updated_at
FROM projects
WHERE id = @id AND owner_user_id = @owner_user_id;

-- name: ListProjectsPage :many
SELECT id, name, description, status, owner_user_id, created_at, updated_at
FROM projects
WHERE owner_user_id = @owner_user_id
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
WHERE owner_user_id = @owner_user_id
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
UPDATE projects
SET
    name = COALESCE(sqlc.narg('name')::text, name),
    description = COALESCE(sqlc.narg('description')::text, description),
    status = COALESCE(sqlc.narg('status')::text, status),
    updated_at = @updated_at
WHERE id = @id AND owner_user_id = @owner_user_id
RETURNING id, name, description, status, owner_user_id, created_at, updated_at;

-- name: ArchiveProject :one
UPDATE projects
SET
    status = 'archived',
    updated_at = @updated_at
WHERE id = @id AND owner_user_id = @owner_user_id
RETURNING id, name, description, status, owner_user_id, created_at, updated_at;
