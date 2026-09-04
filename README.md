# wow-dashboard-api

English | [简体中文](README.zh-CN.md)

A Go API service for the Minimal Starter dashboard projects, designed for Next.js and Vite frontends. It provides typed HTTP endpoints, PostgreSQL persistence, and a committed OpenAPI contract so frontend clients do not have to guess request and response shapes.

**Stack:** Go 1.27.1 · Chi · Huma v2 · PostgreSQL · pgx · sqlc · goose · River

## Features

- Authentication: registration, sign-in, HttpOnly access/refresh cookies, refresh rotation, password reset, and email verification.
- Account security: TOTP MFA, recovery codes, active-session management, recent security activity, and email/in-app alerts for password changes, MFA changes, and new-device sign-ins.
- Access control: database-backed roles, a code-defined permission catalog, and administrator impersonation.
- Business modules: users, roles, projects with owner/editor/viewer access, notifications, and audit events.
- Background jobs: PostgreSQL-backed River workers for email delivery and scheduled retention cleanup.
- Operations: structured logs, readiness checks, Prometheus metrics, OpenTelemetry tracing, and a non-root multi-architecture container image.

This repository contains the backend only. It does not include a dashboard UI. PostgreSQL is required; Redis and the local observability stack are optional.

## Quick Start

### Requirements

- Go 1.27.1 or later in the 1.27 release line; the exact project version is in [go.mod](go.mod).
- Docker with a running daemon and Docker Compose v2 for local PostgreSQL, Mailpit, and integration tests.
- Air for API live reload.
- Bun, or Node.js with npm, for generated TypeScript contract checks. Newman smoke tests use `npx`.

Run the following from the repository root.

### 1. Prepare the environment

Create your local configuration if it does not already exist, then review its values:

```sh
test -f .env || cp .env.example .env
go mod download
go install github.com/air-verse/air@latest
```

Ensure Air is on your `PATH`. [`.env.example`](.env.example) is for local development only; do not reuse its credentials or keys in production.

### 2. Prepare the database and start the API

```sh
make local-setup
docker compose up -d mailpit
make dev
```

`make local-setup` starts PostgreSQL, waits for it, applies goose and River migrations, and seeds the demo administrator. The default local database URL matches `compose.yaml`; override `LOCAL_DATABASE_URL` only when intentionally using another development database.

Air loads `.env` and `.env.local`. The API listens on [http://localhost:7272](http://localhost:7272):

| URL | Purpose |
| --- | --- |
| [`/docs`](http://localhost:7272/docs) | Interactive API documentation |
| [`/openapi`](http://localhost:7272/openapi) | Runtime OpenAPI JSON |
| [`/healthz`](http://localhost:7272/healthz) | Liveness |
| [`/readyz`](http://localhost:7272/readyz) | Readiness, including PostgreSQL |
| [Mailpit](http://localhost:8025) | Captured development emails |

The seeded administrator is `demo@wow-dashboard.test` with password `@Password`. Use it only with disposable local data. Re-running the seed updates the demo account, including its password and administrator role.

### 3. Start the email/background worker

In another terminal, pass the local settings explicitly:

```sh
DATABASE_URL='postgres://wow_dashboard:wow_dashboard@localhost:5432/wow_dashboard_api?sslmode=disable' \
EMAIL_SMTP_HOST=localhost \
EMAIL_SMTP_PORT=1025 \
EMAIL_SMTP_TLS=none \
make worker
```

Unlike Air, `make worker` and direct `go run` commands do not automatically load `.env`. Supply the same relevant environment settings as the API, especially when using a custom database or production configuration.

The API enqueues email jobs; the worker delivers them. Without a worker, queued verification, password-reset, and security-alert emails are not delivered. The worker also purges expired tokens and old audit events.

### 4. Verify the running service

```sh
make smoke-auth
# Optional: Postman/Newman checks against the running API
make postman-test
```

For a self-contained black-box run, `make smoke-local` prepares the local database, starts its own API process, runs Newman, and stops that API process. It leaves PostgreSQL running. See [postman/README.md](postman/README.md).

**Destructive command:** `make local-reset` deletes the local PostgreSQL volume and its data before recreating the database. Do not use it against data you need to preserve.

## Frontend Integration

Use the committed [OpenAPI JSON](openapi/openapi.json) and [generated TypeScript types](openapi/typescript/schema.ts) as the contract. For the Next.js starter, set:

```dotenv
NEXT_PUBLIC_SERVER_URL=http://localhost:7272
```

A Vite frontend should use its own API base-URL environment key.

### Cookie-based authentication

Successful session creation returns `{ "user": ... }` and sets two HttpOnly cookies. **Access tokens are not returned in JSON and must not be stored in browser localStorage.**

```ts
const response = await fetch(baseURL + '/api/auth/me', {
  credentials: 'include',
});
// Axios: axios.create({ baseURL, withCredentials: true })
```

- If sign-in returns `mfaRequired: true`, submit an authenticator/recovery code to `POST /api/auth/mfa/verify` with credentials before treating the user as signed in.
- On an expired-session `401`, call `POST /api/auth/refresh` with credentials and retry the original request once. If refresh fails, return to sign-in.
- Sign-out clears both cookies. Refresh rotation replaces the refresh token.
- The JWT expires after 15 minutes by default. The access cookie's MaxAge is longer: it follows the refresh-session lifetime so a frontend can still attempt refresh after JWT expiry. Cookie presence alone is not proof of authentication.
- Non-browser clients may also present an already-issued access token via `Authorization: Bearer <token>`; the explicit header takes precedence over the access cookie.

Configure `CORS_ALLOWED_ORIGINS` for the exact frontend origins. Cookie-authenticated state-changing requests are CSRF-checked: a different origin, **including a same-site sibling subdomain**, must match the Origin allowlist. Cookie clients without browser Fetch Metadata should send an allowed `Origin`.

Truly cross-site cookies require `SameSite=None` and `Secure=true`, and remain subject to browser third-party-cookie policies. A shared cookie domain is not required merely to call an API on another subdomain; only set `ACCESS_TOKEN_COOKIE_DOMAIN` when deliberately sharing the access cookie with trusted hosts.

See the [Frontend Integration Guide](docs/frontend-integration.md) for additional client setup.

## API Reference

The current contract contains 45 operations across 35 paths. Request fields, status codes, validation, and error schemas are defined in [openapi/openapi.json](openapi/openapi.json), not in hand-maintained frontend DTOs.

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/auth/sign-up` | Register; return 201 and establish a session. |
| `POST` | `/api/auth/sign-in` | Sign in; may require the MFA step. |
| `POST` | `/api/auth/refresh` | Rotate refresh token and issue new session cookies. |
| `POST` | `/api/auth/sign-out` | Revoke current refresh token and clear both cookies. |
| `POST` | `/api/auth/sign-out-others` | Revoke other sessions while retaining the current one. |
| `GET` | `/api/auth/me` | Read profile, roles, permissions, and MFA/verification state. |
| `PATCH` | `/api/auth/me` | Update your profile. |
| `POST` | `/api/auth/change-password` | Change password and revoke refresh sessions. |
| `POST` | `/api/auth/forgot-password` | Request a reset email without revealing account existence. |
| `POST` | `/api/auth/reset-password` | Reset password using a one-time token. |
| `POST` | `/api/auth/verify-email` | Confirm email using a one-time token. |
| `POST` | `/api/auth/resend-verification` | Request another verification email. |
| `POST` | `/api/auth/mfa/setup` | Begin TOTP enrollment. |
| `POST` | `/api/auth/mfa/confirm` | Enable MFA and reveal recovery codes once. |
| `POST` | `/api/auth/mfa/verify` | Complete an MFA-gated sign-in. |
| `DELETE` | `/api/auth/mfa` | Disable MFA with required verification. |
| `GET` | `/api/auth/sessions` | List your active sessions and device information. |
| `DELETE` | `/api/auth/sessions/{id}` | Revoke one of your session families. |
| `GET` | `/api/auth/security-activity` | Read your recent security events. |
| `POST` | `/api/auth/impersonate/{targetUserId}` | Start administrator impersonation. |
| `POST` | `/api/auth/impersonate/stop` | Return to the administrator session. |
| `GET` | `/api/users` | List users with pagination and filters. |
| `GET` | `/api/users/{id}` | Read a user. |
| `PATCH` | `/api/users/{id}` | Change status or replace role assignments. |
| `GET` | `/api/roles` | List roles. |
| `POST` | `/api/roles` | Create a custom role. |
| `GET` | `/api/roles/{id}` | Read a role. |
| `PATCH` | `/api/roles/{id}` | Update a custom role. |
| `DELETE` | `/api/roles/{id}` | Delete an unassigned custom role. |
| `GET` | `/api/permissions` | List assignable permissions. |
| `GET` | `/api/projects` | List owned and shared projects. |
| `POST` | `/api/projects` | Create a project. |
| `GET` | `/api/projects/{id}` | Read an accessible project. |
| `PATCH` | `/api/projects/{id}` | Update a project as owner/editor. |
| `DELETE` | `/api/projects/{id}` | Archive a project as owner; no physical deletion. |
| `GET` | `/api/projects/{id}/members` | List project members. |
| `POST` | `/api/projects/{id}/members` | Owner grants a registered user access by email. |
| `PATCH` | `/api/projects/{id}/members/{userId}` | Owner changes a member's role. |
| `DELETE` | `/api/projects/{id}/members/{userId}` | Owner removes a member. |
| `GET` | `/api/system-events` | Read the system audit log. |
| `GET` | `/api/notifications` | Read your notifications and unread count. |
| `POST` | `/api/notifications/{id}/read` | Mark your notification as read. |
| `POST` | `/api/notifications/read-all` | Mark all your notifications as read. |
| `GET` | `/healthz` | Liveness, independent of external dependencies. |
| `GET` | `/readyz` | Readiness with a PostgreSQL ping; 503 on failure. |

### Authorization and resource behavior

| Permission | Capability |
| --- | --- |
| `users:read` | List/view users |
| `users:manage` | Change user status and role assignments |
| `roles:read` | Read roles and the permission catalog |
| `roles:manage` | Create/update/delete custom roles |
| `system_events:read` | Read system-wide audit events |
| `projects:create` | Create projects |

The built-in `admin` role holds the reserved `*` permission; new registrations receive the `user` role with `projects:create`. System roles cannot be changed through the API. Custom roles are database-backed; effective permissions are their union and are resolved from the database when authenticating the request.

- Project access is separate from global RBAC: owners and members may read, owners/editors may update, and only owners may archive or manage membership. SQL queries enforce accessible-resource scope.
- Project names are unique per owner and case-sensitive after trimming. Archived projects retain their names. Repeated archive requests succeed.
- User updates cannot target the acting administrator's own account. Custom roles cannot be deleted while assigned to users.
- User/project lists use `page`, `pageSize`, and `search`; default page size is 20, maximum 100. Audit, security-activity, and notification lists use cursor pagination with `limit` and `nextCursor`. Consult each operation for filters.
- Notifications and recent security activity are scoped to the current user. Impersonation has additional restrictions on account-security operations.
- Email verification is tracked and returned to the client; it is not currently required to sign in.

### Errors and audit events

API errors share a stable envelope:

```json
{
  "code": "not_found",
  "message": "The requested resource was not found.",
  "request_id": "request-id"
}
```

Validation errors may include `details` entries with `field` and `message`. Empty item arrays are returned as `[]`, not `null`. Unexpected errors use a safe client message while their underlying cause is logged server-side.

Audit producers record stable event types and safe metadata in `system_events`. Audit recording is best-effort and does not replace the original operation result. See [Audit Policy](docs/audit-policy.md) for event taxonomy and metadata rules.

## Configuration

Runtime settings are parsed and validated by [internal/config/config.go](internal/config/config.go). The table shows **runtime defaults when variables are unset**, not every value in `.env.example`. That local example explicitly sets a PostgreSQL URL and Mailpit transport. Seconds-based settings are in seconds unless the name says otherwise.

| Variable | Runtime default | Purpose |
| --- | --- | --- |
| `APP_NAME` | `wow-dashboard-api` | Name used in logs and email notifications. |
| `PORT` | `7272` | HTTP listen port. |
| `ENV` | `development` | development, staging, or production. |
| `APP_BASE_URL` | `http://localhost:3000` | Frontend URL used in password-reset and verification links. |
| `LOG_FORMAT` | `(auto)` | text outside production; json in production. |
| `LOG_LEVEL` | `info` | debug, info, warn, or error. |
| `READ_TIMEOUT_SECONDS` | `15` | HTTP read timeout. |
| `WRITE_TIMEOUT_SECONDS` | `15` | HTTP write timeout. |
| `IDLE_TIMEOUT_SECONDS` | `60` | HTTP idle timeout. |
| `HTTP_SHUTDOWN_TIMEOUT_SECONDS` | `10` | Graceful HTTP shutdown deadline. |
| `REQUEST_BODY_MAX_BYTES` | `1048576` | Transport body cap; 0 disables this outer cap, not Huma's per-operation limit. |
| `CORS_ALLOWED_ORIGINS` | `http://localhost:3000,http://localhost:5173,http://localhost:8082,http://localhost:8083,http://localhost:8084,http://localhost:8085` | Comma-separated frontend origins; production requires exact HTTPS origins. |
| `DATABASE_URL` | `(empty)` | Required to start API/worker. The local Compose URL is provided in .env.example, not as a runtime default. |
| `DB_MAX_CONNS` | `10` | Maximum pool connections. |
| `DB_MIN_CONNS` | `1` | Minimum pool connections; 0 through DB_MAX_CONNS. |
| `DB_MAX_CONN_LIFETIME_SECONDS` | `1800` | Maximum connection lifetime. |
| `DB_MAX_CONN_IDLE_TIME_SECONDS` | `300` | Maximum idle connection lifetime. |
| `DB_HEALTH_TIMEOUT_SECONDS` | `3` | Database readiness ping timeout. |
| `DB_STATEMENT_TIMEOUT_SECONDS` | `30` | PostgreSQL per-statement timeout. |
| `DB_HEALTH_CHECK_PERIOD_SECONDS` | `30` | Pool health-check interval. |
| `AUTH_RATE_LIMIT_ENABLED` | `true` | Enable shared throttling for sensitive auth routes. |
| `AUTH_RATE_LIMIT_REQUESTS` | `10` | Requests allowed per IP per window. |
| `AUTH_RATE_LIMIT_WINDOW_SECONDS` | `60` | Auth rate-limit window. |
| `AUTH_RATE_LIMIT_BURST` | `5` | Immediate burst capacity for the in-memory limiter. |
| `AUTH_MAX_FAILED_LOGIN_ATTEMPTS` | `10` | Consecutive failed sign-ins before account lockout. |
| `AUTH_ACCOUNT_LOCKOUT_SECONDS` | `900` | Temporary account lockout duration. |
| `REDIS_URL` | `(empty)` | Optional Redis-backed shared auth limiter; empty uses local memory. |
| `JWT_ACCESS_SECRET` | `dev-only-change-me-min-32-characters` | HS256 signing key; at least 32 characters; replace for production. |
| `MFA_ENCRYPTION_KEY` | `dev-only-change-me-mfa-encryption-key-32+` | TOTP encryption key; at least 32 characters; production requires a distinct non-placeholder value. |
| `JWT_ISSUER` | `wow-dashboard-api` | Expected JWT issuer. |
| `JWT_AUDIENCE` | `wow-dashboard` | Expected JWT audience. |
| `JWT_ACCESS_TOKEN_TTL_SECONDS` | `900` | JWT lifetime; production accepts 60–3600 seconds. |
| `REFRESH_TOKEN_TTL_SECONDS` | `7776000` | 90-day refresh lifetime, renewed on rotation; also sets access-cookie MaxAge. |
| `REFRESH_TOKEN_COOKIE_NAME` | `wow_dashboard_refresh_token` | HttpOnly refresh cookie, Path=/api/auth. |
| `REFRESH_TOKEN_COOKIE_SECURE` | `false` | Defaults to true if unset in production; explicitly false is rejected there. |
| `REFRESH_TOKEN_COOKIE_SAMESITE` | `lax` | lax, strict, or none; none requires Secure. |
| `ACCESS_TOKEN_COOKIE_NAME` | `wow_dashboard_access_token` | HttpOnly access cookie, Path=/; must differ from refresh cookie name. |
| `ACCESS_TOKEN_COOKIE_SECURE` | `false` | Defaults to true if unset in production; explicitly false is rejected there. |
| `ACCESS_TOKEN_COOKIE_SAMESITE` | `lax` | lax, strict, or none; none requires Secure. |
| `ACCESS_TOKEN_COOKIE_DOMAIN` | `(empty)` | Host-only by default; optional parent domain for deliberate cookie sharing. |
| `EMAIL_SMTP_HOST` | `(empty)` | Empty selects development LogSender; production requires an SMTP host. |
| `EMAIL_SMTP_PORT` | `0` | 0 selects the TLS-mode default: none=25, starttls=587, tls=465. |
| `EMAIL_SMTP_USERNAME` | `(empty)` | SMTP authentication username. |
| `EMAIL_SMTP_PASSWORD` | `(empty)` | SMTP authentication password. |
| `EMAIL_SMTP_TLS` | `starttls` | none, starttls, or tls; local Mailpit uses none. |
| `EMAIL_FROM_ADDRESS` | `noreply@wow-dashboard.test` | Sender address; use your verified sender in production. |
| `EMAIL_FROM_NAME` | `WOW Dashboard` | Sender display name. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `(empty)` | OTLP/HTTP collector URL; empty disables trace export. |
| `METRICS_ADDR` | `(empty)` | Optional separate metrics listener. Without it, /metrics is on the API port outside production only. |
| `ENABLE_DOCS` | `true` | Interactive /docs; defaults to false if unset in production. /openapi remains available. |
| `SYSTEM_EVENTS_RETENTION_DAYS` | `90` | Audit retention window used by the worker cleanup job. |

The legacy `SHUTDOWN_TIMEOUT_SECONDS` alias is accepted only when `HTTP_SHUTDOWN_TIMEOUT_SECONDS` is unset.

### Production checklist

- Set `ENV=production`, a real `DATABASE_URL`, a valid HTTPS `APP_BASE_URL`, exact owned HTTPS CORS origins, and an SMTP host/sender.
- Supply high-entropy `JWT_ACCESS_SECRET` and a different `MFA_ENCRYPTION_KEY`; development placeholders are rejected. Rotating the MFA key without migrating stored secrets requires enrolled users to re-enroll.
- Require Secure cookies. Do not reuse `.env.example` unchanged: it explicitly sets both Secure flags to false and enables docs.
- The server rejects invalid timeouts, pool limits, cookie names, and unsupported settings. It does not trust `X-Forwarded-For` or `X-Real-IP` by default.
- Authentication uses Argon2id password hashing, HS256 JWT validation, per-IP throttling, and temporary account lockout. Redis is optional: startup connection failure falls back to local-memory throttling; runtime Redis failures fail open. Enforce independent limits at the trusted edge for public/multi-instance deployments.
- Keep `/metrics` private. In production, use a separate internal `METRICS_ADDR`; `/docs` is off by default, but `/openapi` is still served.
- Run the worker as well as the API. Development `LogSender` logs email bodies, which can contain one-time links; use a real SMTP transport and protect email/queue data in production.

See [Operations Guide](docs/operations.md) and [Deployment Runbook](docs/deployment.md) for operational procedures.

## Project Structure

```text
cmd/
  api/                  HTTP server
  worker/               River background worker
  openapi/              Offline OpenAPI generator
  seed/                 Demo administrator seeder
  smoke-auth/           Running-service auth smoke checks
  river-migrate/        River schema migrations
  queue-ping/           Queue smoke-test producer
  healthcheck/          Container liveness probe
internal/
  app/                  Dependency wiring and process lifecycle
  config/               Typed environment configuration
  http/                 Huma handlers, middleware, errors, pagination
  auth/                 Passwords, tokens, authentication, RBAC
  <resource>/           Domain types and use-case services
  store/sql/            Hand-written SQL
  store/query/          sqlc-generated Go code
  store/<resource>repo/ Repository adapters and transaction boundaries
  jobs/                 Email and retention workers
  securityalerts/       Email and in-app security notifications
  observability/        Tracing and database/queue metrics
migrations/             goose application schema migrations
openapi/                Committed JSON and TypeScript API contracts
observability/          Local Prometheus, Grafana, and Jaeger stack
postman/                Black-box smoke collection
docs/                   Module, integration, and operations guides
```

Handlers validate/map HTTP input and output; services own business decisions; store adapters own PostgreSQL access and transactions. Use the [CRUD Module Guide](docs/crud-module-guide.md) before adding a resource.

## Development and Verification

```sh
make check
```

This runs formatting checks, `go vet`, sqlc drift checks, unit tests, race tests, Testcontainers integration tests, and OpenAPI JSON/TypeScript drift checks. Docker must be available for the integration stage.

| Command | Purpose |
| --- | --- |
| `make fmt` / `make fmt-check` | Format/check Go sources |
| `make vet` | Static analysis |
| `make test` / `make test-race` | Unit tests, optionally with race detection |
| `make test-integration` | Integration-tagged tests; 300-second timeout |
| `make sqlc` / `make sqlc-check` | Generate/check query wrappers |
| `make openapi` / `make openapi-check` | Generate/check OpenAPI JSON |
| `make openapi-types` / `make openapi-types-check` | Generate/check TypeScript types |
| `make migrate-up` / `make migrate-down` | Apply/roll back goose migrations; DATABASE_URL required |
| `make migrate-river` | Apply River schema; DATABASE_URL required |
| `make seed` | Create/update local demo administrator; DATABASE_URL required |
| `make queue-ping MSG=hello` | Enqueue a worker smoke job; DATABASE_URL required |
| `make compose-up` / `make compose-down` | Start PostgreSQL / stop local Compose services |

When Docker is unavailable, run the non-container subset and report integration tests as unverified:

```sh
make fmt-check vet sqlc-check test test-race openapi-check openapi-types-check
```

After SQL or API changes:

```sh
make sqlc
make openapi
make openapi-types
make check
```

Commit generated query code and frontend contracts alongside the changes that require them. Never edit `internal/store/query/` manually. The offline OpenAPI generator does not require PostgreSQL. Type generation uses `openapi-typescript@7.13.0` through Bun with an `npx` fallback.

## Deployment and Observability

```sh
make docker-build
```

The Dockerfile builds four static binaries: `/api`, `/worker`, `/river-migrate`, and `/healthcheck`. The final image runs as non-root on distroless; its default entrypoint is `/api`.

Deploy API and worker separately with consistent configuration. Apply application migrations using goose outside the image, then apply River migrations before starting either process. The image includes the River migration binary but not goose or the application migration files.

`compose.prod.yaml` is a single-host rehearsal template, not a secure ready-to-use production deployment. It contains local database credentials and Mailpit settings; review it and supply all required configuration, including `MFA_ENCRYPTION_KEY`, before use.

- Use `/healthz` for liveness and `/readyz` for readiness. A database outage should remove traffic, not trigger a liveness restart loop.
- The image's HTTP healthcheck applies to the API; disable it for worker and one-shot migration containers.
- API shutdown drains requests up to `HTTP_SHUTDOWN_TIMEOUT_SECONDS`; the worker has a separate 30-second drain.
- Metrics cover HTTP latency/counts, rate-limit rejections, database pool usage, queue state, and Go runtime metrics.
- Set `OTEL_EXPORTER_OTLP_ENDPOINT` to export HTTP/database traces. Logs include request IDs and, when tracing is active, trace/span IDs.

For local dashboards:

```sh
make observability-up
```

See [Observability Guide](docs/observability.md) for Grafana, Prometheus, Jaeger, metrics, and alert rules.

### Continuous integration

| Workflow | Trigger and behavior |
| --- | --- |
| [CI](.github/workflows/ci.yml) | Main pushes and PRs: full `make check` |
| [Security](.github/workflows/security.yml) | Main pushes, PRs, and weekly schedule: `govulncheck` |
| [Container](.github/workflows/container.yml) | PRs: build and Trivy-scan amd64/arm64; main and version-tag pushes also publish multi-arch images to GHCR after successful scans |
| [Smoke](.github/workflows/smoke-local.yml) | Manual dispatch: Newman black-box checks |

Trivy gates on **fixable HIGH/CRITICAL** OS and library vulnerabilities. Passing a scan is not a guarantee that no vulnerabilities exist. Container images are published under `ghcr.io/aklmans/wow-dashboard-api`; PR builds do not publish.

## Documentation

- [Frontend integration](docs/frontend-integration.md)
- [CRUD module development](docs/crud-module-guide.md)
- [Audit policy](docs/audit-policy.md)
- [Operations](docs/operations.md)
- [Deployment](docs/deployment.md)
- [Observability](docs/observability.md)
- [Postman smoke tests](postman/README.md)

## License

Licensed under the [MIT License](LICENSE).

Copyright (c) 2026 Akman.
