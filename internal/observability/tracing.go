// Package observability wires runtime instrumentation — currently distributed
// tracing — into the application.
package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// SetupTracing configures the global OpenTelemetry tracer.
//
// When endpoint is empty the global tracer is left as the no-op default:
// instrumentation still calls into it, but spans are never recorded or
// exported, so there is no infrastructure requirement and negligible
// overhead. When endpoint is a non-empty OTLP/HTTP collector URL, spans are
// batched and exported there.
//
// The returned shutdown function flushes any pending spans and must be called
// before the process exits.
func SetupTracing(ctx context.Context, serviceName string, endpoint string) (func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }
	if endpoint == "" {
		return noop, nil
	}

	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	if err != nil {
		return noop, fmt.Errorf("observability: create OTLP trace exporter: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewSchemaless(
			attribute.String("service.name", serviceName),
		)),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return provider.Shutdown, nil
}
