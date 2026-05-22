package middleware

import (
	"net/http"
	"regexp"
	"strings"
)

// CORS returns a middleware that handles CORS requests.
// It supports exact origin checks and simple glob-style wildcards (e.g., "https://*-project.vercel.app").
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	var patterns []*regexp.Regexp
	var exactOrigins []string

	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		if strings.Contains(origin, "*") {
			// Translate simple glob wildcard (*) to standard regex matching a-z, A-Z, 0-9 and hyphens
			regexStr := "^" + regexp.QuoteMeta(origin) + "$"
			regexStr = strings.ReplaceAll(regexStr, `\*`, `[a-zA-Z0-9\-]+`)
			if re, err := regexp.Compile(regexStr); err == nil {
				patterns = append(patterns, re)
			}
		} else {
			exactOrigins = append(exactOrigins, origin)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			allowed := false
			exactMatch := false
			// Check exact matches first
			for _, exact := range exactOrigins {
				if exact == origin {
					allowed = true
					exactMatch = true
					break
				}
			}

			// Check wildcard patterns if not allowed yet
			if !allowed {
				for _, re := range patterns {
					if re.MatchString(origin) {
						allowed = true
						break
					}
				}
			}

			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, Accept")
				// Credentialed CORS is granted only to exact, explicitly owned
				// origins. A wildcard-matched origin never receives
				// Allow-Credentials, so a broad pattern cannot be abused to
				// make authenticated cross-origin requests.
				if exactMatch {
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}
				w.Header().Set("Access-Control-Max-Age", "86400")
			}

			// Handle preflight request
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
