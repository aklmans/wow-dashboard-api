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

// CSRFGuard blocks cross-site state-changing requests that authenticate via the
// access cookie. For unsafe methods (POST/PUT/PATCH/DELETE) carrying the access
// cookie, it requires the request to originate from a trusted context — either a
// same-origin/same-site Sec-Fetch-Site, or an Origin in the allowlist. Safe
// methods and requests without the cookie (e.g. pure Bearer API clients, or the
// unauthenticated sign-in/refresh calls) pass through untouched.
//
// This is the companion guard for AccessCookieBridge: SameSite=Lax on the cookie
// stops most cross-site sends, and this check closes the remaining gap.
func CSRFGuard(allowedOrigins []string, accessCookieName string) func(http.Handler) http.Handler {
	allow := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		if trimmed := strings.TrimSpace(origin); trimmed != "" {
			allow[trimmed] = struct{}{}
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isStateChangingMethod(r.Method) && requestHasCookie(r, accessCookieName) && !originIsTrusted(r, allow) {
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

func requestHasCookie(r *http.Request, name string) bool {
	if name == "" {
		return false
	}
	_, err := r.Cookie(name)
	return err == nil
}

// originIsTrusted prefers the Fetch Metadata signal (sent by modern browsers and
// not forgeable by cross-site script), falling back to an Origin allowlist match
// for older clients. A cookie-authenticated unsafe request with neither signal is
// treated as untrusted.
func originIsTrusted(r *http.Request, allow map[string]struct{}) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		// Only a genuine cross-site initiator is a CSRF threat; same-origin,
		// same-site, and none (user-initiated, e.g. address bar) are all safe.
		return site != "cross-site"
	}
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		_, ok := allow[origin]
		return ok
	}
	return false
}
