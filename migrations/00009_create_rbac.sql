-- +goose Up
-- +goose StatementBegin
-- roles is the catalog of named roles. is_system marks the built-in admin and
-- user roles, which cannot be renamed or deleted.
CREATE TABLE roles (
    id          uuid PRIMARY KEY,
    name        text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    is_system   boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL
);

-- role_permissions grants code-defined permission strings to a role. The
-- reserved wildcard '*' grants every permission and is held only by the
-- built-in admin role.
CREATE TABLE role_permissions (
    role_id    uuid NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    permission text NOT NULL,
    PRIMARY KEY (role_id, permission)
);

-- user_roles assigns roles to users. A user may hold multiple roles; their
-- effective permissions are the union across all assigned roles.
CREATE TABLE user_roles (
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role_id uuid NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);
CREATE INDEX idx_user_roles_role_id ON user_roles (role_id);

-- Seed the two built-in system roles with fixed ids so later migrations and
-- seeds can reference them deterministically.
INSERT INTO roles (id, name, description, is_system, created_at, updated_at)
VALUES
    ('00000000-0000-0000-0000-0000000a0001', 'admin', 'Full administrative access.', true, now(), now()),
    ('00000000-0000-0000-0000-0000000a0002', 'user', 'Standard application user.', true, now(), now());

-- admin holds the wildcard. The user role grants no admin permissions; plain
-- users reach non-admin features (auth, projects) which are not permission-gated.
INSERT INTO role_permissions (role_id, permission)
VALUES ('00000000-0000-0000-0000-0000000a0001', '*');

-- Backfill role assignments from the existing users.role column.
INSERT INTO user_roles (user_id, role_id)
SELECT u.id, '00000000-0000-0000-0000-0000000a0001'
FROM users u
WHERE u.role = 'admin';

INSERT INTO user_roles (user_id, role_id)
SELECT u.id, '00000000-0000-0000-0000-0000000a0002'
FROM users u
WHERE u.role <> 'admin';

-- The role column is replaced by user_roles. Dropping it also drops the
-- users_role_valid CHECK constraint that depends on it.
ALTER TABLE users DROP COLUMN role;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users ADD COLUMN role text NOT NULL DEFAULT 'user';
UPDATE users
SET role = 'admin'
WHERE id IN (
    SELECT ur.user_id
    FROM user_roles ur
    WHERE ur.role_id = '00000000-0000-0000-0000-0000000a0001'
);
ALTER TABLE users ADD CONSTRAINT users_role_valid CHECK (role IN ('admin', 'user'));

DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS roles;
-- +goose StatementEnd
