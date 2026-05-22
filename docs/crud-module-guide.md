# CRUD Module Development Guide

This guide is the executable checklist for adding a new CRUD business module to this API. It captures the current `projects` module pattern and should be used before copying or extending that pattern for another resource.

Use `projects` as the reference implementation:

| Layer | Reference file |
|-------|----------------|
| Migration | `migrations/00006_create_projects.sql`, `migrations/00007_add_projects_owner_name_unique_index.sql` |
| SQLC queries | `internal/store/sql/projects.sql` |
| SQLC generated code | `internal/store/query/projects.sql.go` |
| Domain | `internal/projects/domain/project.go` |
| Service | `internal/projects/service/service.go` |
| Service audit | `internal/projects/service/audit.go` |
| Store adapter | `internal/store/projectsrepo/projects.go` |
| Store audit adapter | `internal/store/projectsrepo/audit.go` |
| Handler | `internal/http/handlers/projects.go` |
| App wiring | `internal/app/app.go` |
| OpenAPI export fake | `cmd/openapi/main.go` |
| Tests | `internal/projects/service/service_test.go`, `internal/store/projectsrepo/*_integration_test.go`, `internal/http/handlers/projects_test.go` |

## Module Boundary And Directory Structure

Use this structure for a new resource unless the resource has a reviewed reason to differ:

```txt
internal/<resource>/domain/
internal/<resource>/service/
internal/store/<resourcerepo>/
internal/store/sql/<resource>.sql
internal/http/handlers/<resource>.go
```

Keep responsibilities strict:

| Layer | Owns | Must not own |
|-------|------|--------------|
| `internal/<resource>/domain` | Resource entities, enum types, normalized store inputs/results, domain-level sentinels such as `Err<Resource>NotFound` | HTTP request/response DTOs, sqlc row structs, pgx/pgtype values |
| `internal/<resource>/service` | Use-case ports, validation, normalization, UUID parsing, default values, clocks/IDs, mapping domain sentinels to service sentinels, best-effort audit calls | sqlc, pgx, pgtype, HTTP response envelopes, Huma types |
| `internal/store/<resourcerepo>` | Adapter from service/domain ports to sqlc generated queries, pgtype conversion, `pgx.ErrNoRows` to domain sentinel mapping, owner-scoped DB enforcement | Business validation, HTTP auth, response shaping |
| `internal/store/sql/<resource>.sql` | All production SQL used by the module, with stable sqlc query names and explicit `WHERE` scope | Dynamic SQL concatenation, handler/service-only filtering |
| `internal/http/handlers/<resource>.go` | Huma operation registration, request DTOs, response DTOs, auth/authz checks, mapping service errors to `apierror` envelopes | Business invariants, DB type conversion, SQL access |

The service layer must not import sqlc, `pgx`, or `pgtype`. If you see `internal/store/query`, `github.com/jackc/pgx`, or `github.com/jackc/pgx/v5/pgtype` in `internal/<resource>/service`, stop and move that conversion into the repo adapter.

## Implementation Order

Follow this order. It keeps compile errors local and makes each review step concrete.

1. **Migration**
   - Add a goose migration under `migrations/`.
   - Include a practical rollback.
   - Prefer application-provided IDs/timestamps over random DB defaults unless there is an explicit reason.
   - Add owner/admin foreign keys and indexes needed by expected list/detail lookups.

2. **SQLC query file**
   - Add `internal/store/sql/<resource>.sql`.
   - Define create, detail, list page, count page, partial update, and archive/soft-delete queries as needed.
   - Put owner scope directly in `WHERE` for owner-scoped resources.
   - Run `make sqlc` after the SQL compiles.

3. **Domain types**
   - Add `internal/<resource>/domain`.
   - Define the canonical entity, status/lifecycle enums, normalized store inputs/results, and domain sentinels.
   - Use Go types the service can reason about directly, such as `uuid.UUID`, `time.Time`, pointers for partial updates, and string enum aliases.

4. **Repo adapter**
   - Add `internal/store/<resourcerepo>`.
   - Wrap `*query.Queries` behind a small adapter.
   - Convert sqlc row structs and pgtype values to domain structs.
   - Map `pgx.ErrNoRows` to the domain not-found sentinel.
   - Keep all pgtype and sqlc generated types inside this package.

5. **Service port, use cases, and validation**
   - Define a store port interface in `internal/<resource>/service`.
   - Parse authenticated user/resource IDs at the service boundary.
   - Normalize pagination with `internal/http/pagination.Normalize`.
   - Normalize path UUIDs with `internal/http/pathparam.ParseUUID`.
   - Validate required fields, lengths, enum values, empty patch bodies, and partial-update semantics.
   - Generate IDs/timestamps in the service when the application should own them.
   - Map domain sentinels to service sentinels, for example `ErrNotFound`.

6. **Handler and Huma registration**
   - Add `internal/http/handlers/<resource>.go`.
   - Define the handler-facing service interface.
   - Register every endpoint through Huma with operation IDs, tags, summaries, and explicit error responses.
   - Authenticate before calling the service.
   - Apply resource authz helpers/policies before the use case when the decision can be made from the current user.
   - Map all service errors through `internal/http/apierror` and attach the current request ID with `.ForContext(ctx)`.

7. **App wiring**
   - Add the handler service dependency to `internal/app.Dependencies`.
   - Register routes only when the required services are non-nil.
   - In `Run`, construct the repo adapter and service from the shared `query.New(pool)`.
   - Wire audit recorders or no-op adapters explicitly.

8. **OpenAPI fake service**
   - Update `cmd/openapi/main.go`.
   - Add imports for the new domain/service packages.
   - Add an `openAPI<Resource>Service` that satisfies the handler service interface.
   - Register it through `app.RegisterRoutes`.
   - The fake service should return zero values; it must not connect to PostgreSQL.

9. **Tests**
   - Add service unit tests first.
   - Add repo integration tests behind the `integration` build tag.
   - Add handler tests with `httptest`.
   - Add OpenAPI contract assertions in handler tests.

10. **README/API docs**
   - Document endpoints, authz model, query params, response envelopes, and audit event types in `README.md`.
   - Update `docs/frontend-integration.md` only when frontend contract guidance changes.

11. **Generated artifacts**
   - Run `make sqlc` when SQL changes.
   - Run `make openapi` and `make openapi-types` when request/response/error shapes or route registration change.
   - Commit generated artifacts with the code that required them.

## CRUD Endpoint Conventions

Use plural resource paths under `/api`.

| Operation | Convention |
|-----------|------------|
| List | `GET /api/<resources>` |
| Detail | `GET /api/<resources>/{id}` |
| Create | `POST /api/<resources>` |
| Update | `PATCH /api/<resources>/{id}` |
| Delete/archive | `DELETE /api/<resources>/{id}` |

List endpoints:

- Support `page`, `pageSize`, and `search`.
- Default pagination through `pagination.Normalize`: `page=1`, `pageSize=20`, max page size `100`.
- Return `{ "<resources>": [], "page": 1, "pageSize": 20, "total": 0 }`.
- Mark the items array as non-nullable in Huma DTO tags, for example `nullable:"false"`.
- Build empty responses with `make([]itemDTO, 0, len(items))` so JSON emits `[]`, not `null`.
- Add resource-specific filters only when they are typed and validated, such as `status`.

Detail endpoints:

- Use a UUID path parameter with `format:"uuid"`.
- Return a detail envelope, for example `{ "project": { ... } }`.
- Invalid UUIDs return `422 validation_failed`.
- Missing rows return `404 not_found`.

Create endpoints:

- Return `201 Created` with the same detail body as the detail endpoint.
- The service owns validation, trimming, default status, IDs, and timestamps.
- Do not echo unsafe input values in error messages.
- For owner-scoped resources with unique human-facing names, enforce uniqueness in PostgreSQL with an owner-scoped unique constraint or index. The `projects` baseline uses `UNIQUE (owner_user_id, name)`, keeps PostgreSQL's default case-sensitive comparison, and does not release names when rows are archived.

Update endpoints:

- Use `PATCH`, not `PUT`, for partial updates.
- Request DTO fields that can be omitted must be pointers.
- Empty body returns `422 validation_failed`.
- Omitted fields stay unchanged.
- Empty strings only clear a field when the contract explicitly says so. `projects.description` is the reference: nil means unchanged, `""` means clear.
- Track changed field names for audit metadata; record names only, not values.
- Updating a unique field to a value already used in the same owner scope should return the same conflict envelope as create. Updating a row to its current unique value should still succeed.

Delete endpoints:

- Prefer archive/soft-delete semantics first.
- Do not physically delete rows unless retention, product behavior, and audit implications have been designed.
- For archive, return the archived resource detail body.
- Make archive idempotency explicit. `projects` allows archiving an already archived row and refreshes `updated_at`.

Error responses:

- Every error response must use the stable API envelope from `internal/http/apierror`.
- Include the current request ID through `.ForContext(ctx)` in Huma handlers.
- Expected mappings:
  - malformed JSON/body shape: `400 bad_request` from Huma/pre-handler handling
  - invalid UUID or invalid fields: `422 validation_failed`
  - unauthenticated: `401 unauthorized`
  - forbidden: `403 forbidden`
  - not found: `404 not_found`
  - unique constraint conflicts: `409 conflict` with a resource-specific safe message such as `Project name already exists.`
  - unexpected service/store failures: `500 internal_error`, with the cause logged server-side only

Conflict mapping belongs at the same layer boundaries as not-found mapping. The repo adapter inspects PostgreSQL unique violations with `pgconn.PgError`, SQLSTATE `23505`, and the specific constraint/index name, then maps the database error to a domain sentinel such as `ErrProjectNameAlreadyExists`. The service maps that domain sentinel to a service sentinel such as `ErrNameConflict`, without importing pgx, pgtype, or sqlc packages. The handler maps the service sentinel to `apierror.Conflict(...)` and includes `409` in OpenAPI only for operations that can produce the conflict. Failure-path audit is not implicit; keep success audit only unless a separate failure-audit design is approved.

## Authorization Strategy

Every module must declare one authorization model before implementation:

| Model | Current reference | Rules |
|-------|-------------------|-------|
| Admin-only | `users` endpoints and `internal/http/handlers/authz.go` | Require authenticated current user with `role = "admin"` before calling the service. Use a helper such as `requireAdmin`, not ad hoc role checks in each handler branch. |
| Owner-scoped authenticated user | baseline `projects` ownership | Authenticate the user, pass the current user ID into the service, parse it as a UUID at the service boundary, and enforce ownership in every SQL query `WHERE` clause. |
| Owner + role-based members (shared resource) | `projects` sharing | Layered on owner scope: a `project_members` table grants non-owner `viewer`/`editor` roles. |
| Public read-only | No current CRUD reference | Must be intentionally documented. Limit to read routes, keep response fields safe for anonymous callers, and still validate path/query params. |

Do not scatter authorization as casual inline `if` checks. Extract a helper or policy function that can be tested and reused. Handler-level authz is good for decisions based on the authenticated user, but it is not enough for resource scope.

Resource scope must be enforced in SQL — never filter rows in the handler or service after fetching, which risks leaking existence, data, counts, or timing behavior. For a purely owner-scoped resource, every list/detail/update/archive query carries `WHERE ... AND owner_user_id = @owner_user_id`.

A shared resource still enforces access in SQL, but needs the requester's *role*, which a single boolean `WHERE` cannot express. The `projects` reference uses an SQL access query (`GetProjectWithAccess`) that returns the row plus a computed `access_role` (`owner`/`editor`/`viewer`) — or no row when the user has no access. The service then gates the operation on that role (e.g. `editor` may update, only `owner` may archive or manage members), mapping no-access to not-found and insufficient-role to forbidden. List queries use `owner_user_id = @user OR id IN (member subquery)`.

## Audit Strategy

CRUD write operations should record `system_events` after the main write succeeds.

Use this policy unless the resource design says otherwise:

- Record create, update, and archive/soft-delete success events.
- Use stable event type strings, for example `projects.project.created`.
- Keep metadata safe and stable.
- Include identifiers such as resource ID, owner/admin actor ID, status, changed field names, and `request_id`.
- Do not include sensitive fields, secrets, tokens, password hashes, internal error strings, or free-form business text such as names/descriptions.
- Treat audit persistence as best-effort: log audit failures and keep the original successful API result.
- Keep audit recorder interfaces in the service package and system_events adapters in the store package.

Failure-path audit requires a separate design. Do not add failure events by default just because success audit exists. Decide first which failures matter, what safe reason codes are allowed, how rate/volume is controlled, and whether failures should affect the user-visible response.

## Testing Template

Add tests at each layer. Do not claim completion based only on compilation for modules that touch auth, data persistence, validation, or API contracts.

### Service Unit Tests

Cover:

- Validation for required fields, length limits, enum values, invalid UUIDs, invalid pagination, and empty patch bodies.
- Normalization for trimming, default values, lower-cased enums, page/offset/search, generated IDs, and timestamps via a test clock.
- Partial update behavior: nil means unchanged; explicit empty string clears only fields whose contract allows it.
- Store sentinel mapping: domain not-found sentinel becomes service `ErrNotFound`; invalid input becomes service `ErrInvalidInput`.
- Store conflict sentinel mapping: domain unique-conflict sentinels become service conflict sentinels and do not trigger success audit.
- Store is not called for invalid inputs.
- Audit success metadata for create/update/archive.
- Audit best-effort behavior: audit failure is logged/tolerated and the write result still succeeds.
- Audit metadata safety: no free-form business text, secrets, raw tokens, password hashes, or internal error text.

### Repo Integration Tests

Use the `integration` build tag and `storetest.NewPostgresPool`.

Cover:

- Migration-backed create/get/list round trips.
- sqlc query behavior, including generated row conversion.
- Owner scope for list, detail, update, and archive.
- Wrong owner returns the same not-found sentinel as a missing row.
- Wrong owner cannot mutate another owner row.
- Pagination, search, and resource-specific filters.
- Partial update semantics: unchanged omitted fields, explicit clears, status-only updates.
- Archive/soft-delete behavior and idempotency.
- `pgx.ErrNoRows` mapping to the domain not-found sentinel.
- Audit adapter persistence into `system_events` when the module has audit events.

### Handler Unit Tests

Use `httptest` with fake auth and fake service implementations.

Cover:

- `401` when the bearer token is missing or invalid.
- `403` for admin-only endpoints when the authenticated user is not an admin.
- Owner-scoped endpoints remain available to authenticated non-admin users when that is the intended model.
- `404`, `422`, and `500` envelopes from service error mapping.
- `409 conflict` envelopes for create/update operations that can hit unique constraints.
- Malformed JSON/body shape returns the API error envelope.
- Success response body shape and status codes, including create `201`.
- Authz inputs passed to service, especially current user ID for owner scope.
- Path/query/body fields forwarded correctly.
- Invalid UUID path params do not call the service when Huma rejects them before the handler.
- OpenAPI contract assertions for expected error responses, UUID path parameter formats, and non-null list arrays.

### Generated Contract Checks

When API shape changes:

```sh
make openapi
make openapi-types
git diff --exit-code -- openapi/openapi.json openapi/typescript/schema.ts
```

The final gate is:

```sh
make check
```

For documentation-only changes, Go tests may be skipped, but still verify that no runtime or generated contract files drifted.

## Commit And PR Granularity

A full resource vertical slice can be split into focused commits:

1. Schema and store.
2. Service plus handler create/list/detail.
3. Update and archive.
4. Audit.
5. Docs.

Each commit should pass the relevant tests for the files it changes. The final state must pass `make check`.

Generated artifacts belong in the same commit as the code that changes their source:

- SQL changes include regenerated `internal/store/query`.
- API contract changes include regenerated `openapi/openapi.json` and `openapi/typescript/schema.ts`.

Do not commit temporary plan files under `docs/plans/` unless the user explicitly asks for a plan artifact or the plan is needed as a long-lived cross-module/security/DB/API design record.

PR summaries should read like a GitHub issue close-out:

- Scope and behavior changes.
- API contract changes.
- Migration impact.
- Authz and audit decisions.
- Test evidence.
- Deployment/config notes.
- Known constraints or residual risks.

## Agent Execution Checklist

Use this checklist when implementing a new CRUD module:

- [ ] Read `AGENTS.md`.
- [ ] Run `git status --short` and preserve unrelated user changes.
- [ ] Identify the resource name, route plural, and response envelope key.
- [ ] Declare the authorization model: admin-only, owner-scoped, or public read-only.
- [ ] Declare the audit strategy: success events, failure events if intentionally designed, safe metadata fields, and best-effort behavior.
- [ ] Write or update the migration with rollback and indexes.
- [ ] Write `internal/store/sql/<resource>.sql`.
- [ ] Run `make sqlc`.
- [ ] Add `internal/<resource>/domain`.
- [ ] Add `internal/store/<resourcerepo>`.
- [ ] Add `internal/<resource>/service` ports, use cases, validation, normalization, and sentinels.
- [ ] Add `internal/http/handlers/<resource>.go` with Huma registration and `apierror` envelopes.
- [ ] Wire the service in `internal/app/app.go`.
- [ ] Add the OpenAPI fake service in `cmd/openapi/main.go`.
- [ ] Add service unit tests.
- [ ] Add repo integration tests.
- [ ] Add handler and OpenAPI contract tests.
- [ ] Update `README.md` and any relevant docs.
- [ ] Run `make openapi`.
- [ ] Run `make openapi-types`.
- [ ] Run `make check`.
- [ ] Confirm generated artifacts are committed with their source changes.
- [ ] Report changed files, verification commands/results, and residual risks.
