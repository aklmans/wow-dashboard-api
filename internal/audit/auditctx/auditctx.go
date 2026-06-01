// Package auditctx carries request-scoped audit attribution through context.
//
// During impersonation a request authenticates as the target user (the JWT
// `sub`), but the action is really performed by the admin behind the "act as"
// session (the JWT `act` claim). The per-domain audit builders stamp this
// impersonator id onto every mutation's event metadata so the audit trail
// attributes the real actor, not just the impersonated identity.
//
// This mirrors how chi's RequestID middleware exposes the request id through
// context and the builders read it with middleware.GetReqID — request-scoped
// attribution arrives via context, not threaded through every service input.
package auditctx

import (
	"context"
	"strings"
)

type ctxKey int

const impersonatorKey ctxKey = iota

// WithImpersonator returns a context that attributes the current action to the
// given impersonator id. An empty (or whitespace-only) id is a no-op, so normal
// non-impersonated requests carry nothing and audit metadata stays unset.
func WithImpersonator(ctx context.Context, impersonatorID string) context.Context {
	if strings.TrimSpace(impersonatorID) == "" {
		return ctx
	}
	return context.WithValue(ctx, impersonatorKey, impersonatorID)
}

// Impersonator returns the impersonator id stamped on ctx, or "" when the
// action is not being performed under an impersonation session.
func Impersonator(ctx context.Context) string {
	id, _ := ctx.Value(impersonatorKey).(string)
	return id
}
