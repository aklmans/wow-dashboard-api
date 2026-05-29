# Frontend Integration Guide

How to wire the wow-dashboard or Vite starter frontend to this Go API. This
covers the integration *mechanics*; the per-endpoint contract lives in the
generated artifacts, not here.

## Contract Source

The backend commits two frontend-facing contract artifacts — treat them as the
single source of truth and do not hand-write duplicate request/response DTOs:

| File | Purpose |
|------|---------|
| `openapi/openapi.json` | OpenAPI 3.1 contract exported from the Huma route registry |
| `openapi/typescript/schema.ts` | TypeScript types generated from `openapi/openapi.json` |

Consume them one of two ways:

1. Copy `openapi/typescript/schema.ts` into the frontend's API types directory.
2. Generate an equivalent file in the frontend from `openapi/openapi.json` with
   `openapi-typescript`.

Every endpoint, request body, response body, and error shape is in those
artifacts. When the backend API changes, regenerate and verify here:

```sh
make openapi && make openapi-types && make check
```

## Run the Backend Locally

```sh
make compose-up     # local PostgreSQL
make local-setup    # migrate + seed the demo user
make dev            # API with live reload on http://localhost:7272
make smoke-auth     # (optional) verify the session flow
```

Point the frontend at the API. For the Next starter:

```sh
NEXT_PUBLIC_SERVER_URL=http://localhost:7272
```

A Vite starter uses its own env key; the value still points at the API base URL.

Demo credentials after `make local-setup` (seeded with `role = "admin"`):

```txt
demo@wow-dashboard.test
@Password
```

## Auth Flow

Sign-in and sign-up return `{ user }` JSON and set two `HttpOnly` cookies: the
access token (`Path=/`, short-lived) and the refresh token (`Path=/api/auth`).
Browser JavaScript cannot — and should not — read either, and the access token
is **never** returned in the response body.

Because auth rides on cookies, every API call must send credentials:

```ts
fetch(`${baseURL}/api/auth/me`, { credentials: 'include' });
// axios: axios.create({ baseURL, withCredentials: true })
```

Access-token handling:

- Do not store the access token in JS — there is nothing to store. The browser
  attaches the access cookie automatically, so there is no `Authorization`
  header to set from the frontend.
- On a `401`, call `POST /api/auth/refresh` with credentials. On success the API
  sets a fresh access cookie (nothing to read from the body); retry the original
  request once. If it fails, clear auth state and return the user to login.
- On sign-out, call `POST /api/auth/sign-out` with credentials; the API clears
  both cookies. Then clear frontend user state.
- State-changing requests (`POST/PUT/PATCH/DELETE`) are CSRF-protected: the API
  rejects cookie-authenticated unsafe requests that are cross-site. Same-origin
  and same-site browser requests pass automatically (see CORS & Cookies for
  cross-site deployments).
- Non-browser / API clients may instead send `Authorization: Bearer <token>` —
  the API accepts the access cookie or a Bearer header interchangeably.

Sign-up returns `201`; sign-in, refresh, and me return `200`.

## Authorization

- `/api/auth/*` and `/api/projects/*` work for any authenticated user. A user
  sees the projects they own plus any shared with them; editing requires owner
  or `editor` access and archiving/member management is owner-only.
- `/api/users/*` and `/api/system-events` are **permission-gated**: the user
  must be active and hold the required permission (`users:read`,
  `users:manage`, or `system_events:read`) through one of their roles. A user
  without it gets `403` with
  `{ "code": "forbidden", "message": "You do not have permission to perform this action." }`.
- `GET /api/auth/me` returns the user's `roles` and effective `permissions`
  arrays — use them to render menus and gate UI actions.
- `/api/roles/*` and `/api/permissions` manage roles (gated by `roles:read` /
  `roles:manage`). An admin composes a custom role from the catalog returned
  by `GET /api/permissions`; the built-in `admin` and `user` roles are
  immutable.
- New sign-ups receive the `user` role. The seeded `demo@wow-dashboard.test` user
  holds the `admin` role, which carries the `*` (all-permissions) wildcard.

## Errors

All error responses use the stable envelope — `code`, `message`, `request_id`,
and optional `details` (field-level validation errors):

```json
{ "code": "unauthorized", "message": "Authorization token missing or invalid.", "request_id": "abc-123" }
```

Never log access tokens, refresh tokens, passwords, or `Set-Cookie` values in
the browser console.

## CORS & Cookies

- The frontend origin must be listed in `CORS_ALLOWED_ORIGINS`. Local defaults
  already include common starter ports (`3000`, `5173`, …).
- The access cookie path is `/` (sent on every API call); the refresh cookie
  path is `/api/auth`.
- Production requires `REFRESH_TOKEN_COOKIE_SECURE=true` and
  `ACCESS_TOKEN_COOKIE_SECURE=true` (both default to `true` when
  `ENV=production`). Credentialed CORS is granted only to exact configured
  origins, never to wildcard-matched ones.
- For a cross-site deployment, set `SameSite=None` (requires `Secure`) on both
  cookies and an `ACCESS_TOKEN_COOKIE_DOMAIN` shared parent (e.g. `.example.com`)
  when the app and API use different subdomains; otherwise the access cookie and
  the CSRF origin check assume same-site.

## Acceptance Checklist

- Backend `make smoke-auth` passes.
- Frontend login succeeds with the seeded demo user.
- `POST /api/auth/sign-in` sets the access and refresh `Set-Cookie`s; browser JS cannot read them.
- `POST /api/auth/refresh` sends the refresh cookie and sets a fresh access cookie (no token in the body).
- `GET /api/auth/me` succeeds with cookies sent (`credentials: 'include'`), no bearer header.
- After sign-out, refresh no longer succeeds.
- No token is visible in console logs, storage, or error messages.
