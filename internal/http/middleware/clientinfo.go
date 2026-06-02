package middleware

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/aklmans/wow-dashboard-api/internal/auth/authctx"
)

// ClientInfo stashes the request's User-Agent and client IP into the context so
// session-issuing handlers (sign-in, sign-up, MFA verify) can record them on the
// refresh token for the account "active sessions" list. The IP is the immediate
// peer — the same spoof-resistant source the rate limiter uses — not a forwarded
// header. Cheap and side-effect-free, so it is applied globally.
func ClientInfo() func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		// clientIPKey returns "unknown" when there is no usable remote address;
		// store that as NULL (empty) rather than a literal "unknown".
		ip := clientIPKey(ctx.RemoteAddr())
		if ip == "unknown" {
			ip = ""
		}
		info := authctx.ClientInfo{
			UserAgent: ctx.Header("User-Agent"),
			IPAddress: ip,
		}
		next(huma.WithValue(ctx, authctx.ClientInfoContextKey, info))
	}
}
