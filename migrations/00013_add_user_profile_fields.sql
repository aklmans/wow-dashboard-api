-- +goose Up
-- +goose StatementBegin
ALTER TABLE users
    ADD COLUMN avatar_url text,
    ADD COLUMN phone text,
    ADD COLUMN job_title text,
    ADD COLUMN company text,
    ADD COLUMN last_login_at timestamptz;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users
    DROP COLUMN avatar_url,
    DROP COLUMN phone,
    DROP COLUMN job_title,
    DROP COLUMN company,
    DROP COLUMN last_login_at;
-- +goose StatementEnd
