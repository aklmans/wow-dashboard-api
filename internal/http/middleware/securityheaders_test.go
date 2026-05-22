package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aklmans/wow-dashboard-api/internal/http/middleware"
)

func TestSecurityHeaders(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("sets baseline headers and omits HSTS when disabled", func(t *testing.T) {
		rec := httptest.NewRecorder()
		middleware.SecurityHeaders(false)(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

		for header, want := range map[string]string{
			"X-Content-Type-Options": "nosniff",
			"X-Frame-Options":        "DENY",
			"Referrer-Policy":        "no-referrer",
		} {
			if got := rec.Header().Get(header); got != want {
				t.Errorf("%s = %q, want %q", header, got, want)
			}
		}
		if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
			t.Errorf("Strict-Transport-Security = %q, want unset when HSTS is disabled", got)
		}
	})

	t.Run("adds HSTS when enabled", func(t *testing.T) {
		rec := httptest.NewRecorder()
		middleware.SecurityHeaders(true)(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

		if got := rec.Header().Get("Strict-Transport-Security"); got == "" {
			t.Error("Strict-Transport-Security header missing when HSTS is enabled")
		}
	})
}
