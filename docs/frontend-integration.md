# Frontend Starter Integration Guide

This guide is for the next Agent wiring the wow-dashboard or Vite starter to this Go API. Do not modify the frontend repositories from this backend task. Use this document as the handoff contract for frontend work.

## Scope

This backend currently exposes:

| Area | Endpoint | Notes |
|------|----------|-------|
| Auth | `POST /api/auth/sign-up` | Returns `{ user, accessToken }` and sets the refresh cookie |
| Auth | `POST /api/auth/sign-in` | Returns `{ user, accessToken }` and sets the refresh cookie |
| Auth | `POST /api/auth/refresh` | Rotates the refresh cookie and returns a new `{ user, accessToken }` |
| Auth | `POST /api/auth/sign-out` | Revokes the current refresh token and clears the refresh cookie |
| Auth | `GET /api/auth/me` | Requires `Authorization: Bearer <accessToken>` |
| Users | `GET /api/users` | Requires `Authorization: Bearer <accessToken>`. Admin role required; non-admin authenticated users receive `403`. |

All error responses use the stable API error envelope:

```json
{
  "code": "unauthorized",
  "message": "Authorization token missing or invalid.",
  "request_id": "abc-123-def"
}
```

## Local Development Order

Run the backend locally before starting frontend integration work:

```sh
make compose-up
make local-setup
go run ./cmd/api
```

`make local-setup` starts/waits for PostgreSQL, applies migrations, and seeds the demo user. `go run ./cmd/api` can be replaced with live reload:

```sh
make dev
```

In a second terminal, verify the backend session flow:

```sh
make smoke-auth
```

Configure the frontend starter to call this backend. For the Next starter, use:

```sh
NEXT_PUBLIC_SERVER_URL=http://localhost:7272
```

If a Vite starter uses a different environment key, follow that starter's existing config convention. The value should still point to the Go API base URL, for example `http://localhost:7272`.

Demo credentials after `make local-setup`:

```txt
demo@minimals.cc
@2Minimal
```

## Contract Source

The backend repository commits two frontend-facing contract artifacts:

| File | Purpose |
|------|---------|
| `openapi/openapi.json` | OpenAPI 3.1 source contract exported from Huma route registration |
| `openapi/typescript/schema.ts` | Generated TypeScript types from `openapi/openapi.json` |

Frontend work should not hand-write duplicate DTOs for backend request or response bodies. Prefer one of these flows:

1. Copy `openapi/typescript/schema.ts` into the frontend project's API types directory.
2. Generate an equivalent file inside the frontend project from `openapi/openapi.json` using `openapi-typescript`.

When this backend API changes, regenerate and verify the contract in this backend repo:

```sh
make openapi
make openapi-types
make check
```

Frontend code can import generated path/operation types from `schema.ts`. For example, derive request/response types for `POST /api/auth/sign-in`, `GET /api/auth/me`, and `GET /api/users` from the generated `paths` type instead of manually duplicating shapes.

## Auth Flow

`POST /api/auth/sign-in` and `POST /api/auth/sign-up` return JSON:

```json
{
  "user": {
    "id": "uuid",
    "email": "demo@minimals.cc",
    "displayName": "Demo User",
    "role": "admin"
  },
  "accessToken": "jwt-access-token"
}
```

On success, the backend also sets an HttpOnly refresh token cookie. Browser JavaScript cannot read this cookie, and that is intentional. Do not try to store or inspect the refresh token in frontend state.

Frontend requests that depend on the refresh cookie must include credentials:

```ts
fetch(`${baseURL}/api/auth/refresh`, {
  method: 'POST',
  credentials: 'include',
});
```

For axios:

```ts
axios.create({
  baseURL,
  withCredentials: true,
});
```

Access token handling:

- Store the access token in memory or the starter's existing auth context for the first integration pass.
- Avoid long-term `localStorage` storage for access tokens. It is acceptable only as a temporary bridge if the starter already depends on it; prefer a safer session strategy later.
- Send `/api/auth/me` and `/api/users` with `Authorization: Bearer <accessToken>`.
- On a `401`, call `POST /api/auth/refresh` with credentials included. If refresh succeeds, update frontend access token state and retry the original request once.
- On refresh failure, clear frontend auth state and send the user back to login.
- On sign-out, call `POST /api/auth/sign-out` with credentials included, then clear frontend access token/user state.

Do not log access tokens, refresh tokens, password values, or Set-Cookie headers in browser console output.

## Users List

Endpoint:

```http
GET /api/users?page=1&pageSize=20&search=&role=&status=
Authorization: Bearer <accessToken>
```

Supported query parameters:

| Query | Default | Notes |
|-------|---------|-------|
| `page` | `1` | Page number, 1-based |
| `pageSize` | `20` | Maximum `100` |
| `search` | empty | Matches `email` or `display_name` |
| `role` | empty | Optional `admin` or `user` |
| `status` | empty | Optional `active` or `disabled` |

Response:

```json
{
  "users": [
    {
      "id": "uuid",
      "email": "demo@minimals.cc",
      "displayName": "Demo User",
      "status": "active",
      "role": "admin",
      "createdAt": "2026-05-21T00:00:00Z",
      "updatedAt": "2026-05-21T00:00:00Z"
    }
  ],
  "page": 1,
  "pageSize": 20,
  "total": 1
}
```

`users` is always an array, never `null`. The endpoint does not expose `password_hash`.

`GET /api/users` and `GET /api/users/{id}` are admin-only: the authenticated user must be active **and** carry `role = "admin"`. A non-admin authenticated user receives `403 forbidden` with the envelope:

```json
{
  "code": "forbidden",
  "message": "Admin role required.",
  "request_id": "abc-123-def"
}
```

The seeded `demo@minimals.cc` user has `role = "admin"`, so local `make smoke-auth` and the starter login flow continue to work without changes. New users created through `POST /api/auth/sign-up` default to `role = "user"` and will receive `403` from the users endpoints until promoted in the database.

## CORS And Cookie Notes

- The frontend origin must be present in `CORS_ALLOWED_ORIGINS`.
- Local defaults include common starter ports such as `http://localhost:3000` and `http://localhost:5173`.
- The refresh cookie path is `/api/auth`.
- Production requires `REFRESH_TOKEN_COOKIE_SECURE=true`.
- If the frontend and API are deployed cross-site, confirm the HTTPS, SameSite, and cookie domain strategy before launch.
- If using `SameSite=None`, browsers require `Secure`; that implies HTTPS.

## Recommended Frontend Tasks

Use this as the task list for the frontend Agent:

1. Copy or generate API contract types.
   - Source: `openapi/typescript/schema.ts` or `openapi/openapi.json`.
   - Destination: the frontend project's existing API/client types directory.
   - Do not hand-write duplicate DTOs.

2. Update the HTTP client.
   - Set `baseURL` from the starter's API URL config.
   - Enable `credentials: 'include'` or axios `withCredentials: true`.
   - Inject `Authorization: Bearer <accessToken>` for protected endpoints.
   - Add one-shot `401 -> refresh -> retry` handling.

3. Update the auth provider.
   - Wire sign-in to `POST /api/auth/sign-in`.
   - Wire sign-up to `POST /api/auth/sign-up`.
   - Wire current user bootstrap to `GET /api/auth/me`.
   - Wire session refresh to `POST /api/auth/refresh`.
   - Wire sign-out to `POST /api/auth/sign-out`.

4. Update the users list page or data hook.
   - Call `GET /api/users`.
   - Pass pagination, search, role, and status from UI state.
   - Render `users`, `page`, `pageSize`, and `total`.
   - Treat `users` as a non-null array.

5. Browser acceptance.
   - Log in with the seeded demo user.
   - Refresh the browser and keep or restore the session through `/api/auth/refresh`.
   - Load `/api/auth/me`.
   - Render the real users list.
   - Sign out and verify refresh no longer succeeds.

6. Preserve starter behavior where required.
   - If the starter still needs a mock fallback for template/demo mode, keep it behind existing feature flags or config.
   - Do not let mock code silently shadow the real Go API path during integration testing.

## Acceptance Checklist

- Backend `make smoke-auth` passes.
- Frontend login succeeds with:
  ```txt
  demo@minimals.cc
  @2Minimal
  ```
- Browser Network shows `POST /api/auth/sign-in` returns `Set-Cookie`.
- Browser JavaScript cannot read the refresh token cookie.
- `POST /api/auth/refresh` sends the cookie and returns a new `accessToken`.
- `GET /api/auth/me` succeeds with `Authorization: Bearer <accessToken>`.
- `GET /api/users` renders real users from the Go API.
- Sign-out clears frontend access token state and calls `POST /api/auth/sign-out`.
- After sign-out, refresh fails or the user must log in again.
- The refresh token is not visible in console logs, localStorage, sessionStorage, or API error messages.

## Non-Goals

- Do not modify the frontend repositories from this backend task.
- Do not design complex RBAC for users list.
- Do not introduce OAuth, Supabase, Auth0, or another auth provider.
- Do not write production deployment manifests.
- Do not change backend runtime code, OpenAPI JSON, or generated TypeScript types for this documentation-only task.
