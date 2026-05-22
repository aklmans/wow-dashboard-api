package middleware

import "net/http"

// SecurityHeaders returns middleware that applies baseline security response
// headers to every response:
//
//   - X-Content-Type-Options: nosniff — stop browsers MIME-sniffing a response.
//   - X-Frame-Options: DENY — this is a JSON API and is never meant to be framed.
//   - Referrer-Policy: no-referrer — never leak the request URL to other origins.
//
// When enableHSTS is true a Strict-Transport-Security header is also set. Pass
// it only when the service is reached over HTTPS: HSTS is meaningless on plain
// HTTP and browsers ignore it there, so advertising it in development is noise.
func SecurityHeaders(enableHSTS bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := w.Header()
			header.Set("X-Content-Type-Options", "nosniff")
			header.Set("X-Frame-Options", "DENY")
			header.Set("Referrer-Policy", "no-referrer")
			if enableHSTS {
				header.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}
