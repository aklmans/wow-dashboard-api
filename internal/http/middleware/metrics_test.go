package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/aklmans/wow-dashboard-api/internal/http/middleware"
)

func TestMetricsMiddleware(t *testing.T) {
	metrics := middleware.NewMetrics()
	router := chi.NewRouter()
	router.Use(metrics.Middleware())
	router.Get("/api/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	router.Handle("/metrics", metrics.Handler())

	// Drive a request so a metric is recorded.
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/items/abc", nil))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics status = %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	// The route label must be the matched pattern, not the raw path, to keep
	// label cardinality bounded.
	if !strings.Contains(body, `http_requests_total{method="GET",route="/api/items/{id}",status="200"} 1`) {
		t.Fatalf("metrics output missing the expected request counter; body:\n%s", body)
	}
	if strings.Contains(body, `route="/metrics"`) {
		t.Fatal("the scrape endpoint instrumented itself")
	}
}
