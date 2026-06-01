-- +goose Up
-- +goose StatementBegin
-- TOTP MFA state on the user. mfa_enabled flips true only after the user
-- confirms a code during enrollment; until then a secret row may exist but MFA
-- is not yet active. mfa_confirmed_at records when it was turned on.
ALTER TABLE users
    ADD COLUMN mfa_enabled bool NOT NULL DEFAULT false,
    ADD COLUMN mfa_confirmed_at timestamptz;

-- One TOTP secret per user. secret_encrypted is AES-256-GCM ciphertext, never
-- plaintext. A row exists while enrolling (mfa_enabled still false) and while
-- enrolled (mfa_enabled true); algorithm/digits/period pin the TOTP parameters.
CREATE TABLE user_mfa_secrets (
    id               uuid PRIMARY KEY,
    user_id          uuid NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,
    secret_encrypted text NOT NULL,
    algorithm        text NOT NULL DEFAULT 'SHA1',
    digits           int NOT NULL DEFAULT 6,
    period           int NOT NULL DEFAULT 30,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

-- One-time recovery codes, stored as SHA-256 hashes (like refresh tokens) — the
-- raw codes are shown to the user once at enrollment and never persisted.
-- used_at is set when a code is consumed; NULL means still valid.
CREATE TABLE user_mfa_recovery_codes (
    id         uuid PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash  text NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_user_mfa_recovery_codes_user_id ON user_mfa_recovery_codes (user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS user_mfa_recovery_codes;
DROP TABLE IF EXISTS user_mfa_secrets;
ALTER TABLE users
    DROP COLUMN mfa_enabled,
    DROP COLUMN mfa_confirmed_at;
-- +goose StatementEnd
