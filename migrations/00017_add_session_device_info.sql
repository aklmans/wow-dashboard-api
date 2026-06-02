-- +goose Up
-- +goose StatementBegin
-- Device/session metadata for the account "active sessions" list. Captured at
-- sign-in and carried forward across rotations (the device identity persists for
-- the life of a refresh-token family); last_used_at is bumped on every rotation
-- so the list can show when each session was last active. All nullable so
-- pre-existing tokens (and any path that does not capture them) stay valid.
ALTER TABLE refresh_tokens
    ADD COLUMN user_agent text,
    ADD COLUMN ip_address text,
    ADD COLUMN last_used_at timestamptz;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE refresh_tokens
    DROP COLUMN IF EXISTS last_used_at,
    DROP COLUMN IF EXISTS ip_address,
    DROP COLUMN IF EXISTS user_agent;
-- +goose StatementEnd
