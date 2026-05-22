-- name: CreateUser :one
INSERT INTO users (id, email, display_name, password_hash, status, created_at, updated_at)
VALUES (@id, lower(@email), @display_name, @password_hash, @status, @created_at, @updated_at)
RETURNING id, email, display_name, status, created_at, updated_at;

-- name: UpsertDemoUser :one
INSERT INTO users (id, email, display_name, password_hash, status, created_at, updated_at)
VALUES (@id, lower(@email), @display_name, @password_hash, @status, @created_at, @updated_at)
ON CONFLICT (email) DO UPDATE
SET display_name = EXCLUDED.display_name,
    password_hash = EXCLUDED.password_hash,
    status = EXCLUDED.status,
    updated_at = EXCLUDED.updated_at
RETURNING id, email, display_name, status, created_at, updated_at;

-- name: GetUserByID :one
SELECT id, email, display_name, status, created_at, updated_at, email_verified_at
FROM users
WHERE id = @id;

-- name: GetUserByEmail :one
SELECT id, email, display_name, status, created_at, updated_at
FROM users
WHERE email = lower(@email);

-- name: GetUserByEmailForAuth :one
SELECT id, email, display_name, password_hash, status, created_at, updated_at,
       failed_login_count, locked_until, email_verified_at
FROM users
WHERE email = lower(@email);

-- name: GetUserByIDForAuth :one
SELECT id, email, display_name, password_hash, status, created_at, updated_at,
       failed_login_count, locked_until, email_verified_at
FROM users
WHERE id = @id;

-- name: UpdateUserPassword :exec
UPDATE users
SET password_hash = @password_hash, updated_at = @updated_at
WHERE id = @id;

-- name: SetEmailVerified :exec
UPDATE users
SET email_verified_at = @verified_at, updated_at = @updated_at
WHERE id = @id;

-- name: ListUsers :many
SELECT id, email, display_name, status, created_at, updated_at
FROM users
ORDER BY created_at DESC
LIMIT @limit_val OFFSET @offset_val;

-- name: UpdateUserStatus :one
UPDATE users
SET status = @status, updated_at = @updated_at
WHERE id = @id
RETURNING id, email, display_name, status, created_at, updated_at;

-- name: RegisterLoginFailure :one
-- Records a failed sign-in. On reaching @max_attempts the counter resets and
-- the account is locked until @locked_until; returns the resulting lock time.
UPDATE users
SET failed_login_count = CASE
        WHEN failed_login_count + 1 >= @max_attempts::int THEN 0
        ELSE failed_login_count + 1
    END,
    locked_until = CASE
        WHEN failed_login_count + 1 >= @max_attempts::int THEN @locked_until::timestamptz
        ELSE locked_until
    END,
    updated_at = @updated_at
WHERE id = @id
RETURNING locked_until;

-- name: ClearLoginFailures :exec
-- Clears the failure counter and any lock after a successful sign-in.
UPDATE users
SET failed_login_count = 0, locked_until = NULL, updated_at = @updated_at
WHERE id = @id;
