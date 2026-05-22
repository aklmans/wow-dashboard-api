-- +goose Up
-- +goose StatementBegin
-- Account-level brute-force protection: track consecutive failed sign-ins and
-- a self-healing lock window, independent of the per-IP rate limiter.
ALTER TABLE users
    ADD COLUMN failed_login_count integer NOT NULL DEFAULT 0,
    ADD COLUMN locked_until timestamptz;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users
    DROP COLUMN failed_login_count,
    DROP COLUMN locked_until;
-- +goose StatementEnd
