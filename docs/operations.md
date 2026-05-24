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

## Background Worker

Asynchronous work (transactional emails, exports, periodic cleanups) runs in
`cmd/worker`, a separate process that consumes from a Postgres-backed
[River](https://riverqueue.com/) queue. The same `DATABASE_URL` is shared
with the API; no extra infrastructure is required.

- Apply River's schema once per database (it is idempotent):

  ```sh
  DATABASE_URL=postgres://... make migrate-river
  ```

  `make local-setup` runs this automatically alongside the goose migrations.

- Run a worker locally:

  ```sh
  DATABASE_URL=postgres://... make worker
  ```

- Smoke-test the pipeline by enqueueing a no-op `ping` job:

  ```sh
  DATABASE_URL=postgres://... make queue-ping MSG="hello"
  ```

  The worker logs `ping job processed` with the job id and the message.

In production, deploy `cmd/worker` as a separate Deployment/Service from
`cmd/api` so worker capacity and HTTP capacity scale independently. Both
images run from the same Go module, so a single build produces both
binaries (`go build ./cmd/api ./cmd/worker`). The worker honours
`SIGINT`/`SIGTERM` with a 30-second drain.

New job types live in `internal/jobs/` and are wired into the worker via
`jobs.RegisterAll(workers, deps)`. The pattern: declare an `Args` struct that
implements `Kind() string`, declare a `Worker` that embeds
`river.WorkerDefaults`, add any new collaborator to `jobs.Dependencies`, and
register the worker in `RegisterAll`.

## Email

Transactional email (password reset, email verification) is sent through a
two-stage pipeline so the API never blocks on SMTP:

1. The API process holds a River insert-only client wrapped as
   `jobs.AsyncEmailSender` and passes it to the auth service via
   `authservice.WithEmailSender(...)`. Each `Send` enqueues a `send_email` job
   on the default queue and returns immediately.
2. `cmd/worker` runs `SendEmailWorker`, which delivers each job through the
   transport selected by `email.New(cfg)` — `LogSender` when `EMAIL_SMTP_HOST`
   is empty, otherwise a real `SMTPSender` (`github.com/wneessen/go-mail`).

Configuration variables (all read at process start):

| Variable                | Default                          | Notes                                                      |
| ----------------------- | -------------------------------- | ---------------------------------------------------------- |
| `EMAIL_SMTP_HOST`       | _empty_ (LogSender)              | Required in production.                                    |
| `EMAIL_SMTP_PORT`       | `0` → defaults per TLS mode      | `none=25`, `starttls=587`, `tls=465` when left at `0`.     |
| `EMAIL_SMTP_USERNAME`   | _empty_                          | Enables SMTP AUTH PLAIN when set.                          |
| `EMAIL_SMTP_PASSWORD`   | _empty_                          | Paired with `EMAIL_SMTP_USERNAME`.                         |
| `EMAIL_SMTP_TLS`        | `starttls`                       | One of `none`, `starttls`, `tls`.                          |
| `EMAIL_FROM_ADDRESS`    | `noreply@wow-dashboard.test`     | Required (cannot be empty).                                |
| `EMAIL_FROM_NAME`       | `WOW Dashboard`                  | Optional display name.                                     |

### Local development with Mailpit

`compose.yaml` ships an `axllent/mailpit` service that catches every SMTP
message and exposes a web UI:

```sh
docker compose up -d mailpit
# .env.example already points at Mailpit:
#   EMAIL_SMTP_HOST=localhost
#   EMAIL_SMTP_PORT=1025
#   EMAIL_SMTP_TLS=none
make worker     # delivers via Mailpit
open http://localhost:8025
```

Without Mailpit (or any other relay), leave `EMAIL_SMTP_HOST` empty; the
worker logs each message instead of sending it so the auth flows still work.

### Production

Point `EMAIL_SMTP_HOST` / `PORT` / `USERNAME` / `PASSWORD` / `TLS` at the
managed relay (SendGrid, AWS SES, Postmark, etc.) and set a verified
`EMAIL_FROM_ADDRESS`. The API process refuses to start in `ENV=production`
when `EMAIL_SMTP_HOST` is empty.

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

- A bundled observability stack (collector, dashboards). The app exposes
  Prometheus metrics at `/metrics` and exports OpenTelemetry traces, but
  running the collector and dashboards is left to the deployment.
- External secret-manager integration.
- Distributed locks and job queues.
- An attribute-based access-control (ABAC) policy engine. Role-based access
  control with dynamic, database-backed roles is implemented.
- Refresh-token device/session management UI.
