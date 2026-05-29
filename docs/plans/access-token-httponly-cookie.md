# Plan: Move the JWT access token to an HttpOnly cookie

Status: DONE — backend (PR 1) + frontend (PR 2) implemented and verified live against local PostgreSQL (`make smoke-auth` + a Playwright browser pass). · Scope: cross-repo (`wow-dashboard-api` + `next-ts-starter`) · Risk: high (auth core)

## Goal

Stop the access token from living in JS-readable `sessionStorage` (XSS-exfiltratable)
and stop sending it as a JS-set `Authorization` header. Instead the API issues the
access token as an **HttpOnly cookie**, the browser sends it automatically, and a Next
edge middleware can finally gate `/dashboard/*` on auth presence. This subsumes Phase-0
tasks 0.4 (CSP can later drop `unsafe-*` without token risk), 0.5 (token out of
sessionStorage), and 0.6 (edge guard).

## Key design decision: a cookie→Authorization bridge (minimal churn)

The backend has **no central auth middleware** — all 23 protected endpoints read
`input.Authorization` (`header:"Authorization"`) and call `parseBearerToken` via
`authorizeWithPermission` (`internal/http/handlers/users.go:194`), `authenticateProjects`
(`internal/http/handlers/projects.go:258`), or directly (`auth.go:381` etc.).

Rather than rewrite 23 input structs, add **one chi middleware** that, when an access
cookie is present and no `Authorization` header is set, injects
`Authorization: Bearer <cookie value>` into the request before Huma runs. Every existing
handler then authenticates unchanged — whether the token arrived as a cookie (browser) or
a Bearer header (API clients / tests). This keeps the security-critical handler code
**untouched** and back-compatible.

Trade-off it forces: a cookie sent automatically by the browser reintroduces **CSRF**
(the reason Bearer-in-header was CSRF-safe). So the bridge MUST be paired with CSRF
defense (below).

## Cookie design

New access cookie, mirroring the existing refresh cookie helpers
(`auth.go:540 newRefreshCookie` / `552 clearRefreshCookie`, `RefreshCookieConfig`
`auth.go:139`):

- Name: `wow_dashboard_access_token` (configurable).
- `HttpOnly: true`, `Secure` = same rule as refresh (forced true in production),
  `SameSite` = configurable, **default `Lax`**.
- `Path: /` (must be `/` so it rides on every API call AND is visible to a same-site
  Next middleware; the refresh cookie stays `Path=/api/auth`).
- `MaxAge` = `JWT_ACCESS_TOKEN_TTL_SECONDS` (currently 900s / 15m; prod-validated 60–3600).
- Optional `Domain` (new config, empty by default = host-only). Set to the shared parent
  (e.g. `.example.com`) in prod when the app and API are different subdomains, so both
  the SPA origin and the Next edge see the cookie.

## CSRF strategy (required because of the bridge)

1. **SameSite=Lax** on the access cookie blocks the cookie on cross-site POST/subresource
   requests — covers the common case when app+API share a registrable domain.
2. **Origin allowlist check** middleware for unsafe methods (`POST/PUT/PATCH/DELETE`):
   require the `Origin` (or `Sec-Fetch-Site`) to match the configured CORS allowlist;
   reject otherwise with a `403 forbidden` apierror. Runs only when auth came from the
   cookie bridge (a real Bearer header from a non-browser client is exempt — it can't be
   CSRF'd). Safe methods (`GET/HEAD/OPTIONS`) are exempt.
3. For cross-site deployments that must use `SameSite=None`, (1) no longer protects, but
   (2) still does; a double-submit CSRF token is the documented future upgrade.

## Deployment topology note (affects edge guard 0.6)

Next middleware runs on the **frontend** origin and only sees cookies scoped to that
origin. The auth cookies are issued by the **API** origin. Therefore the edge guard can
read them only when:
- dev: `localhost:8083` ↔ `localhost:7272` are the same host → host-only cookie is shared
  across ports → works out of the box; OR
- prod: cookie `Domain=.example.com` (shared parent) or app+API served same-origin behind
  one gateway.

If the deployment is genuinely cross-site, the edge guard degrades to "no cookie visible →
always treat as unauthenticated at the edge" which is wrong, so in that case we keep the
client-side guard and skip the edge redirect. The middleware will be written to **only
gate when it can see an auth cookie**, and otherwise fall through (backend stays the
authority either way).

## Backend changes (`wow-dashboard-api`)

1. `internal/config/config.go` — add `AccessTokenCookieName`, `AccessTokenCookieSecure`
   (prod-forced true), `AccessTokenCookieSameSite` (validate lax/strict/none),
   `AccessTokenCookieDomain` (optional). Update `.env.example`, README config table, and
   `config_test.go` (valid + invalid + prod-validation cases). Reuse `JWTAccessTokenTTL()`
   for MaxAge.
2. `internal/http/handlers/auth.go` — add `newAccessCookie(cfg,value)` /
   `clearAccessCookie(cfg)` (mirror refresh helpers); have `sessionResponse` set BOTH
   cookies on sign-in/sign-up/refresh; clear BOTH on sign-out and on change-password
   (already clears refresh). **Remove `accessToken` from `authSessionBody` (`auth.go:55`)**
   — contract change.
3. `internal/http/middleware/` — new `AccessCookieBridge(cfg)` (cookie→Authorization) and
   `CSRFGuard(allowedOrigins)` (Origin check for unsafe methods). Wire in `app.go` after
   CORS/security-headers, before the Huma API. `app.go:refreshCookieConfig` extended to
   also produce the access-cookie config.
4. OpenAPI: declare a cookie auth scheme; regenerate `openapi/openapi.json` +
   `openapi/typescript/schema.ts` (`make openapi openapi-types`); the `accessToken` field
   disappears from the session response schema.
5. Tests: handler tests assert Set-Cookie on sign-in/refresh and cleared on sign-out;
   middleware tests for the bridge (cookie present → handler authenticates) and CSRF guard
   (bad Origin on POST → 403; good Origin → pass; GET exempt). Keep Bearer-header tests
   green (fallback path).

## Frontend changes (`next-ts-starter`)

1. `src/lib/axios.ts` — delete the request interceptor's Bearer injection (`:30-38`);
   keep `withCredentials:true`. `performRefresh` no longer reads a token from the body
   (`:58-63`) — it just POSTs `/api/auth/refresh` (cookie set server-side) and resolves;
   the 401 interceptor then replays the original request (fresh cookie rides along).
   `clearSessionAndBounce` drops sessionStorage removal.
2. `src/auth/context/jwt/utils.ts` — `setSession` no longer writes sessionStorage / sets
   the Authorization default; `isValidToken` removed (token is opaque to JS now).
3. `src/auth/context/jwt/auth-provider.tsx` — `checkUserSession` just calls
   `GET /api/auth/me` with credentials; on 401 the interceptor refreshes; no token state.
   Stop spreading `accessToken` onto the context user.
4. `src/auth/context/jwt/constant.ts` — remove `JWT_STORAGE_KEY`.
5. `src/proxy.ts` (new — Next 16 `proxy` convention, formerly `middleware`) — opt-in via
   `NEXT_PUBLIC_EDGE_AUTH_GUARD`; if no auth cookie present on a `/dashboard/*` request,
   redirect to sign-in with `returnTo`. Only gates when a cookie is visible (see topology
   note). Backend remains the real authority.
6. Re-sync `src/lib/api-schema.ts` from the regenerated backend types; drop any
   `accessToken` reads. Update/extend tests (auth provider boot, axios refresh) — pairs
   with the Phase-4 MSW work.

## Rollout (coordinated, backend first)

1. Backend PR: access cookie + bridge + CSRF guard + config + tests + OpenAPI. Keep Bearer
   fallback so nothing breaks before the frontend ships. Verify with `make` non-container
   subset + a local PG smoke (sign-in sets cookie, `/me` works via cookie, sign-out clears).
2. Frontend PR: drop sessionStorage/Bearer, add edge middleware, re-sync types. Verify the
   full flow in a browser against local PG (login persists across hard refresh; logout;
   401→refresh).
3. Follow-up: 0.4 CSP — now safe to pursue nonce + drop `unsafe-eval`.

## Risks / watch-items

- CSRF correctness is the #1 risk — the bridge makes the cookie ambient. The Origin guard
  + SameSite=Lax must both be verified by tests before merge.
- Cross-site topology breaks the edge guard and SameSite=Lax; handled by config
  (`Domain`, `SameSite`) + graceful middleware fallback.
- Contract change (`accessToken` removed from body) must land in the same backend PR as
  the regenerated OpenAPI/TS artifacts and be reflected in the frontend PR.
- Tests/e2e that log in via Bearer keep working through the fallback; the new browser path
  is covered by the cookie tests + the Phase-0.7 E2E (real API + Postgres service container).
