package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// metricsScrapePath is the exposition endpoint, excluded from instrumentation
// so a scrape does not inflate its own counters.
const metricsScrapePath = "/metrics"

// Metrics holds Prometheus HTTP instrumentation on a dedicated registry, so it
// is isolated from the global default registry and safe to use in tests.
type Metrics struct {
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// NewMetrics builds the HTTP metrics and registers them alongside the standard
// Go runtime and process collectors.
func NewMetrics() *Metrics {
	requests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total HTTP requests, labeled by method, matched route, and status.",
	}, []string{"method", "route", "status"})
	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency in seconds, labeled by method and matched route.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route"})

	registry := prometheus.NewRegistry()
	registry.MustRegister(
		requests,
		duration,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return &Metrics{registry: registry, requests: requests, duration: duration}
}

// Middleware records request count and latency for every request other than
// the metrics scrape itself. The route label is the matched Chi pattern (e.g.
// "/api/users/{id}"), not the raw path, so label cardinality stays bounded.
func (m *Metrics) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == metricsScrapePath {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			recorder := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(recorder, r)

			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				route = "unmatched"
			}
			status := recorder.Status()
			if status == 0 {
				status = http.StatusOK
			}
			m.requests.WithLabelValues(r.Method, route, strconv.Itoa(status)).Inc()
			m.duration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
		})
	}
}

// Handler serves the Prometheus exposition endpoint for this registry.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
