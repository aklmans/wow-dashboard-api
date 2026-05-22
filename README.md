# wow-dashboard-api

Go API service for the Minimal Starter dashboard projects. Provides stable, typed, documented endpoints that the Next.js and Vite starter frontends can consume.

**Stack:** Go 1.26 · chi · Huma v2 · PostgreSQL · sqlc · goose · JWT auth · Air

Further documentation:

- [Operations Guide](docs/operations.md) — deployment, configuration, migrations, and release gates.
- [Frontend Integration Guide](docs/frontend-integration.md) — wiring a starter frontend to this API.
- [Audit Policy](docs/audit-policy.md) — audit event scope and failure-event rules.
- [CRUD Module Guide](docs/crud-module-guide.md) — adding a new CRUD business module.

## Prerequisites

- **Go 1.26.x** – [golang.org/dl](https://go.dev/dl/)
- **Air** – live-reload for local development
  ```sh
  go install github.com/air-verse/air@latest
  ```

## Local Development

1. Copy the example environment file and adjust as needed:
   ```sh
   cp .env.example .env
   ```

2. Start local PostgreSQL, apply migrations, and seed the demo auth user:
   ```sh
   make compose-up
   make local-setup
   ```

3. Start the dev server with live reload:
   ```sh
   make dev
   ```
   The API listens on `http://localhost:7272` by default.

4. In another terminal, verify the local auth flow:
   ```sh
   make smoke-auth
   ```

5. Optionally run the black-box Postman/Newman smoke baseline:
   ```sh
   make postman-test
   ```

`make local-reset` deletes the local PostgreSQL Docker volume and all local data before recreating the database, migrations, and demo seed.

## Configuration

All configuration is loaded from environment variables (via `caarlos0/env`). Most fields have local-development defaults; running the API with auth routes enabled requires `DATABASE_URL` so the auth service can reach PostgreSQL.

| Variable | Default | Description |
|----------|---------|-------------|
| `APP_NAME` | `wow-dashboard-api` | Application name for logs and identification |
| `PORT` | `7272` | HTTP listen port |
| `ENV` | `development` | Environment stage (`development`, `staging`, `production`) |
| `LOG_FORMAT` | `text` outside production, `json` in production | Structured log format (`text` or `json`). Invalid values cause startup to fail. |
| `LOG_LEVEL` | `info` | Structured log level (`debug`, `info`, `warn`, `error`). Invalid values cause startup to fail. |
| `READ_TIMEOUT_SECONDS` | `15` | HTTP server read timeout |
| `WRITE_TIMEOUT_SECONDS` | `15` | HTTP server write timeout |
| `IDLE_TIMEOUT_SECONDS` | `60` | HTTP server idle timeout |
| `HTTP_SHUTDOWN_TIMEOUT_SECONDS` | `10` | Graceful shutdown deadline for draining in-flight HTTP requests |
| `CORS_ALLOWED_ORIGINS` | `http://localhost:3000,http://localhost:5173,http://localhost:8082,http://localhost:8083,http://localhost:8084,http://localhost:8085` | Comma-separated allowed origins |
| `DATABASE_URL` | `postgres://wow_dashboard:wow_dashboard@localhost:5432/wow_dashboard_api?sslmode=disable` | PostgreSQL database connection URL for local Compose (masks credentials in errors/logs) |
| `DB_MAX_CONNS` | `10` | Maximum number of open connections in the pool |
| `DB_MIN_CONNS` | `1` | Minimum number of open connections in the pool |
| `DB_MAX_CONN_LIFETIME_SECONDS` | `1800` | Maximum amount of time a connection may be reused |
| `DB_MAX_CONN_IDLE_TIME_SECONDS` | `300` | Maximum amount of time a connection may be idle |
| `DB_HEALTH_TIMEOUT_SECONDS` | `3` | Timeout for pinging the database health |
| `AUTH_RATE_LIMIT_ENABLED` | `true` | Enable per-IP application rate limiting for auth sign-in/sign-up |
| `AUTH_RATE_LIMIT_REQUESTS` | `10` | Sustained auth requests allowed per window per IP |
| `AUTH_RATE_LIMIT_WINDOW_SECONDS` | `60` | Auth rate limit window in seconds |
| `AUTH_RATE_LIMIT_BURST` | `5` | Auth requests allowed immediately before throttling per IP |
| `JWT_ACCESS_SECRET` | `dev-only-change-me-min-32-characters` | JWT signing secret (minimum 32 characters, default forbidden in production) |
| `JWT_ISSUER` | `wow-dashboard-api` | Expected JWT issuer claim |
| `JWT_AUDIENCE` | `wow-dashboard` | Expected JWT audience claim |
| `JWT_ACCESS_TOKEN_TTL_SECONDS` | `900` | Access token time-to-live in seconds (defaults to 15 minutes / 900 seconds) |
| `REFRESH_TOKEN_TTL_SECONDS` | `1209600` | Refresh token time-to-live in seconds (defaults to 14 days) |
| `REFRESH_TOKEN_COOKIE_NAME` | `wow_dashboard_refresh_token` | HttpOnly refresh token cookie name |
| `REFRESH_TOKEN_COOKIE_SECURE` | `false` | Whether refresh cookies require HTTPS; `ENV=production` requires this to be `true` |
| `REFRESH_TOKEN_COOKIE_SAMESITE` | `lax` | Refresh cookie SameSite mode (`lax`, `strict`, or `none`) |

See [`.env.example`](.env.example) for a copy-pasteable template.

### Production Hardening

When `ENV=production`, the application applies strict startup validation: weak
or insecure defaults — a placeholder/short `JWT_ACCESS_SECRET`, wildcard or
loopback CORS origins, insecure refresh cookies, an empty `DATABASE_URL`, an
invalid cookie name, an out-of-range token TTL, or non-positive timeouts/pool
sizes — cause the process to refuse to start. See the
[Operations Guide](docs/operations.md#configuration-checks) for the full rule list.

The application does not trust client-supplied forwarding headers
(`X-Forwarded-For`, `X-Real-IP`) by default; auth rate limiting keys off the
socket remote address. Behind a trusted reverse proxy, enforce abuse limits at
the edge or add a reviewed trusted-proxy configuration first.

## Logging And Shutdown

The API uses the standard library `log/slog` for structured process and request logs. Local development defaults to text logs; `ENV=production` defaults to JSON logs unless `LOG_FORMAT` is explicitly set. `LOG_LEVEL` accepts `debug`, `info`, `warn`, and `error`.

Each HTTP request emits one `http_request` event with fields such as `request_id`, `method`, `path`, `status`, `duration_ms`, `remote_addr`, and `user_agent`. Query strings are logged only after sensitive names such as token and password fields are redacted. Request and response headers are not logged, so `Authorization`, `Cookie`, and `Set-Cookie` values stay out of logs.

Example JSON request log shape:

```json
{
  "level": "INFO",
  "msg": "http_request",
  "request_id": "abc-123",
  "method": "GET",
  "path": "/readyz",
  "status": 200,
  "duration_ms": 2.4,
  "remote_addr": "127.0.0.1:54321",
  "user_agent": "kube-probe/1.30"
}
```

For production, prefer `ENV=production`, leave `LOG_FORMAT` unset or set it to `json`, and ship stdout/stderr to the platform log collector. Avoid `LOG_LEVEL=debug` unless temporarily investigating an issue.

The API handles `SIGINT` and `SIGTERM` with graceful shutdown. On shutdown it stops accepting new connections, lets in-flight requests complete until `HTTP_SHUTDOWN_TIMEOUT_SECONDS`, then closes the PostgreSQL pool before the process exits. In Docker or Kubernetes, use `/healthz` as the liveness probe and `/readyz` as the readiness probe; during a rolling shutdown the platform should stop routing new traffic before or while `SIGTERM` gives the server time to drain.

## Error Responses

All error responses use a stable JSON envelope:

```json
{
  "code": "not_found",
  "message": "The requested resource was not found.",
  "request_id": "abc-123-def"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `code` | `string` | Machine-readable error code (e.g. `bad_request`, `unauthorized`, `not_found`, `internal_error`) |
| `message` | `string` | Human-readable, safe message — never contains internal details |
| `request_id` | `string` | The request ID from the current request for tracing |
| `details` | `array` | Optional field-level validation errors with `field` and `message`. Omitted if empty (`omitempty`). |

Internal errors always return a generic message. The original cause is logged server-side but never sent to clients.

## Health Checks

| Method | Path | Purpose | Dependencies |
|--------|------|---------|--------------|
| `GET` | `/healthz` | Liveness probe returning `{ "status": "ok" }` | None |
| `GET` | `/readyz` | Readiness probe returning `{ "status": "ready" }` | PostgreSQL ping |

When `/readyz` cannot reach PostgreSQL, it returns `503` using the standard API error envelope with `code: "service_unavailable"` and the safe message `Service is not ready.`

Use `/healthz` for liveness and `/readyz` for readiness. Do not configure `/readyz` as a liveness probe; a temporary database outage should stop new traffic, not force a restart loop.

## Auth Endpoints

Starter-compatible JWT auth HTTP endpoints are implemented under `/api/auth`:

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/auth/sign-up` | Create an account, return `{ "user": ..., "accessToken": ... }`, and set the HttpOnly refresh cookie |
| `POST` | `/api/auth/sign-in` | Authenticate credentials, return `{ "user": ..., "accessToken": ... }`, and set the HttpOnly refresh cookie |
| `POST` | `/api/auth/refresh` | Rotate the refresh cookie and return `{ "user": ..., "accessToken": ... }` |
| `POST` | `/api/auth/sign-out` | Revoke the current refresh token when present and clear the refresh cookie |
| `GET` | `/api/auth/me` | Return `{ "user": ... }` — including the caller's `roles` and effective `permissions` — for `Authorization: Bearer <accessToken>` |

`sign-up` and `sign-in` are protected by an in-memory, per-IP rate limiter. The default allows 10 auth attempts per minute with a burst of 5; limited requests return `429` with `code: "rate_limited"` and a `Retry-After` header.

Sign-in additionally enforces a per-account lockout: after 10 consecutive failed attempts an account is locked for 15 minutes (self-healing — the lock simply expires). A locked account returns the same generic invalid-credentials error so the lock state cannot be probed; a successful sign-in clears the counter.

Auth sign-up/sign-in success and failure events are written to the `system_events` table using stable event types:

| Event Type | Description |
|------------|-------------|
| `auth.sign_up.succeeded` | Account registration succeeded |
| `auth.sign_up.failed` | Account registration failed |
| `auth.sign_in.succeeded` | Credential authentication succeeded |
| `auth.sign_in.failed` | Credential authentication failed |

Audit metadata is a safe JSON object with fields such as `masked_email` (the email reduced to a low-PII form like `d***@example.com`), `user_id`, `reason`, and `request_id`. Audit writes are best-effort: if recording an event fails, the auth response keeps its original success or failure result and the server logs the audit error.

Local auth endpoint testing requires a PostgreSQL database and `DATABASE_URL` with migrations applied. `cmd/openapi` uses a stub service, so `make openapi` does not require a database connection.

## Authorization & RBAC

Beyond authentication, access is governed by **role-based access control**. Every user holds one or more **roles**; each role grants a set of **permissions**; a user's effective permissions are the union across all of their roles.

Permissions are a fixed, code-defined catalog (`internal/auth/rbac`) — a permission only carries meaning where a handler enforces it, so the catalog, not the database, is the single source of truth. Each is a `resource:action` string:

| Permission | Grants |
|------------|--------|
| `users:read` | List and view users |
| `users:manage` | Change a user's status or role assignments |
| `roles:read` | List roles and the assignable-permission catalog |
| `roles:manage` | Create, update, and delete roles |
| `system_events:read` | Read the system audit log |
| `projects:create` | Create new projects |

Roles are **dynamic and database-backed** (`roles`, `role_permissions`, and `user_roles` tables): an admin composes custom roles from the catalog through the [Roles endpoints](#roles-endpoints). Two **system roles** are built in and immutable through the API:

| Role | Description |
|------|-------------|
| `admin` | Holds the reserved `*` wildcard, which grants every permission — including ones added in future releases |
| `user` | Default role assigned to new sign-ups; grants `projects:create` but no administrative permissions |

Each handler gates itself with a `requirePermission` check; a caller missing the required permission receives `403 forbidden`. `GET /api/auth/me` returns the caller's `roles` and resolved `permissions` so a frontend can render menus and gate actions — but the server always re-checks independently and never trusts the client. Roles and permissions are resolved fresh on every request, so a change takes effect on the user's next call.

RBAC governs **functional** access — which features and menus a user can reach. It is orthogonal to **project sharing** (see [Projects endpoints](#projects-endpoints)), which governs instance-level access to individual projects through `viewer`/`editor` membership. The two mechanisms are independent and composable.

## Users Endpoints

User management endpoints live under `/api/users` and require `Authorization: Bearer <accessToken>`. The authenticated user must be active **and hold the `users:read` permission** (and `users:manage` for updates) through one of their roles; users without it receive `403 forbidden` with the `You do not have permission to perform this action.` envelope. `PATCH /api/users/{id}` updates a user's status and/or replaces their role set, and an admin cannot modify their own account.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/users` | Return paginated users as `{ "users": [...], "page": 1, "pageSize": 20, "total": 1 }` |
| `GET` | `/api/users/{id}` | Return a single user as `{ "user": { ... } }`. `404` when no user has the given id; `422` when `{id}` is not a valid UUID |
| `PATCH` | `/api/users/{id}` | Update a user's `status` and/or replace their `roleIds`. Returns `200` with `{ "user": { ... } }`; `404` when no user has the given id; `422` for invalid bodies; `403` when an admin targets their own account |

`GET /api/users` supports `page` (default `1`), `pageSize` (default `20`, max `100`), `search` (matches `email` or `display_name`), `role` (a role name), and `status` (`active` or `disabled`). Each user is returned with a `roles` array. Responses never include `password_hash`.

`PATCH /api/users/{id}` accepts a partial body with any subset of `status` (`active` or `disabled`) and `roleIds` (a non-empty set of role UUIDs that **replaces** the user's roles); at least one field must be provided. An administrator **cannot change their own status or roles** — this guarantees the system always retains at least one admin and prevents accidental self-lockout; hand over admin by assigning the admin role to another user first. Disabling a user takes effect immediately on refresh and within the access-token TTL on bearer requests. A successful update writes a `users.user.updated` system event.

## Roles Endpoints

Role management endpoints live under `/api/roles` and require `Authorization: Bearer <accessToken>`. Roles are dynamic and database-backed: an admin composes a role from the fixed permission catalog, and a user's effective permissions are the union across all of their roles. Reading requires the `roles:read` permission; creating, updating, and deleting requires `roles:manage`.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/roles` | List every role with its `permissions` and `userCount` |
| `GET` | `/api/roles/{id}` | Return a single role as `{ "role": { ... } }` |
| `POST` | `/api/roles` | Create a role: `{ "name": "...", "description": "...", "permissions": [...] }`. Returns `201`; `409` when the name is taken |
| `PATCH` | `/api/roles/{id}` | Update a role's `name`, `description`, and/or `permissions` (the set is replaced). `404` when not found; `409` for a system role |
| `DELETE` | `/api/roles/{id}` | Delete a role. `409` for a system role or one still assigned to users |
| `GET` | `/api/permissions` | Return the catalog of permissions that can be assigned to a role |

The built-in `admin` and `user` roles are **system roles**: they cannot be renamed, re-permissioned, or deleted through the API. A custom role cannot be deleted while any user is still assigned to it — reassign those users first. Every assigned permission must belong to the catalog returned by `GET /api/permissions`; the `*` wildcard is reserved for the `admin` role and cannot be assigned. Role writes record `roles.role.created`, `roles.role.updated`, and `roles.role.deleted` system events.

## Projects Endpoints

Project management endpoints live under `/api/projects` and require `Authorization: Bearer <accessToken>`. A project has a single **owner** (`owner_user_id`) and may additionally be **shared** with other users as `viewer` or `editor` members.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/projects` | Return paginated projects the user owns or is a member of, as `{ "projects": [...], "page": 1, "pageSize": 20, "total": 1 }` |
| `GET` | `/api/projects/{id}` | Return a single project as `{ "project": { ... } }`. Available to the owner and any member; `404` when the user has no access; `422` when `{id}` is not a valid UUID |
| `POST` | `/api/projects` | Create a project owned by the current user. Requires the `projects:create` permission. Returns `201` with `{ "project": { ... } }`; `409 conflict` on a duplicate owner-scoped name; `422` for invalid bodies |
| `PATCH` | `/api/projects/{id}` | Partially update a project. Owner or `editor` member only; a `viewer` receives `403`. Returns `200` with `{ "project": { ... } }`; `404` when the user has no access; `409 conflict` on a duplicate name; `422` for invalid bodies |
| `DELETE` | `/api/projects/{id}` | Archive a project. **Owner only** — a non-owner member receives `403`. Returns `200` with `{ "project": { ... } }` carrying `status: "archived"`; `404` when the user has no access |

`GET /api/projects` supports `page`, `pageSize`, `search` (matches `name` or `description`), and `status` (`active` or `archived`). `POST /api/projects` accepts `{ "name": "...", "description": "...", "status": "active" }` (description and status are optional; status defaults to `active`).

`PATCH /api/projects/{id}` accepts a partial body with any subset of `name`, `description`, and `status`. Omitted fields stay unchanged; passing an empty `description` clears the stored value; at least one field must be provided. `updated_at` is refreshed on every successful update.

### Project Sharing

A project owner can grant other users access. Access levels: `viewer` (read), `editor` (read + `PATCH`). The owner alone may archive the project and manage its members.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/projects/{id}/members` | List the project's non-owner members as `{ "members": [...] }`. Available to any user with access to the project |
| `POST` | `/api/projects/{id}/members` | Owner only. Grant access by email: `{ "email": "...", "role": "viewer\|editor" }`. Returns `201`; `409` when the user already has access; `422` when the email is not a registered user |
| `PATCH` | `/api/projects/{id}/members/{userId}` | Owner only. Change a member's role: `{ "role": "viewer\|editor" }`. `404` when the user is not a member |
| `DELETE` | `/api/projects/{id}/members/{userId}` | Owner only. Revoke a user's access. `404` when the user is not a member |

Project names are unique per owner after service-layer trimming. A duplicate create or update returns the standard error envelope with HTTP `409`, `code: "conflict"`, and `message: "Project name already exists."`. This baseline uses PostgreSQL's default case-sensitive text comparison, so `"Demo"` and `"demo"` may coexist for the same owner. Archived projects still reserve their names; archiving does not free a name for reuse.

`DELETE /api/projects/{id}` archives the project (sets `status` to `archived` and refreshes `updated_at`) instead of physically deleting the row. The endpoint is idempotent: archiving a project that is already archived still returns `200` with the archived project.

Project mutation and sharing success events are written to the `system_events` table using stable event types:

| Event Type | Description |
|------------|-------------|
| `projects.project.created` | Project creation succeeded |
| `projects.project.updated` | Project update succeeded |
| `projects.project.archived` | Project archive succeeded |
| `projects.member.added` | A user was granted project access |
| `projects.member.role_changed` | A member's project role was changed |
| `projects.member.removed` | A member's project access was revoked |

Project audit metadata is a safe JSON object with `project_id`, `owner_user_id`, `target_user_id`, `status`, `role`, `changed_fields`, and `request_id` — stable identifiers and enum values only, never project names, descriptions, or email. Audit writes are best-effort: if recording an event fails, the project response keeps its original success result and the server logs the audit error.

## System Events Endpoints

System audit event read endpoints live under `/api/system-events` and require `Authorization: Bearer <accessToken>`. The authenticated user must be active **and hold the `system_events:read` permission** through one of their roles; users without it receive `403 forbidden` with the `You do not have permission to perform this action.` envelope.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/system-events` | Return recent system audit events as `{ "events": [...], "limit": 20 }` |

`GET /api/system-events` supports `limit` (default `20`, min `1`, max `100`). The endpoint is read-only and only exposes events already written by audit producers such as auth sign-up/sign-in and project create/update/archive flows. The `events` array is non-nullable; an empty result returns `[]`.

Each event includes `id`, `eventType`, `message`, `metadata`, and `createdAt`. `metadata` is returned as a safe JSON object from audit writers; non-object metadata is converted to an empty object rather than a string or unsafe internal value.

## CRUD API Conventions

List-style endpoints share a small set of conventions so the dashboard frontends and generated TypeScript types stay predictable:

- **Query parameters**: `page` (default `1`, min `1`), `pageSize` (default `20`, min `1`, max `100`), and `search` (trimmed; empty string treated as no filter). Resource-specific filters (e.g. `role`, `status`) are added per endpoint.
- **Response envelope**: `{ "<items>": [...], "page": 1, "pageSize": 20, "total": N }`. The items array is non-nullable — empty pages return `[]`, never `null`.
- **Pagination helper**: parsing, defaulting, and bounds checking go through `internal/http/pagination.Normalize`. New list services should call it instead of re-deriving page/offset/search rules.
- **Resource detail endpoints**: use `GET /api/<resource>/{id}` with a UUID path param. Invalid UUIDs return `422 validation_failed`; missing resources return `404 not_found`. Path-param parsing goes through `internal/http/pathparam.ParseUUID`.
- **Owner-scoped resources**: services that scope rows to the current user (e.g. `projects`) take the authenticated user's id from the auth layer, parse it as a UUID at the service boundary, and pass it to every store query so isolation is enforced in SQL — not at the handler edge.
- **OpenAPI is the contract**: every endpoint is registered through Huma so that `openapi/openapi.json` and `openapi/typescript/schema.ts` (regenerated via `make openapi` / `make openapi-types`) stay the single source of truth for frontend types.

## Smoke & Acceptance Tests

Beyond `make check`, three black-box paths exercise the running API. The demo
account seeded by `make local-setup` is `demo@minimals.cc` / `@2Minimal`
(`make seed` is idempotent — it creates the user when missing and refreshes the
hash, display name, status, and role otherwise). For connecting a starter
frontend to the API, see the [Frontend Integration Guide](docs/frontend-integration.md).

`make smoke-auth` expects the demo user to be seeded first; `make local-setup` handles migrations and seed data for the local Docker Compose database. The smoke checks cover `GET /healthz`, `GET /readyz`, `POST /api/auth/sign-in`, refresh cookie issuance, `GET /api/auth/me`, `POST /api/auth/refresh` with refresh cookie rotation, old refresh cookie rejection, `POST /api/auth/sign-out`, refresh cookie clearing, and refresh rejection after sign-out. It uses `BASE_URL` (or legacy `SMOKE_AUTH_BASE_URL`), `SMOKE_AUTH_EMAIL`, and `SMOKE_AUTH_PASSWORD` overrides when set.

`make smoke-local` is the one-command local black-box acceptance path: it starts local PostgreSQL, runs migrations and seed data, launches `go run ./cmd/api` with the local database URL, waits up to 30 seconds for `http://localhost:7272/healthz`, runs the Newman collection, and stops only the API process that it started. It leaves PostgreSQL running so it does not disrupt a local database you are reusing. API output is written to `tmp/smoke-local-api.log`. To point this command at a different disposable database, set `SMOKE_LOCAL_DATABASE_URL` or `LOCAL_DATABASE_URL`; do not use the generic `DATABASE_URL` environment variable to drive `smoke-local`.

`make postman-test` runs only the Postman collection in `postman/wow-dashboard-api.postman_collection.json` with Newman via `npx --yes newman`; no global Postman or Newman install is required. Use it when the API is already running, including when you started it with `make dev`, `go run ./cmd/api`, Docker, or another terminal. Override defaults with `POSTMAN_BASE_URL`, `POSTMAN_EMAIL`, and `POSTMAN_PASSWORD`. The collection is a black-box smoke/acceptance check for the current HTTP API and does not replace the Go test suite. Its non-admin users authorization case signs up a timestamped disposable `role=user` account, so run it against disposable local data or use `make local-reset` when you want to clean up.

## Verification

Run all checks (formatting, vet, SQLC drift, tests, OpenAPI JSON drift, and generated TypeScript type drift):

```sh
make check
```

Individual targets are also available:

| Target                | Description                                        |
|-----------------------|----------------------------------------------------|
| `make fmt`            | Auto-format Go source files                        |
| `make fmt-check`      | Fail if any files are unformatted                   |
| `make test`           | Run unit tests                                     |
| `make test-race`      | Run unit tests with the race detector               |
| `make test-integration` | Run integration-tagged tests with Testcontainers and a 300s timeout |
| `make vet`            | Run `go vet`                                       |
| `make sqlc`           | Regenerate type-safe SQL query wrappers via SQLC   |
| `make sqlc-check`     | Verify if committed SQLC generated code drifted    |
| `make compose-up`     | Start local PostgreSQL with Docker Compose          |
| `make compose-down`   | Stop local PostgreSQL Compose services              |
| `make local-setup`    | Start/wait for local PostgreSQL, run migrations, seed demo auth user |
| `make local-reset`    | Delete local PostgreSQL volume, recreate DB, migrate, and seed |
| `make smoke-auth`     | Run the local auth smoke test against the API        |
| `make postman-test`   | Run Newman black-box smoke tests against a running API |
| `make smoke-local`    | Start local deps/API, run Newman smoke tests, then stop the API |
| `make migrate-up`     | Run goose migrations up (requires local DATABASE_URL)|
| `make migrate-down`   | Run goose migrations down (requires local DATABASE_URL)|
| `make seed`           | Create or update the local demo auth user           |
| `make openapi`        | Regenerate `openapi/openapi.json`                   |
| `make openapi-check`  | Regenerate and fail if the committed file drifted   |
| `make openapi-types`  | Regenerate TypeScript types from `openapi/openapi.json` |
| `make openapi-types-check` | Regenerate and fail if committed TypeScript types drifted |

## Database Migrations & Codegen

This project uses `goose` for schema migrations and `sqlc` for type-safe query generation, managed natively by the Go toolchain (via `go tool`).

### Schema Migrations
All database schema changes must be written as Goose SQL migration files inside the `migrations/` directory:
- Apply migrations locally: `DATABASE_URL=postgres://... make migrate-up`
- Roll back migrations: `DATABASE_URL=postgres://... make migrate-down`
- Seed the local demo auth user after migrations: `DATABASE_URL=postgres://... make seed`

For deployment preflight checks, production safety policy, and the `00007` project-name unique index runbook, see the [Operations Guide](docs/operations.md#database--migrations).

### Query Codegen
All SQL queries live in `internal/store/sql/` and are compiled into Go code inside `internal/store/query/` via SQLC:
- Regenerate query wrappers: `make sqlc`
- Manual modifications to the generated `internal/store/query/` package are prohibited.
- CI/CD will fail if the committed query wrappers are out of sync with the SQL files.

## OpenAPI

A static OpenAPI 3.1 spec is generated from the route registry and committed to `openapi/openapi.json` so frontend clients can generate types without running the server.

Regenerate after any route or schema change:

```sh
make openapi
```

CI will fail if the committed spec is out of date.

### Frontend Contract Consumption

The backend commits two frontend-facing contract artifacts:

- `openapi/openapi.json` — the OpenAPI 3.1 source of truth exported from Huma route registration.
- `openapi/typescript/schema.ts` — generated TypeScript types for frontend consumers.

Regenerate both after any API route, request body, response body, or error contract change:

```sh
make openapi
make openapi-types
```

`make check` runs `make openapi-check` and `make openapi-types-check`, so API contract drift fails CI.

The TypeScript generator defaults to the pinned `openapi-typescript` package through Bun when available, with an `npx` fallback. You can override it when needed:

```sh
OPENAPI_TYPESCRIPT="npx --yes openapi-typescript@7.13.0" make openapi-types
```

The wow-dashboard and Vite starters should treat these artifacts as the contract source for API client types. Prefer copying or generating from `openapi/typescript/schema.ts`, or running the same generator against `openapi/openapi.json`, instead of hand-writing duplicate request and response interfaces in the frontend.

> **Note:** The core business service layer (`internal/auth/service`) backs the HTTP auth endpoints and provides registration (`SignUp`), credentials validation with timing attack mitigation (`SignIn`), and session checks (`CurrentUser`).

## Password Hashing

Password hashing uses **Argon2id** via the `internal/auth/password` package with OWASP-baseline parameters (19 MiB memory, 2 iterations, parallelism 1). All password hashing and verification must go through `password.Hash` and `password.Verify` — never store or compare passwords by other means.

## JWT Authentication

JWT access token management is handled via the `internal/auth/token` package. It provides symmetric HS256-signed tokens using `github.com/golang-jwt/jwt/v5`.

Key features:
- **HS256 Only**: Enforces symmetric HMAC-SHA256 signature verification.
- **Access Token Issuance**: Issued via `IssueAccessToken(userID string)` with essential registered claims (`sub`, `iss`, `aud`, `iat`, `nbf`, `exp`).
- **Access Token Verification**: Verified via `VerifyAccessToken(raw string)` which strictly validates the signature, issuer, audience, expiration, and `iat` (issued-at) claims.
- **Custom Clock Support**: Supports clock injection (`WithClock`) for deterministic, sleep-free testing of token expiry.
- **Security Invariants**:
  - The signing secret must be at least 32 characters long.
  - Using the default development secret in production environment (`ENV=production`) is strictly blocked at startup.
  - Parsing and verification errors do not leak raw token values, secrets, or internal parser details.

## Docker

Build a production image:

```sh
docker build -t wow-dashboard-api:local .
```

Run the container (adjust environment variables for your deployment):

```sh
docker run -d \
  --name wow-dashboard-api \
  -p 7272:7272 \
  -e ENV=production \
  -e 'DATABASE_URL=postgres://user:pass@host:5432/dbname?sslmode=require' \
  -e JWT_ACCESS_SECRET=prod-token-signing-key-9f7c2a8b4e6d1c0f \
  -e CORS_ALLOWED_ORIGINS=https://app.example.com \
  -e REFRESH_TOKEN_COOKIE_SECURE=true \
  wow-dashboard-api:local
```

The Dockerfile uses a multi-stage build with `CGO_ENABLED=0` for a static binary and runs as a non-root user on `distroless/static-debian12:nonroot`. Port 7272 is exposed by default.

### Production Configuration

`ENV=production` validates configuration at startup and refuses to start with
unsafe defaults. See the [Operations Guide](docs/operations.md#configuration-checks)
for the required variables and the full validation rule list.

### Health Checks in Containers

- **Liveness probe**: `GET /healthz` — returns `200` with `{"status":"ok"}`; no dependencies checked.
- **Readiness probe**: `GET /readyz` — returns `200` with `{"status":"ready"}` after confirming PostgreSQL connectivity; returns `503` when the database is unreachable.

In Kubernetes or Docker Compose, configure `/healthz` as the liveness probe and `/readyz` as the readiness probe. Do not use `/readyz` as a liveness probe — a temporary database outage should stop new traffic, not force a restart loop.

### Database Migrations

The production container ships no migration tooling — run migrations as a
separate step (CI job, init container, or operator action) before the API
receives traffic; it never auto-migrates. See the
[Operations Guide](docs/operations.md#database--migrations) for the runbook and
the migration `00007` preflight.

## CI

GitHub Actions uses layered checks so pull requests stay stable while deeper black-box smoke coverage remains available on demand:

| Layer | Command | When it runs | Purpose |
|-------|---------|--------------|---------|
| Main CI gate | `make check` | Pushes to `main` and pull requests | Formatting, vet, SQLC drift, Go tests, integration tests, OpenAPI JSON drift, and generated TypeScript type drift |
| Image smoke | `docker build -t wow-dashboard-api:ci .` | Pushes to `main` and pull requests | Verifies the production Docker image still builds; the image is not pushed |
| Local black-box acceptance | `make smoke-local` | Manual GitHub Actions dispatch or local developer run | Starts Docker Compose PostgreSQL, migrates/seeds the local database, runs the API, executes Newman, then stops the API |
| Existing API collection | `make postman-test` | Local/manual when an API is already running | Runs the Postman/Newman collection against the configured `POSTMAN_BASE_URL` |

The default PR/push workflow is [`.github/workflows/ci.yml`](.github/workflows/ci.yml). The local smoke harness is intentionally separate in [`.github/workflows/smoke-local.yml`](.github/workflows/smoke-local.yml) and can be run from the GitHub Actions **Local Smoke** workflow via **Run workflow**.
