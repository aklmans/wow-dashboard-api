# Production Readiness Checklist

This checklist is the release gate for the current API infrastructure baseline.
It is intentionally operational: an Agent or operator should be able to move
down the page and decide whether the service is ready for local use, staging,
or production.

Current infrastructure scope includes Go, chi, Huma v2, sqlc, pgx, auth with
refresh tokens and sign-out, admin-only users and system-events reads,
owner-scoped projects CRUD/archive/conflict behavior, success audit events,
Postman/Newman smoke coverage, `make smoke-local`, Docker/CI, migration
deployment notes, and the audit failure policy.

## Readiness Levels

### Local Development Ready

Use this level before handing the API to frontend or CRUD-module development.

Required conditions:

- `make check` passes on the developer machine or CI.
- `make smoke-local` passes against disposable local PostgreSQL data.
- `openapi/openapi.json` and `openapi/typescript/schema.ts` have no generated
  drift.
- Local `.env` values are development-only and are not staged or committed.
- `/healthz`, `/readyz`, auth, admin users, system events, and projects flows
  match the README and Postman collection.
- Any new CRUD module has tests at service, handler, and store integration
  layers where persistence is involved.

### Staging Ready

Use this level before deploying to a shared non-production environment.

Required conditions:

- Everything in **Local Development Ready** is true.
- `docker build -t wow-dashboard-api:local .` succeeds from a clean checkout.
- Staging configuration sets `ENV=staging`, uses a real staging
  `DATABASE_URL`, and does not reuse local secrets.
- Staging database migrations are run as a separate step before the API
  container starts.
- The migration preflight SQL for migration `00007` returns no rows in staging.
- Staging CORS origins are exact known frontend origins; no wildcard origins.
- Readiness probes use `/readyz`; liveness probes use `/healthz`.
- Post-deploy smoke runs are planned with either `make postman-test` against the
  staging URL or an equivalent CI/manual workflow.

### Production Candidate

Use this level before asking for final production approval.

Required conditions:

- Everything in **Staging Ready** is true.
- Staging has already run the same migration set and API image intended for
  production.
- Production environment variables have been reviewed by another engineer or
  operator, with special attention to CORS, cookie, database, and token settings.
- A production database backup plan exists and has a named owner before
  migrations run.
- The production migration runbook includes the `00007` duplicate project-name
  preflight query and an explicit stop condition if rows are returned.
- Rollback expectations are documented: schema rollback can drop schema changes,
  but it cannot automatically undo manual data cleanup.
- Deployment probes, log collection, and post-release smoke checks are defined.
- Known non-goals in this document are accepted as non-blocking for the starter
  API infrastructure pause point.

### Production Ready

Use this level only when a production rollout can proceed.

Required conditions:

- Everything in **Production Candidate** is true.
- `ENV=production` startup validation passes with production values.
- `DATABASE_URL` points to the approved production PostgreSQL instance, not a
  loopback, local Compose, developer, or staging database.
- `JWT_ACCESS_SECRET` is strong, private, and not a placeholder or reused local
  secret.
- Refresh-token cookies are `HttpOnly`, `Secure`, and compatible with the
  reviewed SameSite/frontend deployment strategy.
- Credentialed CORS allows only exact owned `https://` origins.
- `LOG_FORMAT=json` is used or left to the production default.
- Auth rate limiting is enabled or an approved stronger edge/application
  control is in place.
- Production migrations were run separately from the API container after backup
  and preflight checks.
- A release operator has verified `/healthz`, `/readyz`, and the black-box
  smoke path after deployment.

## Required Commands

Run commands from the repository root unless noted otherwise.

### Standard Verification

```sh
make check
make smoke-local
docker build -t wow-dashboard-api:local .
git diff --exit-code -- openapi/openapi.json openapi/typescript/schema.ts
```

Optional when the API is already running:

```sh
make postman-test
```

For doc-only changes, `git diff --check` is still required. Go tests are not
required unless runtime code or generated artifacts changed, but `make check`
remains the full gate for infrastructure or API behavior changes.

### Migration Preflight SQL

Run this before applying migration
`migrations/00007_add_projects_owner_name_unique_index.sql` in any shared,
staging, or production database:

```sql
SELECT owner_user_id, name, COUNT(*) AS duplicate_count
FROM projects
GROUP BY owner_user_id, name
HAVING COUNT(*) > 1
ORDER BY duplicate_count DESC, owner_user_id, name;
```

Expected result: no rows.

If rows are returned, stop the migration. Export the affected rows for manual
review, decide the approved remediation, record what changed, and rerun the
preflight SQL before migration.

## Configuration Checks

Before staging or production, confirm each item explicitly:

- `ENV` is set to one of `development`, `staging`, or `production`.
  Production must use `ENV=production`.
- `DATABASE_URL` is non-empty and points to the approved PostgreSQL instance.
  Production must not point at `localhost`, `127.0.0.1`, `0.0.0.0`, `[::1]`,
  local Compose, a developer database, or staging.
- Production database URLs use the deployment's required TLS mode, such as
  `sslmode=require` when the provider requires it.
- `JWT_ACCESS_SECRET` is at least 32 characters, private, generated for this
  environment, and free of placeholder words such as `change-me`, `dev-only`,
  `example`, or `secret`.
- `JWT_ACCESS_TOKEN_TTL_SECONDS` is intentionally short. Production startup
  validation requires it to be at least 60 seconds and no more than 3600
  seconds.
- `REFRESH_TOKEN_COOKIE_SECURE=true` in production.
- `REFRESH_TOKEN_COOKIE_SAMESITE` matches the deployment shape:
  - `lax` or `strict` for same-site frontend/API deployments.
  - `none` only when cross-site cookies are required and `Secure=true`.
- `CORS_ALLOWED_ORIGINS` contains exact owned `https://` origins only in
  production. Do not use `*`, loopback, empty entries, or shared-hosting
  wildcards such as `*.vercel.app` or `*.netlify.app` for credentialed CORS.
- `LOG_FORMAT=json` is set or left to the production default.
- HTTP timeout values are positive and reviewed:
  `READ_TIMEOUT_SECONDS`, `WRITE_TIMEOUT_SECONDS`, `IDLE_TIMEOUT_SECONDS`, and
  `HTTP_SHUTDOWN_TIMEOUT_SECONDS`.
- Database pool values are positive and sized for the deployment:
  `DB_MAX_CONNS`, `DB_MIN_CONNS`, `DB_MAX_CONN_LIFETIME_SECONDS`,
  `DB_MAX_CONN_IDLE_TIME_SECONDS`, and `DB_HEALTH_TIMEOUT_SECONDS`.
- Auth rate limiting is enabled and reviewed:
  `AUTH_RATE_LIMIT_ENABLED`, `AUTH_RATE_LIMIT_REQUESTS`,
  `AUTH_RATE_LIMIT_WINDOW_SECONDS`, and `AUTH_RATE_LIMIT_BURST`.
- `.env`, `.env.local`, real database URLs, private keys, tokens, and production
  secrets are not staged or committed. `.env.example` is the only committed env
  template.

## Database And Migration Checks

- Back up production before running production migrations.
- Run schema-changing migrations in staging first, preferably with
  production-like data.
- Run the `00007` duplicate project-name preflight SQL before applying the
  owner/name unique index migration to any shared database.
- If the preflight returns rows, do not rely on the migration to clean data.
  Human-approved data remediation must happen first.
- Rollback for migration `00007` can drop
  `idx_projects_owner_name_unique`, but it cannot automatically restore any
  project names, archives, merges, or other manual data changes performed before
  migration.
- The API container does not automatically migrate the database. Run migrations
  as a separate CI step, init job, or operator action before the API container
  receives traffic.
- Do not log complete production DSNs. Use sanitized database URLs in error
  contexts and operational notes.

## API Contract Checks

- `openapi/openapi.json` is committed and reflects the Huma route registry.
- `openapi/typescript/schema.ts` is committed and generated from the OpenAPI
  artifact.
- `git diff --exit-code -- openapi/openapi.json openapi/typescript/schema.ts`
  passes after any route, request, response, or error-envelope change.
- `make openapi` and `make openapi-types` are run whenever API contracts change.
- Frontend clients and Agents consume generated contract types from
  `openapi/typescript/schema.ts` or regenerate from `openapi/openapi.json`.
  They must not hand-write duplicate DTOs that drift from the backend contract.
- New endpoints remain under `/api/...` unless there is an explicit reviewed
  exception.
- Error responses use the stable `apierror` envelope with `code`, `message`,
  `request_id`, and optional `details`.

## Security And Auth Checks

- Access token TTL is short and within the production validation window.
- Refresh tokens are delivered only through the HttpOnly refresh cookie and are
  cleared on sign-out.
- Refresh-token cookies use `Secure=true` in production and a SameSite setting
  compatible with the frontend/API deployment.
- Sign-in and sign-up keep application-level auth rate limiting enabled unless
  a reviewed stronger lockout or edge control replaces it.
- The application does not trust `X-Forwarded-For`, `X-Real-IP`, or similar
  headers by default. If real client IPs are needed behind a proxy, define a
  trusted-proxy strategy before using forwarded headers for rate limits or
  audit metadata.
- Credentialed CORS is restricted to exact owned origins. Do not combine
  credentials with broad wildcard origins.
- Admin-only endpoints stay admin-only:
  - `GET /api/users`
  - `GET /api/users/{id}`
  - `GET /api/system-events`
- Owner-scoped endpoints enforce owner scope in SQL, not only in handlers.
- Audit metadata remains safe for long-term storage and admin viewing:
  identifiers, enum reasons, status values, `changed_fields`, and `request_id`
  are allowed; passwords, tokens, cookies, authorization headers, raw SQL errors,
  stack traces, DSNs, and free-form business text are not.
- New failure audit producers must follow `docs/audit-policy.md` before runtime
  code is added.

## Observability And Operations Checks

- Use `GET /healthz` for liveness. It confirms the process is alive and does not
  check dependencies.
- Use `GET /readyz` for readiness. It checks PostgreSQL connectivity and should
  stop traffic during dependency outages.
- Do not configure `/readyz` as a liveness probe; a temporary database outage
  should stop routing, not force a restart loop.
- Production logs are structured JSON through `slog`.
- Every request log and API error response carries a `request_id` so operators
  can connect client failures to server logs.
- Request/response headers are not logged. Sensitive query parameters are
  redacted in request logs.
- Docker or deployment probes should use:
  - Liveness: `GET /healthz`
  - Readiness: `GET /readyz`
- After deploy, run a black-box verification path:
  - `make postman-test` against the deployed API when credentials and URL are
    configured for that environment.
  - `make smoke-local` for local release rehearsal before deployment.
- The API should receive `SIGTERM` with enough time to drain in-flight requests
  according to `HTTP_SHUTDOWN_TIMEOUT_SECONDS`.

## Agent Workflow Gate

Before completing any new CRUD module, an Agent must run or explicitly justify
not running:

- `make sqlc` if SQL files or migrations changed.
- `make openapi` and `make openapi-types` if routes, request bodies, response
  bodies, status codes, or error shapes changed.
- `make check` as the normal final gate.
- `make smoke-local` when the module changes public HTTP behavior covered by
  the Postman/Newman black-box baseline or when a new collection case was added.
- `git diff --exit-code -- openapi/openapi.json openapi/typescript/schema.ts`
  before reporting API contract stability.

Update `docs/crud-module-guide.md` when:

- The recommended module layout, implementation order, authorization models,
  pagination conventions, endpoint conventions, testing pattern, or generated
  artifact rules change.
- A new CRUD module introduces a reusable pattern future modules should follow.

Update `docs/audit-policy.md` when:

- A new `event_type` is introduced.
- A failure path becomes auditable.
- Safe metadata rules or event taxonomy changes.
- A resource needs a different audit tier or volume-control rule.

Update the Postman collection when:

- A new frontend-facing endpoint is added.
- Auth/session behavior changes.
- A new admin-only, owner-scoped, conflict, archive, or audit-read behavior
  needs black-box coverage.
- A response envelope or status code changes in a way external clients should
  observe.

Update OpenAPI and TypeScript artifacts when:

- Any Huma operation is added, removed, renamed, or retagged.
- Request DTOs, response DTOs, path/query params, error responses, status codes,
  or nullable array guarantees change.
- Frontend/Agent code would otherwise need to guess a type by hand.

## Explicit Non-Goals And TODO

These are future evolution items. They do not block the current starter API
infrastructure pause point:

- Metrics and distributed tracing are not implemented yet.
- External secret manager integration is not wired yet.
- Distributed locks and job queues are not implemented yet.
- Full RBAC or a policy engine is not implemented yet.
- Refresh-token device/session management UI is not implemented yet.

## API Infrastructure Pause Criteria

The API infrastructure work can pause and shift toward concrete business CRUD
module development when all of the following are true:

- The repository is **Local Development Ready**.
- Staging and production readiness gaps are documented in this checklist rather
  than hidden in code comments or tribal knowledge.
- Auth, refresh, sign-out, admin users, projects CRUD/archive/conflict,
  system-events reads, audit success events, and audit failure policy are
  documented and covered by the available test/smoke gates.
- `make check`, `make smoke-local`, Docker build, OpenAPI/TypeScript drift
  checks, and migration preflight expectations are clear to future Agents.
- The README links to this checklist, deployment migration notes, audit policy,
  and frontend integration guidance.
- No runtime code, migrations, or generated artifacts are required solely to
  declare the infrastructure baseline paused.
