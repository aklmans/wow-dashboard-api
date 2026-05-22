-- +goose Up
-- +goose StatementBegin
CREATE TABLE system_events (
    id uuid PRIMARY KEY,
    event_type text NOT NULL,
    message text NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS system_events;
-- +goose StatementEnd
