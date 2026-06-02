// Package authctx carries request-scoped client metadata (User-Agent, IP) from
// the HTTP layer to the auth service through context, so session-issuing flows
// can record it on the refresh token for the account "active sessions" list.
// It mirrors how impersonator attribution flows through audit context.
package authctx

import "context"

// ClientInfo is the device fingerprint captured at sign-in. Empty fields are
// fine — they are stored as NULL and simply render as "unknown" in the list.
type ClientInfo struct {
	UserAgent string
	IPAddress string
}

type clientInfoKeyType struct{}

// ClientInfoContextKey keys ClientInfo in a context. It is exported so the HTTP
// middleware can set it via huma.WithValue (which needs the key value); read it
// with ClientInfoFrom rather than touching the key directly.
var ClientInfoContextKey = clientInfoKeyType{}

// WithClientInfo returns a context carrying info.
func WithClientInfo(ctx context.Context, info ClientInfo) context.Context {
	return context.WithValue(ctx, ClientInfoContextKey, info)
}

// ClientInfoFrom returns the ClientInfo stored in ctx, or a zero value when the
// middleware did not run (e.g. a background or test context).
func ClientInfoFrom(ctx context.Context) ClientInfo {
	info, _ := ctx.Value(ClientInfoContextKey).(ClientInfo)
	return info
}
