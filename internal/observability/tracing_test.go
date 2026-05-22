package observability_test

import (
	"context"
	"testing"
	"time"

	"github.com/aklmans/wow-dashboard-api/internal/observability"
)

func TestSetupTracing(t *testing.T) {
	ctx := context.Background()

	t.Run("no endpoint yields a callable no-op shutdown", func(t *testing.T) {
		shutdown, err := observability.SetupTracing(ctx, "test-service", "")
		if err != nil {
			t.Fatalf("SetupTracing returned error: %v", err)
		}
		if shutdown == nil {
			t.Fatal("shutdown func is nil")
		}
		if err := shutdown(ctx); err != nil {
			t.Fatalf("no-op shutdown returned error: %v", err)
		}
	})

	t.Run("with an endpoint installs an exporter and a callable shutdown", func(t *testing.T) {
		shutdown, err := observability.SetupTracing(ctx, "test-service", "http://localhost:4318")
		if err != nil {
			t.Fatalf("SetupTracing returned error: %v", err)
		}
		if shutdown == nil {
			t.Fatal("shutdown func is nil")
		}
		// No spans are produced in this test, so shutting down flushes nothing
		// and never contacts the (absent) collector.
		shutdownCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			t.Fatalf("shutdown returned error: %v", err)
		}
	})
}
