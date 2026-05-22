-- +goose Up
-- +goose StatementBegin
CREATE INDEX idx_system_events_created_at ON system_events (created_at DESC);
CREATE INDEX idx_system_events_event_type ON system_events (event_type);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_system_events_event_type;
DROP INDEX IF EXISTS idx_system_events_created_at;
-- +goose StatementEnd
