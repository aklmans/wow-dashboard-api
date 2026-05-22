package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	authservice "github.com/aklmans/wow-dashboard-api/internal/auth/service"
	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
)

// adminRole is the role string an authenticated user must carry to access
// admin-only endpoints. Matches the domain UserRole constant value.
const adminRole = "admin"

// requireAdmin returns nil when user carries the admin role; otherwise it
// returns a 403 apierror envelope tied to the request context. The helper is
// safe to call on a nil user (returns the same forbidden envelope), so
// handlers can pipe their CurrentUser result through it without an extra nil
// check.
func requireAdmin(ctx context.Context, user *authservice.PublicUser) huma.StatusError {
	if user == nil || user.Role != adminRole {
		return apierror.Forbidden("Admin role required.").ForContext(ctx)
	}
	return nil
}
