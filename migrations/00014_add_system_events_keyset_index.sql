-- +goose Up
-- +goose StatementBegin
-- Composite index for keyset pagination over the audit log, ordered by
-- (created_at DESC, id DESC). Its leftmost column also serves the existing
-- created_at-range queries (listing, retention purge).
CREATE INDEX idx_system_events_created_at_id ON system_events (created_at DESC, id DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_system_events_created_at_id;
-- +goose StatementEnd
