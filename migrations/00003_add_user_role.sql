-- +goose Up
-- +goose StatementBegin
ALTER TABLE users
    ADD COLUMN role text NOT NULL DEFAULT 'user';

ALTER TABLE users
    ADD CONSTRAINT users_role_valid CHECK (role IN ('admin', 'user'));

CREATE INDEX idx_users_role ON users (role);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_users_role;

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_role_valid;

ALTER TABLE users
    DROP COLUMN IF EXISTS role;
-- +goose StatementEnd
