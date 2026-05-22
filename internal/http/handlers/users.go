package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/aklmans/wow-dashboard-api/internal/auth/rbac"
	authservice "github.com/aklmans/wow-dashboard-api/internal/auth/service"
	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
	"github.com/aklmans/wow-dashboard-api/internal/users/domain"
	userservice "github.com/aklmans/wow-dashboard-api/internal/users/service"
)

type UsersAuthenticator interface {
	CurrentUser(ctx context.Context, rawAccessToken string) (*authservice.PublicUser, error)
}

type UsersService interface {
	ListUsers(ctx context.Context, input userservice.ListUsersInput) (domain.ListUsersResult, error)
	GetUser(ctx context.Context, id string) (domain.User, error)
	UpdateUser(ctx context.Context, input userservice.UpdateUserInput) (domain.User, error)
}

type listUsersInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
	Page          int    `query:"page" default:"1" minimum:"1" example:"1" doc:"Page number, defaults to 1"`
	PageSize      int    `query:"pageSize" default:"20" minimum:"1" maximum:"100" example:"20" doc:"Users per page, defaults to 20 and must not exceed 100"`
	Search        string `query:"search" example:"demo" doc:"Optional email or display name search term"`
	Role          string `query:"role" example:"admin" doc:"Optional role name filter"`
	Status        string `query:"status" example:"active" doc:"Optional status filter: active or disabled"`
}

type usersListResponse struct {
	Body usersListBody
}

type usersListBody struct {
	Users    []usersListItem `json:"users" nullable:"false" doc:"Users for the requested page"`
	Page     int             `json:"page" example:"1" doc:"Current page number"`
	PageSize int             `json:"pageSize" example:"20" doc:"Users per page"`
	Total    int             `json:"total" example:"1" doc:"Total users matching the filters"`
}

type usersListItem struct {
	ID          string    `json:"id" example:"c8a89c0b-8e75-4e61-9fa0-70fb83554e66" doc:"User identifier"`
	Email       string    `json:"email" example:"demo@minimals.cc" doc:"User email address"`
	DisplayName string    `json:"displayName" example:"Demo User" doc:"User display name"`
	Status      string    `json:"status" example:"active" doc:"User status"`
	Roles       []string  `json:"roles" nullable:"false" doc:"Names of the roles assigned to the user"`
	CreatedAt   time.Time `json:"createdAt" doc:"Creation timestamp"`
	UpdatedAt   time.Time `json:"updatedAt" doc:"Last update timestamp"`
}

type getUserInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
	ID            string `path:"id" format:"uuid" example:"c8a89c0b-8e75-4e61-9fa0-70fb83554e66" doc:"User UUID"`
}

type updateUserInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
	ID            string `path:"id" format:"uuid" example:"c8a89c0b-8e75-4e61-9fa0-70fb83554e66" doc:"User UUID"`
	Body          struct {
		Status  *string   `json:"status,omitempty" enum:"active,disabled" example:"active" doc:"New status; omit to leave unchanged"`
		RoleIDs *[]string `json:"roleIds,omitempty" minItems:"1" doc:"Replacement set of role ids; omit to leave roles unchanged"`
	}
}

type userDetailResponse struct {
	Body userDetailBody
}

type userDetailBody struct {
	User usersListItem `json:"user" doc:"Requested user"`
}

func RegisterUsers(api huma.API, authSvc UsersAuthenticator, usersSvc UsersService) {
	huma.Register(api, huma.Operation{
		OperationID: "get-users",
		Method:      http.MethodGet,
		Path:        "/api/users",
		Summary:     "List users",
		Description: "Returns a paginated list of users. Requires the users:read permission.",
		Tags:        []string{"Users"},
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *listUsersInput) (*usersListResponse, error) {
		if _, err := authorizeWithPermission(ctx, authSvc, input.Authorization, rbac.PermissionUsersRead); err != nil {
			return nil, err
		}

		result, err := usersSvc.ListUsers(ctx, userservice.ListUsersInput{
			Page:     input.Page,
			PageSize: input.PageSize,
			Search:   input.Search,
			Role:     input.Role,
			Status:   input.Status,
		})
		if err != nil {
			return nil, mapUsersError(ctx, err)
		}

		return listUsersResponseFromDomain(result), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-user",
		Method:      http.MethodGet,
		Path:        "/api/users/{id}",
		Summary:     "Get user by id",
		Description: "Returns a single user by id. Requires the users:read permission.",
		Tags:        []string{"Users"},
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusNotFound,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *getUserInput) (*userDetailResponse, error) {
		if _, err := authorizeWithPermission(ctx, authSvc, input.Authorization, rbac.PermissionUsersRead); err != nil {
			return nil, err
		}

		user, err := usersSvc.GetUser(ctx, input.ID)
		if err != nil {
			return nil, mapUsersError(ctx, err)
		}

		return userDetailResponseFromDomain(user), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "patch-user",
		Method:      http.MethodPatch,
		Path:        "/api/users/{id}",
		Summary:     "Update user status or roles",
		Description: "Updates a user's status and/or replaces their role set. Requires the users:manage permission. An admin cannot change their own account.",
		Tags:        []string{"Users"},
		Responses: apiErrorResponses(api,
			http.StatusBadRequest,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusNotFound,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *updateUserInput) (*userDetailResponse, error) {
		currentUser, authErr := authorizeWithPermission(ctx, authSvc, input.Authorization, rbac.PermissionUsersManage)
		if authErr != nil {
			return nil, authErr
		}

		user, err := usersSvc.UpdateUser(ctx, userservice.UpdateUserInput{
			ActorUserID:  currentUser.ID,
			TargetUserID: input.ID,
			Status:       input.Body.Status,
			RoleIDs:      input.Body.RoleIDs,
		})
		if err != nil {
			return nil, mapUsersError(ctx, err)
		}

		return userDetailResponseFromDomain(user), nil
	})
}

// authorizeWithPermission resolves the bearer token to the current user and
// confirms they hold perm. It returns the resolved user so callers can read
// the actor id.
func authorizeWithPermission(ctx context.Context, authSvc UsersAuthenticator, authHeader string, perm rbac.Permission) (*authservice.PublicUser, huma.StatusError) {
	rawAccessToken, ok := parseBearerToken(authHeader)
	if !ok {
		return nil, apierror.Unauthorized("Authorization token missing or invalid.").ForContext(ctx)
	}
	currentUser, err := authSvc.CurrentUser(ctx, rawAccessToken)
	if err != nil {
		return nil, mapAuthError(ctx, err)
	}
	if currentUser == nil {
		return nil, apierror.Unauthorized("Authorization token missing or invalid.").ForContext(ctx)
	}
	if permErr := requirePermission(ctx, currentUser, perm); permErr != nil {
		return nil, permErr
	}
	return currentUser, nil
}

func listUsersResponseFromDomain(result domain.ListUsersResult) *usersListResponse {
	users := make([]usersListItem, 0, len(result.Users))
	for _, user := range result.Users {
		users = append(users, usersListItemFromDomain(user))
	}

	return &usersListResponse{Body: usersListBody{
		Users:    users,
		Page:     result.Page,
		PageSize: result.PageSize,
		Total:    result.Total,
	}}
}

func mapUsersError(ctx context.Context, err error) huma.StatusError {
	switch {
	case errors.Is(err, userservice.ErrSelfModification):
		return apierror.Forbidden("Administrators cannot change their own status or roles.").ForContext(ctx)
	case errors.Is(err, userservice.ErrInvalidInput):
		return apierror.ValidationFailed("Invalid users request.").ForContext(ctx)
	case errors.Is(err, userservice.ErrNotFound):
		return apierror.NotFound("User not found.").ForContext(ctx)
	default:
		return apierror.InternalError(err).ForContext(ctx)
	}
}

func userDetailResponseFromDomain(user domain.User) *userDetailResponse {
	return &userDetailResponse{Body: userDetailBody{User: usersListItemFromDomain(user)}}
}

func usersListItemFromDomain(user domain.User) usersListItem {
	roles := user.Roles
	if roles == nil {
		roles = []string{}
	}
	return usersListItem{
		ID:          user.ID.String(),
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Status:      string(user.Status),
		Roles:       roles,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
	}
}
