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
demo@minimals.cc
@2Minimal
```

## Auth Flow

Sign-in and sign-up return `{ user, accessToken }` JSON and additionally set an
`HttpOnly` refresh-token cookie. Browser JavaScript cannot — and should not —
read that cookie; never store or inspect the refresh token in frontend state.

Requests that rely on the refresh cookie must send credentials:

```ts
fetch(`${baseURL}/api/auth/refresh`, { method: 'POST', credentials: 'include' });
// axios: axios.create({ baseURL, withCredentials: true })
```

Access-token handling:

- Keep the access token in memory or the starter's existing auth context.
  Avoid long-term `localStorage`; use it only as a temporary bridge if the
  starter already depends on it.
- Send protected requests with `Authorization: Bearer <accessToken>`.
- On a `401`, call `POST /api/auth/refresh` with credentials. If it succeeds,
  update the access token and retry the original request once. If it fails,
  clear auth state and return the user to login.
- On sign-out, call `POST /api/auth/sign-out` with credentials, then clear
  frontend access-token/user state.

Sign-up returns `201`; sign-in, refresh, and me return `200`.

## Authorization

- `/api/auth/*` and `/api/projects/*` work for any authenticated user. A user
  sees the projects they own plus any shared with them; editing requires owner
  or `editor` access and archiving/member management is owner-only.
- `/api/users/*` and `/api/system-events` are **admin-only**: the user must be
  active and carry `role = "admin"`. A non-admin authenticated user gets `403`
  with `{ "code": "forbidden", "message": "Admin role required." }`.
- New sign-ups default to `role = "user"` and are promoted in the database.
  The seeded `demo@minimals.cc` user is already `admin`.

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
- The refresh cookie path is `/api/auth`.
- Production requires `REFRESH_TOKEN_COOKIE_SECURE=true`. Credentialed CORS is
  granted only to exact configured origins, never to wildcard-matched ones.
- For a cross-site deployment, confirm the HTTPS / SameSite / cookie-domain
  strategy before launch (`SameSite=None` requires `Secure`).

## Acceptance Checklist

- Backend `make smoke-auth` passes.
- Frontend login succeeds with the seeded demo user.
- `POST /api/auth/sign-in` returns `Set-Cookie`; browser JS cannot read it.
- `POST /api/auth/refresh` sends the cookie and returns a fresh `accessToken`.
- `GET /api/auth/me` succeeds with the bearer token.
- After sign-out, refresh no longer succeeds.
- No token is visible in console logs, storage, or error messages.
