-- name: LockUserMfaEnabled :one
-- Locks the user row for the duration of the transaction so concurrent MFA
-- setup/confirm requests for the same user serialize, and returns the current
-- mfa_enabled flag observed under that lock.
SELECT mfa_enabled FROM users WHERE id = @id FOR UPDATE;

-- name: SetUserMfaEnabled :exec
UPDATE users
SET mfa_enabled = @mfa_enabled,
    mfa_confirmed_at = @mfa_confirmed_at,
    updated_at = @updated_at
WHERE id = @id;

-- name: UpsertUserMfaSecret :one
-- Setup replaces any in-progress (unconfirmed) secret for the user; the UNIQUE
-- user_id makes this an upsert.
INSERT INTO user_mfa_secrets (id, user_id, secret_encrypted, algorithm, digits, period, created_at, updated_at)
VALUES (@id, @user_id, @secret_encrypted, @algorithm, @digits, @period, @created_at, @updated_at)
ON CONFLICT (user_id) DO UPDATE
SET secret_encrypted = EXCLUDED.secret_encrypted,
    algorithm = EXCLUDED.algorithm,
    digits = EXCLUDED.digits,
    period = EXCLUDED.period,
    updated_at = EXCLUDED.updated_at
RETURNING id, user_id, secret_encrypted, algorithm, digits, period, created_at, updated_at;

-- name: GetUserMfaSecret :one
SELECT id, user_id, secret_encrypted, algorithm, digits, period, created_at, updated_at
FROM user_mfa_secrets
WHERE user_id = @user_id;

-- name: DeleteUserMfaSecret :exec
DELETE FROM user_mfa_secrets WHERE user_id = @user_id;

-- name: DeleteMfaRecoveryCodesForUser :exec
DELETE FROM user_mfa_recovery_codes WHERE user_id = @user_id;

-- name: CreateMfaRecoveryCode :exec
INSERT INTO user_mfa_recovery_codes (id, user_id, code_hash, created_at)
VALUES (@id, @user_id, @code_hash, @created_at);

-- name: ConsumeMfaRecoveryCode :one
-- Atomically mark an unused recovery code as used and return its id. No row
-- (pgx.ErrNoRows) means the code was wrong or already used.
UPDATE user_mfa_recovery_codes
SET used_at = @used_at
WHERE user_id = @user_id AND code_hash = @code_hash AND used_at IS NULL
RETURNING id;
