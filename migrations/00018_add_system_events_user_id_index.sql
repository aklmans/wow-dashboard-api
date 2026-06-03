-- +goose Up
-- +goose StatementBegin
-- Supports the per-user "security activity" query, which scopes system_events to
-- a single user via metadata->>'user_id' and pages by (created_at DESC, id DESC).
-- The expression index lets that lookup avoid a full scan of the audit log.
CREATE INDEX idx_system_events_user_id
    ON system_events ((metadata ->> 'user_id'), created_at DESC, id DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_system_events_user_id;
-- +goose StatementEnd
