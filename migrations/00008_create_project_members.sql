-- +goose Up
-- +goose StatementBegin
-- project_members holds non-owner access grants for a project. The project
-- owner is authoritative on projects.owner_user_id and is never stored here.
CREATE TABLE project_members (
    project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role       text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (project_id, user_id),
    CONSTRAINT project_members_role_valid CHECK (role IN ('viewer', 'editor'))
);
-- Supports "projects shared with me" lookups keyed by user.
CREATE INDEX idx_project_members_user_id ON project_members (user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS project_members;
-- +goose StatementEnd
