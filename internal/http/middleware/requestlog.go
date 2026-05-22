package middleware

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// RequestLogger records one structured log event per HTTP request.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			attrs := []any{
				"request_id", chimiddleware.GetReqID(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"duration_ms", durationMilliseconds(time.Since(start)),
				"remote_addr", r.RemoteAddr,
				"user_agent", r.UserAgent(),
			}
			if query := sanitizeRawQuery(r.URL.RawQuery); query != "" {
				attrs = append(attrs, "query", query)
			}

			logger.InfoContext(r.Context(), "http_request", attrs...)
		})
	}
}

func durationMilliseconds(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}

func sanitizeRawQuery(raw string) string {
	if raw == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "[unparseable]"
	}
	for key := range values {
		if isSensitiveName(key) {
			values[key] = []string{"[REDACTED]"}
		}
	}
	return values.Encode()
}

func isSensitiveName(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	compact := strings.NewReplacer("_", "", "-", "").Replace(normalized)
	return strings.Contains(compact, "token") ||
		strings.Contains(compact, "password") ||
		normalized == "authorization" ||
		normalized == "cookie" ||
		normalized == "set-cookie"
}
