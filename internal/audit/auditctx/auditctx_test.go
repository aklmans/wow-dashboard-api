package auditctx_test

import (
	"context"
	"testing"

	"github.com/aklmans/wow-dashboard-api/internal/audit/auditctx"
)

func TestImpersonatorRoundTrip(t *testing.T) {
	ctx := auditctx.WithImpersonator(context.Background(), "admin-123")
	if got := auditctx.Impersonator(ctx); got != "admin-123" {
		t.Fatalf("Impersonator = %q, want %q", got, "admin-123")
	}
}

func TestImpersonatorAbsentByDefault(t *testing.T) {
	if got := auditctx.Impersonator(context.Background()); got != "" {
		t.Fatalf("Impersonator on a bare context = %q, want empty", got)
	}
}

func TestWithImpersonatorEmptyIsNoOp(t *testing.T) {
	for _, in := range []string{"", "   "} {
		ctx := auditctx.WithImpersonator(context.Background(), in)
		if got := auditctx.Impersonator(ctx); got != "" {
			t.Fatalf("WithImpersonator(%q) then Impersonator = %q, want empty (no-op)", in, got)
		}
	}
}
