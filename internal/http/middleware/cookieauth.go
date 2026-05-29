package middleware

import (
	"net/http"
	"strings"

	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
)

// AccessCookieBridge lets the access token arrive as an HttpOnly cookie while
// every downstream handler keeps reading it from the Authorization header. When
// the named cookie is present and no Authorization header was sent, it copies
// the cookie value into "Authorization: Bearer <value>" before the request
// reaches Huma. A genuine Bearer header (non-browser API client) always wins, so
// both auth styles coexist.
//
// Because the cookie is now ambient (the browser attaches it automatically),
// CSRFGuard MUST run alongside this middleware to protect state-changing routes.
func AccessCookieBridge(cookieName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cookieName != "" && r.Header.Get("Authorization") == "" {
				if cookie, err := r.Cookie(cookieName); err == nil {
					if value := strings.TrimSpace(cookie.Value); value != "" {
						r.Header.Set("Authorization", "Bearer "+value)
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CSRFGuard blocks cross-site state-changing requests that authenticate via an
// ambient cookie. For unsafe methods (POST/PUT/PATCH/DELETE) that carry any of
// the named auth cookies (access OR refresh — the refresh cookie still rides on
// /api/auth/refresh and /sign-out after the short-lived access cookie expires),
// it requires the request to come from a trusted context: a same-origin (or
// user-initiated) Sec-Fetch-Site, or an allowlisted Origin. Same-site requests
// from sibling subdomains are NOT trusted on the Sec-Fetch-Site signal alone —
// they must still match the Origin allowlist. Safe methods and requests without
// an auth cookie (pure Bearer API clients, unauthenticated sign-in) pass through.
func CSRFGuard(allowedOrigins []string, cookieNames ...string) func(http.Handler) http.Handler {
	allow := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		if trimmed := strings.TrimSpace(origin); trimmed != "" {
			allow[trimmed] = struct{}{}
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isStateChangingMethod(r.Method) && requestHasAnyCookie(r, cookieNames) && !originIsTrusted(r, allow) {
				apierror.WriteResponse(w, apierror.Forbidden("Cross-site request blocked.").WithRequestID(apierror.RequestIDFromRequest(r)))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isStateChangingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func requestHasAnyCookie(r *http.Request, names []string) bool {
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, err := r.Cookie(name); err == nil {
			return true
		}
	}
	return false
}

// originIsTrusted decides whether a cookie-authenticated unsafe request comes
// from a trusted context. It prefers the Fetch Metadata signal (which a
// cross-site script cannot forge) but treats only same-origin and user-initiated
// requests as inherently trusted; a same-site request (e.g. a sibling subdomain
// under the same registrable domain, which may be untrusted) must still match
// the Origin allowlist, as must any request without Fetch Metadata.
func originIsTrusted(r *http.Request, allow map[string]struct{}) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		// Exact same origin, or a user-initiated navigation (address bar /
		// bookmark) — neither is a cross-origin CSRF vector.
		return true
	case "same-site", "cross-site":
		// A different origin (including an untrusted sibling subdomain): require
		// an explicitly allowlisted Origin.
		return originAllowed(r, allow)
	default:
		// No Fetch Metadata (older client / non-browser): fall back to the
		// Origin allowlist; a missing Origin is untrusted.
		return originAllowed(r, allow)
	}
}

func originAllowed(r *http.Request, allow map[string]struct{}) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	_, ok := allow[origin]
	return ok
}
