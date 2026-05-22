# Operations Guide

Deployment, configuration, migration, and release-gate reference for the API.
For runtime configuration variables and their defaults, see the Configuration
table in [`README.md`](../README.md); this document covers how to operate the
service safely, not every individual variable.

## Readiness Levels

Move down the list and stop at the first level whose conditions are not met.

### Local Development Ready

The bar before handing the API to frontend or new CRUD-module work.

- `make check` passes (formatting, vet, sqlc drift, tests, integration tests,
  OpenAPI/TypeScript drift).
- `make smoke-local` passes against disposable local PostgreSQL data.
- Local `.env` values are development-only and are not staged or committed.

### Staging Ready

The bar before deploying to a shared non-production environment.

- Everything in **Local Development Ready** holds.
- `docker build -t wow-dashboard-api:local .` succeeds from a clean checkout.
- `ENV=staging`, a real staging `DATABASE_URL`, and no reused local secrets.
- Migrations run as a separate step before the API container starts (see
  [Database & Migrations](#database--migrations)).
- Staging CORS origins are exact `https://` frontend origins; no wildcards.

### Production Ready

The bar before a production rollout proceeds.

- Everything in **Staging Ready** holds, and staging already ran the same
  migration set and API image intended for production.
- `ENV=production` startup validation passes with production values.
- `DATABASE_URL` points at the approved production PostgreSQL instance — not a
  loopback, local Compose, developer, or staging database.
- `JWT_ACCESS_SECRET` is strong, private, and not a placeholder or reused
  local secret.
- Refresh-token cookies are `HttpOnly`, `Secure`, and use a SameSite mode
  compatible with the frontend deployment.
- Credentialed CORS allows only exact owned `https://` origins.
- Production environment variables were reviewed by a second engineer, with
  attention to CORS, cookie, database, and token settings.
- A production database backup exists with a named owner before migrations run.
- A release operator verified `/healthz`, `/readyz`, and a black-box smoke run
  after deployment.

## Configuration Checks

`ENV=production` enforces the rules below at startup and refuses to start on a
violation. Confirm each before staging or production:

- `JWT_ACCESS_SECRET` — at least 32 characters, private, generated for this
  environment, free of placeholder words (`change-me`, `changeme`, `dev-only`,
  `example`, `secret`).
- `JWT_ACCESS_TOKEN_TTL_SECONDS` — between 60 and 3600 seconds.
- `CORS_ALLOWED_ORIGINS` — exact owned `https://` origins only; no `*`
  wildcards, loopback hosts, or empty entries. Public shared-hosting wildcards
  such as `*.vercel.app` are rejected for credentialed CORS.
- `REFRESH_TOKEN_COOKIE_SECURE` — must be `true` (auto-defaults to `true` when
  unset in production; an explicit `false` blocks startup).
- `REFRESH_TOKEN_COOKIE_SAMESITE` — `none` requires `REFRESH_TOKEN_COOKIE_SECURE=true`.
- `DATABASE_URL` — non-empty, points at the approved instance, and uses the
  provider's required TLS mode (e.g. `sslmode=require`).
- All second-based timeouts are `> 0`; `0 < DB_MIN_CONNS <= DB_MAX_CONNS`.
- `.env`, real database URLs, and production secrets are never committed —
  `.env.example` is the only committed env template.

## Database & Migrations

- The API container does **not** run migrations automatically. Apply them as a
  separate deployment step (CI job, init job, or operator workstation) before
  the API container receives traffic.
- Back up the production database before running production migrations.
- Run schema-changing migrations in staging first, with production-like data
  when possible.
- Stop the deployment if a migration preflight query reports unsafe data.

### Runbook

```sh
# local / staging
DATABASE_URL=postgres://... make migrate-up
DATABASE_URL=postgres://... make migrate-down   # rollback

# production container has no migration tooling — run goose directly
go tool goose -dir migrations postgres "$DATABASE_URL" up
```

### Migration 00007 — projects owner/name unique index

`migrations/00007_add_projects_owner_name_unique_index.sql` creates
`UNIQUE (owner_user_id, name)`. The index build **fails** if any owner already
has more than one project with the same `name` (archived projects still
reserve their names). A fresh database created from migration `00006` has no
duplicates; an existing populated database must be checked first.

Preflight — run before applying `00007` to any shared, staging, or production
database. Expected result: no rows.

```sql
SELECT owner_user_id, name, COUNT(*) AS duplicate_count
FROM projects
GROUP BY owner_user_id, name
HAVING COUNT(*) > 1
ORDER BY duplicate_count DESC, owner_user_id, name;
```

If rows are returned, stop. Do not rely on the migration to clean data:

- Export the affected rows for review; never auto-delete project data.
- A product/business owner confirms which project keeps the original name.
- Remediate by renaming or by archive-and-merge after review, leaving each
  owner exactly one project per name regardless of status.
- Record what changed, then rerun the preflight before applying the migration.

Rollback drops `idx_projects_owner_name_unique` but does **not** restore any
data renamed, archived, or merged before the migration — keep the backup and a
record of manual remediation steps.

Future unique-index migrations must ship their own preflight SQL, duplicate-data
policy, and rollback notes in this document.

## Security & Auth Checks

- Access token TTL is short and within the production validation window.
- Refresh tokens are delivered only through the `HttpOnly` refresh cookie and
  cleared on sign-out. A replayed, already-revoked refresh token revokes its
  whole token family.
- Sign-in and sign-up keep application-level rate limiting enabled unless a
  reviewed stronger edge control replaces it.
- The application does not trust `X-Forwarded-For` / `X-Real-IP` by default.
  Rate limiting keys off the socket remote address; behind a proxy, enforce
  abuse limits at the trusted edge or add a reviewed trusted-proxy config first.
- Credentialed CORS is restricted to exact owned origins; wildcard-matched
  origins never receive `Access-Control-Allow-Credentials`.
- Admin-only endpoints stay admin-only: `GET /api/users`, `GET /api/users/{id}`,
  `GET /api/system-events`.
- Owner-scoped endpoints enforce owner scope in SQL, not only in handlers.
- New failure-audit producers follow [`audit-policy.md`](audit-policy.md).

## Observability & Operations

- `GET /healthz` — liveness; process-only, no dependency checks.
- `GET /readyz` — readiness; checks PostgreSQL connectivity, returns `503` on
  outage. Never configure `/readyz` as a liveness probe — a temporary database
  outage should stop new traffic, not force a restart loop.
- Production logs are structured JSON via `slog`. Every request log and error
  response carries a `request_id`. Request/response headers are never logged;
  sensitive query parameters are redacted.
- The process handles `SIGINT`/`SIGTERM` with graceful shutdown: it stops
  accepting connections, drains in-flight requests until
  `HTTP_SHUTDOWN_TIMEOUT_SECONDS`, then closes the PostgreSQL pool.
- After deploy, run a black-box check: `make postman-test` against the deployed
  API, or `make smoke-local` for a local release rehearsal.

## Standard Verification Commands

```sh
make check                                  # full gate
make smoke-local                            # local black-box acceptance
docker build -t wow-dashboard-api:local .   # production image builds
```

When API routes, request/response bodies, status codes, or error shapes
change, regenerate and commit the contract artifacts in the same change:

```sh
make sqlc          # if SQL files or migrations changed
make openapi
make openapi-types
```

## Non-Goals

Future evolution items, not blockers for the current baseline:

- Metrics and distributed tracing.
- External secret-manager integration.
- Distributed locks and job queues.
- Full RBAC or a policy engine.
- Refresh-token device/session management UI.
