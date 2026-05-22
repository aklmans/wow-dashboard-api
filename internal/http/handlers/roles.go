package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/aklmans/wow-dashboard-api/internal/auth/rbac"
	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
	rolesdomain "github.com/aklmans/wow-dashboard-api/internal/roles/domain"
	rolesservice "github.com/aklmans/wow-dashboard-api/internal/roles/service"
)

// RolesService is the role management use-case port required by the handlers.
type RolesService interface {
	ListRoles(ctx context.Context) ([]rolesdomain.Role, error)
	GetRole(ctx context.Context, id string) (rolesdomain.Role, error)
	CreateRole(ctx context.Context, input rolesservice.CreateRoleInput) (rolesdomain.Role, error)
	UpdateRole(ctx context.Context, input rolesservice.UpdateRoleInput) (rolesdomain.Role, error)
	DeleteRole(ctx context.Context, actorUserID string, id string) error
}

type roleBody struct {
	ID          string    `json:"id" example:"c8a89c0b-8e75-4e61-9fa0-70fb83554e66" doc:"Role identifier"`
	Name        string    `json:"name" example:"auditor" doc:"Role name"`
	Description string    `json:"description" example:"Read-only access to audit data" doc:"Role description"`
	IsSystem    bool      `json:"isSystem" example:"false" doc:"Whether this is a built-in role that cannot be modified"`
	Permissions []string  `json:"permissions" nullable:"false" doc:"Permission strings granted by the role"`
	UserCount   int       `json:"userCount" example:"3" doc:"Number of users currently assigned this role"`
	CreatedAt   time.Time `json:"createdAt" doc:"Creation timestamp"`
	UpdatedAt   time.Time `json:"updatedAt" doc:"Last update timestamp"`
}

type rolesListResponse struct {
	Body rolesListBody
}

type rolesListBody struct {
	Roles []roleBody `json:"roles" nullable:"false" doc:"All roles"`
}

type roleDetailResponse struct {
	Body roleDetailBody
}

type roleDetailBody struct {
	Role roleBody `json:"role" doc:"Requested role"`
}

type roleCreatedResponse struct {
	Body roleDetailBody
}

type roleDeletedResponse struct {
	Body roleDeletedBody
}

type roleDeletedBody struct {
	Success bool `json:"success" example:"true" doc:"Deletion success flag"`
}

type permissionsListResponse struct {
	Body permissionsListBody
}

type permissionsListBody struct {
	Permissions []string `json:"permissions" nullable:"false" doc:"Every permission an admin can assign to a role"`
}

type listRolesInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
}

type getRoleInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
	ID            string `path:"id" format:"uuid" example:"c8a89c0b-8e75-4e61-9fa0-70fb83554e66" doc:"Role UUID"`
}

type createRoleInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
	Body          struct {
		Name        string   `json:"name" minLength:"1" maxLength:"50" example:"auditor" doc:"Unique role name"`
		Description string   `json:"description,omitempty" maxLength:"280" example:"Read-only access to audit data" doc:"Optional role description"`
		Permissions []string `json:"permissions,omitempty" doc:"Permission strings to grant; each must be an assignable catalog permission"`
	}
}

type updateRoleInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
	ID            string `path:"id" format:"uuid" example:"c8a89c0b-8e75-4e61-9fa0-70fb83554e66" doc:"Role UUID"`
	Body          struct {
		Name        *string   `json:"name,omitempty" minLength:"1" maxLength:"50" doc:"New name; omit to leave unchanged"`
		Description *string   `json:"description,omitempty" maxLength:"280" doc:"New description; omit to leave unchanged"`
		Permissions *[]string `json:"permissions,omitempty" doc:"Replacement permission set; omit to leave unchanged"`
	}
}

type deleteRoleInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
	ID            string `path:"id" format:"uuid" example:"c8a89c0b-8e75-4e61-9fa0-70fb83554e66" doc:"Role UUID"`
}

type listPermissionsInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
}

// RegisterRoles wires the role management endpoints. Reading roles requires
// the roles:read permission; creating, updating, and deleting them requires
// roles:manage. The built-in admin and user roles are immutable through the
// API.
func RegisterRoles(api huma.API, authSvc UsersAuthenticator, rolesSvc RolesService) {
	huma.Register(api, huma.Operation{
		OperationID: "list-roles",
		Method:      http.MethodGet,
		Path:        "/api/roles",
		Summary:     "List roles",
		Description: "Returns every role with its permissions and assigned-user count. Requires the roles:read permission.",
		Tags:        []string{"Roles"},
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *listRolesInput) (*rolesListResponse, error) {
		if _, err := authorizeWithPermission(ctx, authSvc, input.Authorization, rbac.PermissionRolesRead); err != nil {
			return nil, err
		}

		roles, err := rolesSvc.ListRoles(ctx)
		if err != nil {
			return nil, mapRolesError(ctx, err)
		}

		body := rolesListBody{Roles: make([]roleBody, 0, len(roles))}
		for _, role := range roles {
			body.Roles = append(body.Roles, roleResponseFromDomain(role))
		}
		return &rolesListResponse{Body: body}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-role",
		Method:      http.MethodGet,
		Path:        "/api/roles/{id}",
		Summary:     "Get role by id",
		Description: "Returns a single role by id. Requires the roles:read permission.",
		Tags:        []string{"Roles"},
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusNotFound,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *getRoleInput) (*roleDetailResponse, error) {
		if _, err := authorizeWithPermission(ctx, authSvc, input.Authorization, rbac.PermissionRolesRead); err != nil {
			return nil, err
		}

		role, err := rolesSvc.GetRole(ctx, input.ID)
		if err != nil {
			return nil, mapRolesError(ctx, err)
		}
		return &roleDetailResponse{Body: roleDetailBody{Role: roleResponseFromDomain(role)}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-role",
		Method:        http.MethodPost,
		Path:          "/api/roles",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a role",
		Description:   "Creates a custom role from assignable catalog permissions. Requires the roles:manage permission.",
		Tags:          []string{"Roles"},
		Responses: apiErrorResponses(api,
			http.StatusBadRequest,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusConflict,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *createRoleInput) (*roleCreatedResponse, error) {
		currentUser, authErr := authorizeWithPermission(ctx, authSvc, input.Authorization, rbac.PermissionRolesManage)
		if authErr != nil {
			return nil, authErr
		}

		role, err := rolesSvc.CreateRole(ctx, rolesservice.CreateRoleInput{
			ActorUserID: currentUser.ID,
			Name:        input.Body.Name,
			Description: input.Body.Description,
			Permissions: input.Body.Permissions,
		})
		if err != nil {
			return nil, mapRolesError(ctx, err)
		}
		return &roleCreatedResponse{Body: roleDetailBody{Role: roleResponseFromDomain(role)}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-role",
		Method:      http.MethodPatch,
		Path:        "/api/roles/{id}",
		Summary:     "Update a role",
		Description: "Updates a custom role's name, description, and/or permission set. Requires the roles:manage permission. System roles cannot be modified.",
		Tags:        []string{"Roles"},
		Responses: apiErrorResponses(api,
			http.StatusBadRequest,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusNotFound,
			http.StatusConflict,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *updateRoleInput) (*roleDetailResponse, error) {
		currentUser, authErr := authorizeWithPermission(ctx, authSvc, input.Authorization, rbac.PermissionRolesManage)
		if authErr != nil {
			return nil, authErr
		}

		role, err := rolesSvc.UpdateRole(ctx, rolesservice.UpdateRoleInput{
			ActorUserID: currentUser.ID,
			RoleID:      input.ID,
			Name:        input.Body.Name,
			Description: input.Body.Description,
			Permissions: input.Body.Permissions,
		})
		if err != nil {
			return nil, mapRolesError(ctx, err)
		}
		return &roleDetailResponse{Body: roleDetailBody{Role: roleResponseFromDomain(role)}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-role",
		Method:      http.MethodDelete,
		Path:        "/api/roles/{id}",
		Summary:     "Delete a role",
		Description: "Deletes a custom role. Requires the roles:manage permission. System roles, and roles still assigned to users, cannot be deleted.",
		Tags:        []string{"Roles"},
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusNotFound,
			http.StatusConflict,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *deleteRoleInput) (*roleDeletedResponse, error) {
		currentUser, authErr := authorizeWithPermission(ctx, authSvc, input.Authorization, rbac.PermissionRolesManage)
		if authErr != nil {
			return nil, authErr
		}

		if err := rolesSvc.DeleteRole(ctx, currentUser.ID, input.ID); err != nil {
			return nil, mapRolesError(ctx, err)
		}
		return &roleDeletedResponse{Body: roleDeletedBody{Success: true}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-permissions",
		Method:      http.MethodGet,
		Path:        "/api/permissions",
		Summary:     "List assignable permissions",
		Description: "Returns the catalog of permissions an admin can assign to a role. Requires the roles:read permission.",
		Tags:        []string{"Roles"},
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *listPermissionsInput) (*permissionsListResponse, error) {
		if _, err := authorizeWithPermission(ctx, authSvc, input.Authorization, rbac.PermissionRolesRead); err != nil {
			return nil, err
		}

		permissions := make([]string, 0, len(rbac.Catalog))
		for _, p := range rbac.Catalog {
			permissions = append(permissions, string(p))
		}
		return &permissionsListResponse{Body: permissionsListBody{Permissions: permissions}}, nil
	})
}

func roleResponseFromDomain(role rolesdomain.Role) roleBody {
	permissions := role.Permissions
	if permissions == nil {
		permissions = []string{}
	}
	return roleBody{
		ID:          role.ID.String(),
		Name:        role.Name,
		Description: role.Description,
		IsSystem:    role.IsSystem,
		Permissions: permissions,
		UserCount:   role.UserCount,
		CreatedAt:   role.CreatedAt,
		UpdatedAt:   role.UpdatedAt,
	}
}

func mapRolesError(ctx context.Context, err error) huma.StatusError {
	switch {
	case errors.Is(err, rolesservice.ErrInvalidInput):
		return apierror.ValidationFailed("Invalid roles request.").ForContext(ctx)
	case errors.Is(err, rolesservice.ErrNotFound):
		return apierror.NotFound("Role not found.").ForContext(ctx)
	case errors.Is(err, rolesservice.ErrNameConflict):
		return apierror.Conflict("A role with that name already exists.").ForContext(ctx)
	case errors.Is(err, rolesservice.ErrRoleInUse):
		return apierror.Conflict("This role is assigned to one or more users and cannot be deleted.").ForContext(ctx)
	case errors.Is(err, rolesservice.ErrSystemRole):
		return apierror.Conflict("Built-in system roles cannot be modified or deleted.").ForContext(ctx)
	default:
		return apierror.InternalError(err).ForContext(ctx)
	}
}
