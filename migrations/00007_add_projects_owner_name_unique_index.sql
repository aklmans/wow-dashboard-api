-- +goose Up
-- +goose StatementBegin
-- Enforces one project name per owner. This index build aborts if the
-- projects table already holds duplicate (owner_user_id, name) rows; on an
-- existing populated database, de-duplicate those rows before applying this
-- migration. A fresh database created from migration 00006 has no duplicates.
CREATE UNIQUE INDEX idx_projects_owner_name_unique ON projects (owner_user_id, name);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_projects_owner_name_unique;
-- +goose StatementEnd
