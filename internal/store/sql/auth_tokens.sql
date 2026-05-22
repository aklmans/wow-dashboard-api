-- name: CreateAuthToken :exec
INSERT INTO auth_tokens (id, user_id, purpose, token_hash, expires_at, created_at)
VALUES (@id, @user_id, @purpose, @token_hash, @expires_at, @created_at);

-- name: GetAuthTokenByHash :one
SELECT id, user_id, purpose, token_hash, expires_at, used_at, created_at
FROM auth_tokens
WHERE token_hash = @token_hash AND purpose = @purpose;

-- name: MarkAuthTokenUsed :exec
UPDATE auth_tokens
SET used_at = @used_at
WHERE id = @id;

-- name: DeleteAuthTokensForUser :exec
-- Invalidates any outstanding tokens of a purpose for a user, so issuing a new
-- one supersedes the old.
DELETE FROM auth_tokens
WHERE user_id = @user_id AND purpose = @purpose;
