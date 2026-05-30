-- +goose Up
-- +goose StatementBegin
-- notifications is a per-user feed surfaced in the app's notification bell.
-- Rows are created by domain code (e.g. when a user's roles change) and read
-- back only by their owner. read_at is NULL until the user marks the row read.
CREATE TABLE notifications (
    id         uuid PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    type       text NOT NULL,
    title      text NOT NULL,
    body       text NOT NULL DEFAULT '',
    metadata   jsonb NOT NULL DEFAULT '{}'::jsonb,
    read_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Keyset pagination index: a user's notifications, newest first.
CREATE INDEX idx_notifications_user_created_id ON notifications (user_id, created_at DESC, id DESC);

-- Partial index backing the unread-count and unread-only list, kept small by
-- excluding already-read rows.
CREATE INDEX idx_notifications_user_unread ON notifications (user_id) WHERE read_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS notifications;
-- +goose StatementEnd
