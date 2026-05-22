package middleware

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// TraceRoute names the active server span after Chi has matched a route, so a
// tracing backend aggregates by endpoint (e.g. "GET /api/users/{id}") rather
// than by raw path. It also records the matched route as the http.route
// attribute. When tracing is disabled the active span is a no-op and these
// calls cost nothing.
func TraceRoute() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)

			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				return
			}
			span := trace.SpanFromContext(r.Context())
			span.SetName(r.Method + " " + route)
			span.SetAttributes(attribute.String("http.route", route))
		})
	}
}
