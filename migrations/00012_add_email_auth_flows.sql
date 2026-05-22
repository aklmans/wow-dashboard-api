-- +goose Up
-- +goose StatementBegin
-- One-time tokens for email-driven auth flows. The raw token is emailed to the
-- user; only its hash is stored here, the same scheme used for refresh tokens.
CREATE TABLE auth_tokens (
    id         uuid PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose    text NOT NULL,
    token_hash text NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL,
    CONSTRAINT auth_tokens_purpose_valid
        CHECK (purpose IN ('password_reset', 'email_verification'))
);
CREATE INDEX idx_auth_tokens_user_purpose ON auth_tokens (user_id, purpose);

-- Records when a user confirmed their email address; NULL means unverified.
ALTER TABLE users ADD COLUMN email_verified_at timestamptz;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN email_verified_at;
DROP TABLE IF EXISTS auth_tokens;
-- +goose StatementEnd
