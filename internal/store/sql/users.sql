-- name: CreateUser :one
INSERT INTO users (id, email, display_name, password_hash, status, role, created_at, updated_at)
VALUES (@id, lower(@email), @display_name, @password_hash, @status, @role, @created_at, @updated_at)
RETURNING id, email, display_name, status, role, created_at, updated_at;

-- name: UpsertDemoUser :one
INSERT INTO users (id, email, display_name, password_hash, status, role, created_at, updated_at)
VALUES (@id, lower(@email), @display_name, @password_hash, @status, @role, @created_at, @updated_at)
ON CONFLICT (email) DO UPDATE
SET display_name = EXCLUDED.display_name,
    password_hash = EXCLUDED.password_hash,
    status = EXCLUDED.status,
    role = EXCLUDED.role,
    updated_at = EXCLUDED.updated_at
RETURNING id, email, display_name, status, role, created_at, updated_at;

-- name: GetUserByID :one
SELECT id, email, display_name, status, role, created_at, updated_at
FROM users
WHERE id = @id;

-- name: GetUserByEmail :one
SELECT id, email, display_name, status, role, created_at, updated_at
FROM users
WHERE email = lower(@email);

-- name: GetUserByEmailForAuth :one
SELECT id, email, display_name, password_hash, status, role, created_at, updated_at
FROM users
WHERE email = lower(@email);

-- name: ListUsers :many
SELECT id, email, display_name, status, role, created_at, updated_at
FROM users
ORDER BY created_at DESC
LIMIT @limit_val OFFSET @offset_val;

-- name: UpdateUser :one
UPDATE users
SET role = COALESCE(sqlc.narg('role'), role),
    status = COALESCE(sqlc.narg('status'), status),
    updated_at = @updated_at
WHERE id = @id
RETURNING id, email, display_name, status, role, created_at, updated_at;
