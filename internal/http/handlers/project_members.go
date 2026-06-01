package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/aklmans/wow-dashboard-api/internal/audit/auditctx"
	"github.com/aklmans/wow-dashboard-api/internal/projects/domain"
	projectservice "github.com/aklmans/wow-dashboard-api/internal/projects/service"
)

type listProjectMembersInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
	ProjectID     string `path:"id" format:"uuid" example:"c8a89c0b-8e75-4e61-9fa0-70fb83554e66" doc:"Project UUID"`
}

type addProjectMemberInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
	ProjectID     string `path:"id" format:"uuid" example:"c8a89c0b-8e75-4e61-9fa0-70fb83554e66" doc:"Project UUID"`
	Body          struct {
		Email string `json:"email" format:"email" maxLength:"320" example:"teammate@example.com" doc:"Email of the user to grant access to"`
		Role  string `json:"role" enum:"viewer,editor" example:"editor" doc:"Access role to grant"`
	}
}

type updateProjectMemberInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
	ProjectID     string `path:"id" format:"uuid" example:"c8a89c0b-8e75-4e61-9fa0-70fb83554e66" doc:"Project UUID"`
	UserID        string `path:"userId" format:"uuid" example:"d1f6b2a4-1c3e-4a8b-9d2f-6e7a0b1c2d3e" doc:"Member user UUID"`
	Body          struct {
		Role string `json:"role" enum:"viewer,editor" example:"viewer" doc:"New access role"`
	}
}

type deleteProjectMemberInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
	ProjectID     string `path:"id" format:"uuid" example:"c8a89c0b-8e75-4e61-9fa0-70fb83554e66" doc:"Project UUID"`
	UserID        string `path:"userId" format:"uuid" example:"d1f6b2a4-1c3e-4a8b-9d2f-6e7a0b1c2d3e" doc:"Member user UUID"`
}

type projectMemberItem struct {
	UserID      string    `json:"userId" example:"d1f6b2a4-1c3e-4a8b-9d2f-6e7a0b1c2d3e" doc:"Member user identifier"`
	Email       string    `json:"email" example:"teammate@example.com" doc:"Member email address"`
	DisplayName string    `json:"displayName" example:"Team Mate" doc:"Member display name"`
	Role        string    `json:"role" example:"editor" doc:"Member access role"`
	CreatedAt   time.Time `json:"createdAt" doc:"Grant creation timestamp"`
	UpdatedAt   time.Time `json:"updatedAt" doc:"Grant last update timestamp"`
}

type projectMembersBody struct {
	Members []projectMemberItem `json:"members" nullable:"false" doc:"Users granted non-owner access to the project"`
}

type projectMembersResponse struct {
	Body projectMembersBody
}

type projectMemberGrant struct {
	UserID    string    `json:"userId" example:"d1f6b2a4-1c3e-4a8b-9d2f-6e7a0b1c2d3e" doc:"Member user identifier"`
	Role      string    `json:"role" example:"editor" doc:"Member access role"`
	CreatedAt time.Time `json:"createdAt" doc:"Grant creation timestamp"`
	UpdatedAt time.Time `json:"updatedAt" doc:"Grant last update timestamp"`
}

type projectMemberBody struct {
	Member projectMemberGrant `json:"member" doc:"The project access grant"`
}

type projectMemberResponse struct {
	Body projectMemberBody
}

type projectMemberDeletedBody struct {
	Success bool `json:"success" example:"true" doc:"Operation success flag"`
}

type projectMemberDeletedResponse struct {
	Body projectMemberDeletedBody
}

// registerProjectMembers wires the project sharing endpoints. Listing members
// is available to any user with access to the project; adding, updating, and
// removing members is restricted to the project owner.
func registerProjectMembers(api huma.API, authSvc ProjectsAuthenticator, projectsSvc ProjectsService) {
	huma.Register(api, huma.Operation{
		OperationID: "get-project-members",
		Method:      http.MethodGet,
		Path:        "/api/projects/{id}/members",
		Summary:     "List project members",
		Description: "Lists the non-owner members of a project. Any user with access to the project may view its members.",
		Tags:        []string{"Projects"},
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusNotFound,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *listProjectMembersInput) (*projectMembersResponse, error) {
		currentUser, err := authenticateProjects(ctx, authSvc, input.Authorization)
		if err != nil {
			return nil, err
		}

		members, err := projectsSvc.ListMembers(ctx, currentUser.ID, input.ProjectID)
		if err != nil {
			return nil, mapProjectsError(ctx, err)
		}
		return projectMembersResponseFromDomain(members), nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "post-project-member",
		Method:        http.MethodPost,
		Path:          "/api/projects/{id}/members",
		Summary:       "Add a project member",
		Description:   "Grants a user, identified by email, viewer or editor access to a project. Owner only.",
		Tags:          []string{"Projects"},
		DefaultStatus: http.StatusCreated,
		Responses: apiErrorResponses(api,
			http.StatusBadRequest,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusNotFound,
			http.StatusConflict,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *addProjectMemberInput) (*projectMemberResponse, error) {
		currentUser, err := authenticateProjects(ctx, authSvc, input.Authorization)
		if err != nil {
			return nil, err
		}

		ctx = auditctx.WithImpersonator(ctx, currentUser.ImpersonatorID)

		member, err := projectsSvc.AddMember(ctx, projectservice.AddMemberInput{
			RequestingUserID: currentUser.ID,
			ProjectID:        input.ProjectID,
			Email:            input.Body.Email,
			Role:             input.Body.Role,
		})
		if err != nil {
			return nil, mapProjectsError(ctx, err)
		}
		return projectMemberResponseFromDomain(member), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "patch-project-member",
		Method:      http.MethodPatch,
		Path:        "/api/projects/{id}/members/{userId}",
		Summary:     "Update a project member role",
		Description: "Changes an existing member's access role. Owner only.",
		Tags:        []string{"Projects"},
		Responses: apiErrorResponses(api,
			http.StatusBadRequest,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusNotFound,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *updateProjectMemberInput) (*projectMemberResponse, error) {
		currentUser, err := authenticateProjects(ctx, authSvc, input.Authorization)
		if err != nil {
			return nil, err
		}

		ctx = auditctx.WithImpersonator(ctx, currentUser.ImpersonatorID)

		member, err := projectsSvc.UpdateMemberRole(ctx, projectservice.UpdateMemberRoleInput{
			RequestingUserID: currentUser.ID,
			ProjectID:        input.ProjectID,
			TargetUserID:     input.UserID,
			Role:             input.Body.Role,
		})
		if err != nil {
			return nil, mapProjectsError(ctx, err)
		}
		return projectMemberResponseFromDomain(member), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-project-member",
		Method:      http.MethodDelete,
		Path:        "/api/projects/{id}/members/{userId}",
		Summary:     "Remove a project member",
		Description: "Revokes a user's access to a project. Owner only.",
		Tags:        []string{"Projects"},
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusNotFound,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *deleteProjectMemberInput) (*projectMemberDeletedResponse, error) {
		currentUser, err := authenticateProjects(ctx, authSvc, input.Authorization)
		if err != nil {
			return nil, err
		}

		ctx = auditctx.WithImpersonator(ctx, currentUser.ImpersonatorID)

		if err := projectsSvc.RemoveMember(ctx, currentUser.ID, input.ProjectID, input.UserID); err != nil {
			return nil, mapProjectsError(ctx, err)
		}
		return &projectMemberDeletedResponse{Body: projectMemberDeletedBody{Success: true}}, nil
	})
}

func projectMembersResponseFromDomain(members []domain.ProjectMemberDetail) *projectMembersResponse {
	items := make([]projectMemberItem, 0, len(members))
	for _, member := range members {
		items = append(items, projectMemberItem{
			UserID:      member.UserID.String(),
			Email:       member.Email,
			DisplayName: member.DisplayName,
			Role:        string(member.Role),
			CreatedAt:   member.CreatedAt,
			UpdatedAt:   member.UpdatedAt,
		})
	}
	return &projectMembersResponse{Body: projectMembersBody{Members: items}}
}

func projectMemberResponseFromDomain(member domain.ProjectMember) *projectMemberResponse {
	return &projectMemberResponse{Body: projectMemberBody{Member: projectMemberGrant{
		UserID:    member.UserID.String(),
		Role:      string(member.Role),
		CreatedAt: member.CreatedAt,
		UpdatedAt: member.UpdatedAt,
	}}}
}
