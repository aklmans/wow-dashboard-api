# Audit Policy

This document defines when failures should be written to `system_events`.
It exists to keep audit events useful, safe, and bounded. Do not treat every
HTTP `4xx` or `5xx` response as an audit event.

## Goals And Non-Goals

Audit events are for:

- Security traceability, especially authentication, authorization, token, and
  admin-sensitive actions.
- Critical business change traceability, such as successful create, update, and
  archive mutations.
- Investigation of abnormal behavior when a concise event helps connect actors,
  resources, and safe reason categories.

Audit events are not:

- General request logs.
- Debug logs.
- A per-validation-error ledger.
- A replacement for rate limiting, metrics, traces, or structured request logs.

## Event Classification

Use these tiers before adding any new failure audit producer.

| Tier | Policy | Examples |
|------|--------|----------|
| Required | Record as audit events because they are security-sensitive or describe critical state changes. | Auth success/failure, permission denied, critical mutation success, critical mutation conflict. |
| Optional | Record only when there is a clear investigation value and volume control exists. | Repeated `404` probing, suspicious bulk `422`, refresh-token replay. |
| Do not record | Keep in request logs, validation responses, metrics, or rate-limit counters. | Ordinary form validation errors, health checks, normal list queries, static assets, one-off `404` with no security meaning. |

Do not write `system_events` for every malformed request. If a failure can be
triggered cheaply and repeatedly by an unauthenticated client, it needs a
threshold, sampler, aggregator, or rate limiter before it becomes an audit
event.

## Auth Failure Policy

`POST /api/auth/sign-in` failure is a required audit event. The current event
type is `auth.sign_in.failed`.

Refresh-token and access-token failures should be handled by value:

- `refresh replay`: record when replay is detected because it can indicate token
  theft or session compromise. Use a specific reason such as `replay_detected`.
- `revoked token`: record only when it indicates suspicious reuse, repeated
  reuse, or a security rule trigger. A routine rejected refresh after sign-out
  can stay in request logs.
- `malformed token`: do not record every malformed token. Use request logs,
  rate limits, and metrics by default. Record only threshold-triggered or
  aggregated events.
- `permission denied`: record when an authenticated actor is denied access to an
  admin-only or role-protected action.

Sign-up failures should be narrow:

- `duplicate email`: may be recorded as a low-frequency business/security
  signal, but metadata must not store long-term plaintext email.
- `weak password`: do not record the password, password length, password class
  details, or field-level validation text. Prefer no audit event unless a
  broader abuse threshold is triggered.
- `invalid email`: do not record ordinary single validation failures. If a
  consolidated sign-up failure event is emitted, use only a coarse reason such
  as `invalid_input` and avoid storing the raw submitted email.

Safe auth metadata may include:

- `user_id` when a user is known.
- `email_hash` or `masked_email`; prefer these over plaintext email for failure
  events.
- `reason` as a stable enum, for example `invalid_credentials`,
  `email_already_exists`, `invalid_input`, `user_disabled`,
  `replay_detected`, or `permission_denied`.
- `request_id`.
- `ip` and `user_agent` only after the project has a reviewed trusted-source
  strategy for those values.

Auth metadata must not include:

- Passwords or password-derived details.
- Access tokens, refresh tokens, raw JWTs, or token hashes unless a future
  reviewed design explicitly allows a non-reversible token identifier.
- Cookies.
- Complete `Authorization` headers.
- Internal error messages or stack traces.

## Projects Failure Policy

Project success audit already records create, update, and archive successes.
Failure audit must remain explicit.

- `409 duplicate name`: record as a low-frequency business conflict when useful
  for support or abuse investigation. Metadata should include only
  `project_id` when known, `owner_user_id`, `reason`, and `request_id`. Do not
  store project `name`, `description`, or submitted text.
- `404 owner-scoped not found`: do not record a single not-found response. Owner
  scoping intentionally hides whether a project exists. Record only
  threshold-triggered probing or a security rule trigger, using a safe event
  such as `projects.project.suspicious_not_found_probe`.
- `403 permission denied`: if project admin roles or shared-project permissions
  are introduced later, record denied access to protected mutations or sensitive
  reads.
- `422 validation failed`: do not record a single validation error. Use request
  logs, metrics, and rate-limit counters. Consider aggregation only for
  suspicious bulk behavior.
- `500 internal error`: do not write raw error messages, SQL errors, stack
  traces, or user input into audit metadata. If a high-value action needs a
  failure event, use a coarse reason category such as `internal_error`,
  `database_unavailable`, or `dependency_timeout`.

## Rate Limiting And Anti-Abuse

Failure audit must not become a write-amplification attack surface.

- Do not add audit writes to every malformed request, invalid JSON body, bad
  UUID, missing auth header, invalid token parse, or validation error.
- Prefer application rate limiters, edge controls, request logs, and metrics for
  high-frequency failures.
- Use sampling, aggregation, or threshold-triggered events for repeated failures.
- Keep unauthenticated failure audit especially narrow because attackers can
  produce those events at low cost.
- Audit writes remain best-effort unless a future design explicitly introduces a
  stronger compliance requirement.

## Metadata Safety Rules

All audit metadata must be safe for long-term storage and admin viewing.

- Use `snake_case` keys.
- Prefer IDs, enum reasons, status values, `changed_fields`, and `request_id`.
- Keep `changed_fields` to field names only, never submitted values.
- Treat email as PII. Failure events should use `masked_email` or `email_hash`
  by default, not long-term plaintext email.
- Do not store free-form business text such as project names, descriptions,
  comments, search strings, or user-provided long text.
- Do not store secrets: `password`, `token`, `cookie`, `authorization`,
  `refresh_token`, `access_token`, raw JWTs, session secrets, or API keys.
- Do not store raw internal errors, SQL messages, stack traces, DSNs, or config
  values.

## Event Taxonomy

Event types currently emitted:

| Event Type | Notes |
|------------|-------|
| `auth.sign_up.succeeded` | Account registration succeeded. |
| `auth.sign_up.failed` | Account registration failed; keep metadata minimal and safe. |
| `auth.sign_in.succeeded` | Credential authentication succeeded. |
| `auth.sign_in.failed` | Credential authentication failed. |
| `projects.project.created` | Project creation succeeded. |
| `projects.project.updated` | Project update succeeded. |
| `projects.project.archived` | Project archive succeeded. |
| `projects.member.added` | A user was granted project access. |
| `projects.member.role_changed` | A project member's role was changed. |
| `projects.member.removed` | A project member's access was revoked. |
| `users.user.updated` | An admin changed another user's status or replaced their role set. |

When adding an `event_type`, update this table and add tests that prove the
event is emitted only under the intended conditions with safe metadata. Before
storing `ip` or `user_agent`, define a trusted-source policy for those values.

## Agent Execution Rules

Before adding any failure audit runtime code, Agents must cite this file in the
task summary, plan, or PR description and explain why the new event belongs in
one of the allowed tiers.

- Add or update the event taxonomy in this file before introducing a new
  `event_type`.
- Add tests for event emission, non-emission, best-effort behavior, and metadata
  safety.
- Never record sensitive fields or free-form user content.
- Never change the user-visible business error semantics just to make audit
  writing easier.
- Treat audit writes as best-effort. An audit write failure must not block the
  main business result unless a future approved design explicitly requires
  strong audit semantics for a specific action.
