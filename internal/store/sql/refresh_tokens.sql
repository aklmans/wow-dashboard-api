-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens (
    id,
    user_id,
    token_hash,
    family_id,
    expires_at,
    revoked_at,
    replaced_by_token_id,
    created_at,
    updated_at,
    user_agent,
    ip_address,
    last_used_at
) VALUES (
    @id,
    @user_id,
    @token_hash,
    @family_id,
    @expires_at,
    NULL,
    NULL,
    @created_at,
    @updated_at,
    @user_agent,
    @ip_address,
    @last_used_at
)
RETURNING
    id,
    user_id,
    token_hash,
    family_id,
    expires_at,
    revoked_at,
    replaced_by_token_id,
    created_at,
    updated_at,
    user_agent,
    ip_address,
    last_used_at;

-- name: GetRefreshTokenByHash :one
SELECT
    id,
    user_id,
    token_hash,
    family_id,
    expires_at,
    revoked_at,
    replaced_by_token_id,
    created_at,
    updated_at,
    user_agent,
    ip_address,
    last_used_at
FROM refresh_tokens
WHERE token_hash = @token_hash;

-- name: RotateRefreshToken :one
WITH revoked AS (
    UPDATE refresh_tokens AS old_token
    SET
        revoked_at = sqlc.arg(revoked_at),
        replaced_by_token_id = sqlc.arg(new_id),
        updated_at = sqlc.arg(revoked_at)
    WHERE old_token.id = sqlc.arg(old_id)
      AND old_token.revoked_at IS NULL
    RETURNING id
)
INSERT INTO refresh_tokens (
    id,
    user_id,
    token_hash,
    family_id,
    expires_at,
    revoked_at,
    replaced_by_token_id,
    created_at,
    updated_at,
    user_agent,
    ip_address,
    last_used_at
)
SELECT
    sqlc.arg(new_id),
    sqlc.arg(user_id),
    sqlc.arg(token_hash),
    sqlc.arg(family_id),
    sqlc.arg(expires_at),
    NULL,
    NULL,
    sqlc.arg(created_at),
    sqlc.arg(updated_at),
    sqlc.arg(user_agent),
    sqlc.arg(ip_address),
    sqlc.arg(last_used_at)
WHERE EXISTS (SELECT 1 FROM revoked)
RETURNING
    id,
    user_id,
    token_hash,
    family_id,
    expires_at,
    revoked_at,
    replaced_by_token_id,
    created_at,
    updated_at,
    user_agent,
    ip_address,
    last_used_at;

-- name: RevokeRefreshTokenByHash :exec
UPDATE refresh_tokens
SET
    revoked_at = @revoked_at,
    updated_at = @revoked_at
WHERE token_hash = @token_hash
  AND revoked_at IS NULL;

-- name: RevokeRefreshTokenFamily :exec
UPDATE refresh_tokens
SET
    revoked_at = @revoked_at,
    updated_at = @revoked_at
WHERE family_id = @family_id
  AND revoked_at IS NULL;

-- name: RevokeAllUserRefreshTokens :exec
UPDATE refresh_tokens
SET
    revoked_at = @revoked_at,
    updated_at = @revoked_at
WHERE user_id = @user_id
  AND revoked_at IS NULL;

-- name: RevokeUserRefreshTokensExceptFamily :exec
-- Revoke every active session for the user except the caller's current token
-- family, so "sign out other sessions" leaves the calling device signed in.
UPDATE refresh_tokens
SET
    revoked_at = @revoked_at,
    updated_at = @revoked_at
WHERE user_id = @user_id
  AND family_id <> @family_id
  AND revoked_at IS NULL;

-- name: ListActiveSessionsByUserID :many
-- One row per active session (refresh-token family): the family's current,
-- non-revoked, unexpired token, carrying the device metadata captured at
-- sign-in. Most-recently-used first.
SELECT
    id,
    user_id,
    token_hash,
    family_id,
    expires_at,
    revoked_at,
    replaced_by_token_id,
    created_at,
    updated_at,
    user_agent,
    ip_address,
    last_used_at
FROM refresh_tokens
WHERE user_id = @user_id
  AND revoked_at IS NULL
  AND expires_at > now()
ORDER BY last_used_at DESC NULLS LAST, created_at DESC;

-- name: RevokeUserRefreshTokenFamily :execrows
-- Revoke one active session (family) belonging to the user. Scoped by user_id so
-- a user can never revoke another account's session, and limited to unexpired
-- tokens so it matches exactly what the list exposes; rows affected = 0 means the
-- family was not one of the user's active sessions (wrong id, expired, or already
-- revoked) and the caller gets a 404.
UPDATE refresh_tokens
SET
    revoked_at = @revoked_at,
    updated_at = @revoked_at
WHERE user_id = @user_id
  AND family_id = @family_id
  AND revoked_at IS NULL
  AND expires_at > now();

-- name: DeleteExpiredRefreshTokens :execrows
-- Retention purge: drop refresh tokens that have already expired. Revoked but
-- not-yet-expired tokens are kept so reuse detection still recognises them.
DELETE FROM refresh_tokens
WHERE expires_at < now();
