package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/aklmans/wow-dashboard-api/internal/auth/rbac"
	authservice "github.com/aklmans/wow-dashboard-api/internal/auth/service"
	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
)

// requirePermission returns nil when the user's effective permissions grant
// perm; otherwise it returns a 403 apierror envelope tied to the request
// context. It is safe to call on a nil user (returns the same forbidden
// envelope), so handlers can pipe their CurrentUser result through it without
// an extra nil check.
func requirePermission(ctx context.Context, user *authservice.PublicUser, perm rbac.Permission) huma.StatusError {
	if user != nil && rbac.NewSet(user.Permissions).Has(perm) {
		return nil
	}
	return apierror.Forbidden("You do not have permission to perform this action.").ForContext(ctx)
}
