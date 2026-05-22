package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"github.com/aklmans/wow-dashboard-api/internal/app"
	authservice "github.com/aklmans/wow-dashboard-api/internal/auth/service"
	projectsdomain "github.com/aklmans/wow-dashboard-api/internal/projects/domain"
	projectservice "github.com/aklmans/wow-dashboard-api/internal/projects/service"
	systemeventsdomain "github.com/aklmans/wow-dashboard-api/internal/systemevents/domain"
	systemeventsservice "github.com/aklmans/wow-dashboard-api/internal/systemevents/service"
	"github.com/aklmans/wow-dashboard-api/internal/users/domain"
	userservice "github.com/aklmans/wow-dashboard-api/internal/users/service"
)

func main() {
	router := chi.NewRouter()

	// Use the shared configuration logic to prevent drift
	api := app.NewAPI(router)

	app.RegisterRoutes(api, app.Dependencies{
		AuthService:         openAPIAuthService{},
		UsersService:        openAPIUsersService{},
		ProjectsService:     openAPIProjectsService{},
		SystemEventsService: openAPISystemEventsService{},
		ReadyChecker:        openAPIReadyChecker{},
	})

	spec := api.OpenAPI()
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal OpenAPI spec: %v\n", err)
		os.Exit(1)
	}

	dir := "openapi"
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create openapi directory: %v\n", err)
		os.Exit(1)
	}

	path := filepath.Join(dir, "openapi.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write openapi.json: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully exported OpenAPI spec to %s\n", path)
}

type openAPIAuthService struct{}

type openAPIUsersService struct{}

type openAPISystemEventsService struct{}

type openAPIReadyChecker struct{}

func (openAPIReadyChecker) Ready(context.Context) error {
	return nil
}

func (openAPIAuthService) SignUp(context.Context, authservice.SignUpInput) (*authservice.Session, error) {
	return nil, nil
}

func (openAPIAuthService) SignIn(context.Context, authservice.SignInInput) (*authservice.Session, error) {
	return nil, nil
}

func (openAPIAuthService) Refresh(context.Context, string) (*authservice.Session, error) {
	return nil, nil
}

func (openAPIAuthService) SignOut(context.Context, string) error {
	return nil
}

func (openAPIAuthService) CurrentUser(context.Context, string) (*authservice.PublicUser, error) {
	return nil, nil
}

func (openAPIUsersService) ListUsers(context.Context, userservice.ListUsersInput) (domain.ListUsersResult, error) {
	return domain.ListUsersResult{}, nil
}

func (openAPIUsersService) GetUser(context.Context, string) (domain.User, error) {
	return domain.User{}, nil
}

func (openAPIUsersService) UpdateUser(context.Context, userservice.UpdateUserInput) (domain.User, error) {
	return domain.User{}, nil
}

type openAPIProjectsService struct{}

func (openAPIProjectsService) ListProjects(context.Context, projectservice.ListProjectsInput) (projectsdomain.ListProjectsResult, error) {
	return projectsdomain.ListProjectsResult{}, nil
}

func (openAPIProjectsService) GetProject(context.Context, string, string) (projectsdomain.Project, error) {
	return projectsdomain.Project{}, nil
}

func (openAPIProjectsService) CreateProject(context.Context, projectservice.CreateProjectInput) (projectsdomain.Project, error) {
	return projectsdomain.Project{}, nil
}

func (openAPIProjectsService) UpdateProject(context.Context, projectservice.UpdateProjectInput) (projectsdomain.Project, error) {
	return projectsdomain.Project{}, nil
}

func (openAPIProjectsService) ArchiveProject(context.Context, string, string) (projectsdomain.Project, error) {
	return projectsdomain.Project{}, nil
}

func (openAPISystemEventsService) ListEvents(context.Context, systemeventsservice.ListEventsInput) (systemeventsdomain.ListEventsResult, error) {
	return systemeventsdomain.ListEventsResult{}, nil
}
