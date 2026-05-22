package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
	"github.com/aklmans/wow-dashboard-api/internal/projects/domain"
	projectservice "github.com/aklmans/wow-dashboard-api/internal/projects/service"
)

// ProjectsAuthenticator surfaces the auth check the projects handler needs.
type ProjectsAuthenticator = UsersAuthenticator

// ProjectsService is the project use-case surface required by handlers.
type ProjectsService interface {
	ListProjects(ctx context.Context, input projectservice.ListProjectsInput) (domain.ListProjectsResult, error)
	GetProject(ctx context.Context, ownerUserID string, id string) (domain.Project, error)
	CreateProject(ctx context.Context, input projectservice.CreateProjectInput) (domain.Project, error)
	UpdateProject(ctx context.Context, input projectservice.UpdateProjectInput) (domain.Project, error)
	ArchiveProject(ctx context.Context, ownerUserID string, id string) (domain.Project, error)
}

type listProjectsInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
	Page          int    `query:"page" default:"1" minimum:"1" example:"1" doc:"Page number, defaults to 1"`
	PageSize      int    `query:"pageSize" default:"20" minimum:"1" maximum:"100" example:"20" doc:"Projects per page, defaults to 20 and must not exceed 100"`
	Search        string `query:"search" example:"demo" doc:"Optional name or description search term"`
	Status        string `query:"status" example:"active" doc:"Optional status filter: active or archived"`
}

type projectsListResponse struct {
	Body projectsListBody
}

type projectsListBody struct {
	Projects []projectItem `json:"projects" nullable:"false" doc:"Projects for the requested page"`
	Page     int           `json:"page" example:"1" doc:"Current page number"`
	PageSize int           `json:"pageSize" example:"20" doc:"Projects per page"`
	Total    int           `json:"total" example:"1" doc:"Total projects matching the filters"`
}

type projectItem struct {
	ID          string    `json:"id" example:"c8a89c0b-8e75-4e61-9fa0-70fb83554e66" doc:"Project identifier"`
	Name        string    `json:"name" example:"Demo Project" doc:"Project name"`
	Description string    `json:"description" example:"" doc:"Project description"`
	Status      string    `json:"status" example:"active" doc:"Project status"`
	OwnerUserID string    `json:"ownerUserId" example:"c8a89c0b-8e75-4e61-9fa0-70fb83554e66" doc:"Owner user identifier"`
	CreatedAt   time.Time `json:"createdAt" doc:"Creation timestamp"`
	UpdatedAt   time.Time `json:"updatedAt" doc:"Last update timestamp"`
}

type getProjectInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
	ID            string `path:"id" format:"uuid" example:"c8a89c0b-8e75-4e61-9fa0-70fb83554e66" doc:"Project UUID"`
}

type projectDetailResponse struct {
	Body projectDetailBody
}

type projectDetailBody struct {
	Project projectItem `json:"project" doc:"Requested project"`
}

type createProjectInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
	Body          struct {
		Name        string `json:"name" maxLength:"120" example:"Demo Project" doc:"Project name"`
		Description string `json:"description,omitempty" maxLength:"2000" example:"" doc:"Optional project description"`
		Status      string `json:"status,omitempty" example:"active" doc:"Optional status; defaults to active"`
	}
}

type projectCreatedResponse struct {
	Body projectDetailBody
}

type updateProjectInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
	ID            string `path:"id" format:"uuid" example:"c8a89c0b-8e75-4e61-9fa0-70fb83554e66" doc:"Project UUID"`
	Body          struct {
		Name        *string `json:"name,omitempty" maxLength:"120" example:"New name" doc:"New project name; omit to leave unchanged"`
		Description *string `json:"description,omitempty" maxLength:"2000" example:"New description" doc:"New project description; pass an empty string to clear, omit to leave unchanged"`
		Status      *string `json:"status,omitempty" example:"active" doc:"New project status (active or archived); omit to leave unchanged"`
	}
}

// RegisterProjects wires the projects endpoints onto the Huma API.
func RegisterProjects(api huma.API, authSvc ProjectsAuthenticator, projectsSvc ProjectsService) {
	huma.Register(api, huma.Operation{
		OperationID: "get-projects",
		Method:      http.MethodGet,
		Path:        "/api/projects",
		Summary:     "List projects",
		Description: "Returns a paginated list of projects owned by the authenticated user.",
		Tags:        []string{"Projects"},
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *listProjectsInput) (*projectsListResponse, error) {
		currentUser, err := authenticateProjects(ctx, authSvc, input.Authorization)
		if err != nil {
			return nil, err
		}

		result, err := projectsSvc.ListProjects(ctx, projectservice.ListProjectsInput{
			OwnerUserID: currentUser.ID,
			Page:        input.Page,
			PageSize:    input.PageSize,
			Search:      input.Search,
			Status:      input.Status,
		})
		if err != nil {
			return nil, mapProjectsError(ctx, err)
		}
		return listProjectsResponseFromDomain(result), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-project",
		Method:      http.MethodGet,
		Path:        "/api/projects/{id}",
		Summary:     "Get project by id",
		Description: "Returns a single project owned by the authenticated user.",
		Tags:        []string{"Projects"},
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusNotFound,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *getProjectInput) (*projectDetailResponse, error) {
		currentUser, err := authenticateProjects(ctx, authSvc, input.Authorization)
		if err != nil {
			return nil, err
		}

		project, err := projectsSvc.GetProject(ctx, currentUser.ID, input.ID)
		if err != nil {
			return nil, mapProjectsError(ctx, err)
		}
		return &projectDetailResponse{Body: projectDetailBody{Project: projectItemFromDomain(project)}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "post-projects",
		Method:        http.MethodPost,
		Path:          "/api/projects",
		Summary:       "Create project",
		Description:   "Creates a new project owned by the authenticated user.",
		Tags:          []string{"Projects"},
		DefaultStatus: http.StatusCreated,
		Responses: apiErrorResponses(api,
			http.StatusBadRequest,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusConflict,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *createProjectInput) (*projectCreatedResponse, error) {
		currentUser, err := authenticateProjects(ctx, authSvc, input.Authorization)
		if err != nil {
			return nil, err
		}

		project, err := projectsSvc.CreateProject(ctx, projectservice.CreateProjectInput{
			OwnerUserID: currentUser.ID,
			Name:        input.Body.Name,
			Description: input.Body.Description,
			Status:      input.Body.Status,
		})
		if err != nil {
			return nil, mapProjectsError(ctx, err)
		}
		return &projectCreatedResponse{Body: projectDetailBody{Project: projectItemFromDomain(project)}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "patch-project",
		Method:      http.MethodPatch,
		Path:        "/api/projects/{id}",
		Summary:     "Update project",
		Description: "Partially updates a project owned by the authenticated user. Omitted fields stay unchanged; an empty description clears the field; at least one field must be provided.",
		Tags:        []string{"Projects"},
		Responses: apiErrorResponses(api,
			http.StatusBadRequest,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusNotFound,
			http.StatusConflict,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *updateProjectInput) (*projectDetailResponse, error) {
		currentUser, err := authenticateProjects(ctx, authSvc, input.Authorization)
		if err != nil {
			return nil, err
		}

		project, err := projectsSvc.UpdateProject(ctx, projectservice.UpdateProjectInput{
			OwnerUserID: currentUser.ID,
			ID:          input.ID,
			Name:        input.Body.Name,
			Description: input.Body.Description,
			Status:      input.Body.Status,
		})
		if err != nil {
			return nil, mapProjectsError(ctx, err)
		}
		return &projectDetailResponse{Body: projectDetailBody{Project: projectItemFromDomain(project)}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-project",
		Method:      http.MethodDelete,
		Path:        "/api/projects/{id}",
		Summary:     "Archive project",
		Description: "Archives a project owned by the authenticated user. Sets status to archived rather than physically deleting; idempotent for already-archived projects.",
		Tags:        []string{"Projects"},
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusNotFound,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *getProjectInput) (*projectDetailResponse, error) {
		currentUser, err := authenticateProjects(ctx, authSvc, input.Authorization)
		if err != nil {
			return nil, err
		}

		project, err := projectsSvc.ArchiveProject(ctx, currentUser.ID, input.ID)
		if err != nil {
			return nil, mapProjectsError(ctx, err)
		}
		return &projectDetailResponse{Body: projectDetailBody{Project: projectItemFromDomain(project)}}, nil
	})
}

func authenticateProjects(ctx context.Context, authSvc ProjectsAuthenticator, authHeader string) (currentUser projectsAuthUser, err error) {
	rawAccessToken, ok := parseBearerToken(authHeader)
	if !ok {
		return currentUser, apierror.Unauthorized("Authorization token missing or invalid.").ForContext(ctx)
	}

	user, err := authSvc.CurrentUser(ctx, rawAccessToken)
	if err != nil {
		return currentUser, mapAuthError(ctx, err)
	}
	if user == nil {
		return currentUser, apierror.Unauthorized("Authorization token missing or invalid.").ForContext(ctx)
	}
	currentUser.ID = user.ID
	return currentUser, nil
}

type projectsAuthUser struct {
	ID string
}

func listProjectsResponseFromDomain(result domain.ListProjectsResult) *projectsListResponse {
	items := make([]projectItem, 0, len(result.Projects))
	for _, project := range result.Projects {
		items = append(items, projectItemFromDomain(project))
	}
	return &projectsListResponse{Body: projectsListBody{
		Projects: items,
		Page:     result.Page,
		PageSize: result.PageSize,
		Total:    result.Total,
	}}
}

func projectItemFromDomain(project domain.Project) projectItem {
	return projectItem{
		ID:          project.ID.String(),
		Name:        project.Name,
		Description: project.Description,
		Status:      string(project.Status),
		OwnerUserID: project.OwnerUserID.String(),
		CreatedAt:   project.CreatedAt,
		UpdatedAt:   project.UpdatedAt,
	}
}

func mapProjectsError(ctx context.Context, err error) huma.StatusError {
	switch {
	case errors.Is(err, projectservice.ErrInvalidInput):
		return apierror.ValidationFailed("Invalid projects request.").ForContext(ctx)
	case errors.Is(err, projectservice.ErrNotFound):
		return apierror.NotFound("Project not found.").ForContext(ctx)
	case errors.Is(err, projectservice.ErrNameConflict):
		return apierror.Conflict("Project name already exists.").ForContext(ctx)
	default:
		return apierror.InternalError(err).ForContext(ctx)
	}
}
