-- +goose Up
-- +goose StatementBegin
-- Project creation became permission-gated (projects:create). Grant it to the
-- built-in user role so existing and new standard users can still create
-- projects; admin already holds the '*' wildcard.
INSERT INTO role_permissions (role_id, permission)
VALUES ('00000000-0000-0000-0000-0000000a0002', 'projects:create')
ON CONFLICT (role_id, permission) DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM role_permissions
WHERE role_id = '00000000-0000-0000-0000-0000000a0002'
  AND permission = 'projects:create';
-- +goose StatementEnd
