# AGENTS.md

These instructions apply to every file under this repository. More deeply nested AGENTS.md files may override these rules for their own subtree. Direct user, developer, and system instructions always take precedence.

## Project Purpose

This repository is the Go API service for the Minimal Starter dashboard projects. The API should provide stable, typed, documented endpoints that the Next.js and Vite starter frontends can consume without ad hoc contract guessing.

The project is an established, production-shaped service. Keep it small and focused rather than growing it into a large demo scaffold.

## Codex Operating Rules

- Keep this file as a map, not a manual. Put long-lived architecture notes in `docs/`, migrations in `migrations/`, and task-specific plans in `docs/plans/`.
- For large features, security-sensitive work, schema changes, or cross-cutting refactors, start in Ask/plan mode and write or update a plan before editing code.
- Do not commit process-only `docs/plans` files by default. Commit plans only for cross-module designs, security/DB/API contract changes, multi-agent handoff tasks, or when the user explicitly asks for a plan artifact.
- Structure task prompts and PR summaries like GitHub issues: include scope, files, expected behavior, acceptance checks, and known constraints.
- If you need OpenAI API, ChatGPT Apps SDK, Codex, or related OpenAI documentation, always use the OpenAI developer documentation MCP server first without requiring the user to explicitly ask.
- After code changes, run the applicable checks listed in this file and report exact commands and results.
- Do not rewrite or substantially reorganize `AGENTS.md` without explicit user approval. Small updates are fine only when they keep future agents aligned with an accepted project decision.

## Default Technical Direction

Use this stack unless the user explicitly changes it:

- Language/runtime: current stable Go, initial target `1.26.x`.
- HTTP: standard `net/http`, `chi` for routing/middleware, `Huma v2` for typed handlers, validation, and OpenAPI 3.1 generation.
- Database: PostgreSQL.
- DB driver/pool: `pgx/v5` and `pgxpool`.
- SQL layer: `sqlc` with explicit SQL files. Do not introduce GORM unless requested.
- Migrations: `goose` for the first version. Consider Atlas only when schema governance becomes a real requirement.
- Auth: JWT access tokens plus refresh-token flow; use `golang-jwt/jwt/v5`.
- Passwords: Argon2id through `golang.org/x/crypto/argon2` by default. Support bcrypt only as a legacy hash verifier if migration from old accounts requires it.
- Config: environment variables parsed into typed config, preferably with `caarlos0/env/v11`.
- Logging: standard library `log/slog`.
- Observability: OpenTelemetry-friendly middleware and context propagation.
- Tests: standard `testing`, `httptest`, and `testcontainers-go` for PostgreSQL integration tests.

## Configuration Rules

- All runtime configuration lives in `internal/config/Config` and is loaded from environment variables via `caarlos0/env/v11`.
- When adding a new config field, update all of the following in the same PR:
  1. `internal/config/config.go` — add the typed field with `env` tag and a sensible `envDefault`.
  2. `.env.example` — document the variable and its default.
  3. `README.md` Configuration table — add the new variable.
  4. If the field affects runtime behavior (timeouts, feature flags, etc.), add or extend a test in `internal/config/config_test.go`.
- Use explicit types (int for seconds, `[]string` for comma-separated lists). Add `time.Duration` helper methods in the config package instead of converting in the app layer.
- For constrained fields (e.g. `LOG_LEVEL`), validate in `Load()` and return an error for invalid values instead of falling back silently. Add tests for both valid and invalid inputs.
- Enforce strict, early-failing validations in `Load()` for production configurations (e.g., `ENV=production` must reject wildcard/loopback CORS, weak/placeholder secrets, insecure refresh token cookie policies, empty database URLs, and invalid cookie names).
- Config tests must call `clearConfigEnv(t)` before `Load()` to isolate from host environment variables. Keep the key list in `configEnvKeys` up to date when adding new fields.
- Never expose the `Config` struct directly in an HTTP response or log it in full — it may contain secrets once database and auth fields are added.

## Project Layout

The project follows a per-resource module structure. Keep this shape unless a better local pattern emerges:

```txt
cmd/api/                 main package for the HTTP server
cmd/openapi/             OpenAPI artifact generator
cmd/seed/                local demo data seeder
cmd/smoke-auth/          local auth smoke-test runner
internal/app/            application wiring and lifecycle
internal/config/         typed environment config
internal/logging/        slog logger construction
internal/http/           router, handlers, middleware, apierror, pagination, pathparam
internal/auth/           token, password, auth service, and auth policy code
internal/<resource>/domain   domain types and business invariants per resource
internal/<resource>/service  use-case services per resource
internal/store/          database access layer (per-resource repos)
internal/store/sql/      hand-written SQL sources
internal/store/query/    sqlc generated code
internal/seed/           shared seed logic
migrations/              goose SQL migrations
docs/                    architecture, API notes, plans, and operations docs
openapi/                 generated OpenAPI artifacts and TypeScript types
scripts/                 small project scripts
```

See `docs/crud-module-guide.md` for the per-resource module conventions.

Do not put business logic in `cmd/api`. Keep handlers thin: parse/validate input, call a service, return a response.

## API Contract Rules

- Keep frontend compatibility explicit. The dashboard starters expect API paths under `/api/...`.
- Initial compatibility endpoints should include auth and health checks: `/api/auth/sign-in`, `/api/auth/sign-up`, `/api/auth/me`, `/healthz`, and `/readyz`.
- `/healthz` is a fixed liveness probe, but `/readyz` must check core runtime dependencies such as PostgreSQL and must not be implemented as a fixed success response.
- Every endpoint should be represented in OpenAPI through Huma. Do not add undocumented routes.
- Keep a static OpenAPI artifact available for frontend type generation. Add a small generator, for example `cmd/openapi` or `scripts/gen_openapi.go`, that builds the same route registry and writes `openapi/openapi.json` without starting the HTTP server.
- Keep committed frontend contract artifacts in sync: `openapi/openapi.json` is the source contract and `openapi/typescript/schema.ts` is the generated TypeScript type surface. When API request/response/error shapes change, regenerate and commit both artifacts.
- Use stable JSON response shapes. If a response shape changes, update OpenAPI, tests, and any frontend client/types in the same task.
- Use consistent error envelopes with machine-readable codes. Do not leak stack traces, SQL errors, tokens, or internal config values.
- All error responses must use the `internal/http/apierror` package to produce a stable JSON envelope with fields: `code`, `message`, `request_id`, and optional `details`. Do not return bare string errors or ad hoc JSON from handlers.
- Auth, database, and unexpected internal errors must be wrapped with `apierror.InternalError(cause)` or an appropriate code so the original error is logged but never sent to clients.
- Every error response must include the current `request_id` from chi's middleware context. Use `apierror.ForContext(ctx)` in Huma handlers or `apierror.WriteResponse` with `WithRequestID` elsewhere.
- Prefer pagination, filtering, and sorting contracts that are explicit and typed. Avoid passing raw SQL fragments or arbitrary field names from clients.

## Database Rules

- Every schema change must have a migration and a rollback where practical, written as a goose migration file inside the `migrations/` directory. Do not use random defaults like `gen_random_uuid()` without explicit intent; pass primary key values or parameters from the application layer where possible.
- Every database query used by production code must reside in standard SQL files inside `internal/store/sql/` and be generated under `internal/store/query/` via `sqlc` (run `make sqlc` to regenerate).
- Do not manually edit the generated `internal/store/query/` Go package; it must remain 100% managed by `sqlc`.
- Avoid dynamic SQL string concatenation. If dynamic filtering is needed, use a narrow, reviewed helper with whitelisted fields.
- Use transactions for multi-step writes. Pass `context.Context` through every DB operation.
- Keep `pgx.Tx` and other driver-specific transaction details inside `internal/store`. Expose a small transaction boundary such as `Store.WithTx(ctx, func(ctx context.Context, s Store) error) error` or a `TxManager` interface so services can require atomic work without importing `pgx`.
- All PostgreSQL connection pool construction must occur through `internal/store.NewPool` (which wraps `pgxpool`).
- Complete database connection credentials (specifically passwords) must never be written in raw log outputs or returned in raw error messages. Always sanitize database URLs/DSNs via `internal/store.SanitizeDatabaseURL` before logging or returning error contexts.
- Integration tests requiring a database must utilize `testcontainers-go` and not depend on static local or external database instances. Keep these tests fast and separate using the `integration` build tag.

## Security Rules

- Never commit secrets, private keys, real tokens, or production database URLs.
- Keep config in environment variables and document required keys in `.env.example`.
- Authentication and authorization must be enforced in services/handlers, not only middleware.
- User roles must come from the database or an explicit seed/config path. Do not hardcode all auth responses to a single role in services or handlers.
- Store password hashes only. Never store plaintext passwords.
- All password hashing and verification must go through `internal/auth/password.Hash` and `internal/auth/password.Verify`. Do not introduce alternative hashing paths without explicit approval.
- Never log or include plaintext passwords, full password hashes, or raw Argon2id encoded strings in error messages, log output, or API responses.
- Never log `Authorization`, `Cookie`, `Set-Cookie`, plaintext passwords, access tokens, refresh tokens, or token-bearing query parameters.
- Set CORS deliberately for the starter frontend origins; do not use unrestricted CORS in production config. Production credentialed CORS must use exact owned origins, not public shared-hosting wildcards such as `*.vercel.app` or `*.netlify.app`.
- The application must not trust `X-Forwarded-For`, `X-Real-IP`, or similar proxy headers by default. If real client IPs are needed behind a reverse proxy, enforce rate limits at the trusted edge or add reviewed trusted-proxy configuration first.
- Treat request bodies, headers, path params, query params, and JWT claims as untrusted input.
- Auth sign-in and sign-up must keep application-level rate limiting or a stronger lockout strategy before public exposure.
- Auth audit events must use stable event type strings and safe metadata only. Never record plaintext passwords, password hashes, access tokens, or raw internal error strings in `system_events`.

## Testing Policy

This API is a real backend, not a UI template. New business behavior should include tests.

- Unit test pure logic: config parsing, auth token handling, password helpers, permission checks, validators, and service decisions.
- Handler test HTTP behavior with `httptest`: status codes, request validation, response shapes, and auth failures.
- Store test database behavior with PostgreSQL through `testcontainers-go` once persistence is introduced.
- Keep routine unit tests fast. Gate slower PostgreSQL/container tests behind an `integration` build tag or a clearly named command, and reuse containers per package or test run instead of starting one per test case.
- Test configuration should not depend on developer `.env` database values. Integration tests should create their own containerized database or use explicit test-only overrides.
- Contract test frontend-facing endpoints so dashboard login and core CRUD flows do not regress.
- For bug fixes, write a failing test that reproduces the bug before or alongside the fix when feasible.

Do not claim completion based only on compilation if the change affects auth, data persistence, validation, or API response contracts.

## Local Development

Use Air for local live reload:

```sh
air -c .air.toml
```

The Air config builds `./cmd/api` into `./tmp/wow-dashboard-api`, loads `.env` and `.env.local` when present, excludes generated/build/editor folders, and keeps the browser proxy disabled because this is an API service. It intentionally uses local-debug build flags instead of production `-trimpath`; keep production release flags in Docker/CI scripts. If Air is not installed, use `go install github.com/air-verse/air@latest` or, with Go 1.25+, add it as a tool and run `go tool air -c .air.toml`.

## Commands

The project provides a `Makefile` that wraps all verification steps. **Prefer `make check` as the single command to validate changes.** CI runs the same target.

```sh
make check          # run all checks: fmt, vet, tests, race, integration, openapi/json/types drift
make fmt            # auto-format Go source files
make fmt-check      # fail if any files are unformatted
make test           # go test ./...
make test-race      # go test -race ./...
make test-integration # go test -tags=integration ./...
make vet            # go vet ./...
make openapi        # regenerate openapi/openapi.json
make openapi-check  # regenerate and fail if committed file drifted
make openapi-types  # regenerate openapi/typescript/schema.ts
make openapi-types-check # regenerate and fail if committed TypeScript types drifted
make seed           # seed local demo auth user (requires local DATABASE_URL)
make compose-up     # start local PostgreSQL for development
make local-setup    # migrate local PostgreSQL and seed demo auth user
make smoke-auth     # verify local health/ready/sign-in/me auth flow
make dev            # start Air live-reload dev server
```

The underlying raw commands, for reference or scripts that cannot use Make:

```sh
gofmt -w .
go test ./...
go test -tags=integration ./...
go test -race ./...
go vet ./...
go run ./cmd/openapi
make openapi-types
go run ./cmd/seed
git diff --exit-code -- openapi/openapi.json
git diff --exit-code -- openapi/typescript/schema.ts
```

When sqlc, migrations, or lint tools are introduced, add their exact commands here and keep CI in sync.

## Codex Cloud Notes

Use this repository as its own Codex cloud environment. Cross-repository tasks
that also change `wow-dashboard-starter` should be split into coordinated PRs
unless the user explicitly asks for a different workflow.

Recommended Codex cloud setup script:

```sh
go mod download
bunx --bun openapi-typescript@7.13.0 --version >/dev/null
```

Preferred validation is `make check`. If the cloud runtime cannot run Docker or
`testcontainers-go` integration tests, run the non-container subset
(`make fmt-check`, `make vet`, `make test`, `make test-race`,
`make openapi-check`, and `make openapi-types-check`) and state exactly which
integration checks were skipped.

## Review Guidelines

- Treat auth, CORS, token handling, password hashing, migrations, and OpenAPI response changes as high-risk.
- Verify every new endpoint has tests and appears in `openapi/openapi.json`.
- Verify generated OpenAPI JSON and TypeScript contract artifacts are updated in the same PR as handler/schema changes.
- Flag unrestricted CORS, leaked internal errors, missing context propagation, and untested auth paths as P1.
- Flag any change that logs or returns raw database URLs, tokens, cookies, password hashes, SQL errors, or internal stack traces as P1.
- Verify readiness checks still test PostgreSQL connectivity instead of becoming fixed success responses.
- Verify background worker, queue, and migration changes keep idempotency, context propagation, and transaction boundaries intact.
- Do not suggest broad rewrites unless they directly reduce a demonstrated risk.

## Git And PR Notes

- Keep commits focused on one coherent change.
- PR summaries should include behavior changes, API contract changes, migration impact, test evidence, and deployment/config notes.
- Preserve unrelated user changes. Do not rewrite history or remove local files unless explicitly asked.
