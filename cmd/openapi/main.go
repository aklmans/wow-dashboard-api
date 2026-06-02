package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/app"
	authservice "github.com/aklmans/wow-dashboard-api/internal/auth/service"
	mfaservice "github.com/aklmans/wow-dashboard-api/internal/mfa/service"
	notificationsdomain "github.com/aklmans/wow-dashboard-api/internal/notifications/domain"
	notificationsservice "github.com/aklmans/wow-dashboard-api/internal/notifications/service"
	projectsdomain "github.com/aklmans/wow-dashboard-api/internal/projects/domain"
	projectservice "github.com/aklmans/wow-dashboard-api/internal/projects/service"
	rolesdomain "github.com/aklmans/wow-dashboard-api/internal/roles/domain"
	rolesservice "github.com/aklmans/wow-dashboard-api/internal/roles/service"
	systemeventsdomain "github.com/aklmans/wow-dashboard-api/internal/systemevents/domain"
	systemeventsservice "github.com/aklmans/wow-dashboard-api/internal/systemevents/service"
	"github.com/aklmans/wow-dashboard-api/internal/users/domain"
	userservice "github.com/aklmans/wow-dashboard-api/internal/users/service"
)

func main() {
	router := chi.NewRouter()

	// Use the shared configuration logic to prevent drift. Docs are enabled here
	// so the generated spec is independent of runtime config (DocsPath is a UI
	// route, not part of the OpenAPI document).
	api := app.NewAPI(router, true)

	app.RegisterRoutes(api, app.Dependencies{
		AuthService:          openAPIAuthService{},
		MfaService:           openAPIMfaService{},
		UsersService:         openAPIUsersService{},
		RolesService:         openAPIRolesService{},
		ProjectsService:      openAPIProjectsService{},
		SystemEventsService:  openAPISystemEventsService{},
		NotificationsService: openAPINotificationsService{},
		ReadyChecker:         openAPIReadyChecker{},
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

type openAPINotificationsService struct{}

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

func (openAPIAuthService) RefreshSession(context.Context, string, string) (*authservice.Session, error) {
	return nil, nil
}

func (openAPIAuthService) SignOut(context.Context, string) error {
	return nil
}

func (openAPIAuthService) SignOutOtherSessions(context.Context, string) error {
	return nil
}

func (openAPIAuthService) VerifyPassword(context.Context, uuid.UUID, string) error {
	return nil
}

func (openAPIAuthService) CompleteMfaSignIn(context.Context, string, string) (*authservice.Session, error) {
	return &authservice.Session{}, nil
}

type openAPIMfaService struct{}

func (openAPIMfaService) Setup(context.Context, uuid.UUID, string) (mfaservice.SetupResult, error) {
	return mfaservice.SetupResult{}, nil
}

func (openAPIMfaService) Confirm(context.Context, uuid.UUID, string) ([]string, error) {
	return nil, nil
}

func (openAPIMfaService) Disable(context.Context, uuid.UUID, string) error {
	return nil
}

func (openAPIAuthService) CurrentUser(context.Context, string) (*authservice.PublicUser, error) {
	return nil, nil
}

func (openAPIAuthService) Impersonate(context.Context, *authservice.PublicUser, string) (*authservice.Session, error) {
	return nil, nil
}

func (openAPIAuthService) StopImpersonation(context.Context, string, string) (*authservice.Session, error) {
	return nil, nil
}

func (openAPIAuthService) UpdateMyProfile(context.Context, string, authservice.UpdateMyProfileInput) (*authservice.PublicUser, error) {
	return nil, nil
}

func (openAPIAuthService) ChangePassword(context.Context, string, string, string) error {
	return nil
}

func (openAPIAuthService) ForgotPassword(context.Context, string) error {
	return nil
}

func (openAPIAuthService) ResetPassword(context.Context, string, string) error {
	return nil
}

func (openAPIAuthService) VerifyEmail(context.Context, string) error {
	return nil
}

func (openAPIAuthService) ResendVerification(context.Context, string) error {
	return nil
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

type openAPIRolesService struct{}

func (openAPIRolesService) ListRoles(context.Context) ([]rolesdomain.Role, error) {
	return nil, nil
}

func (openAPIRolesService) GetRole(context.Context, string) (rolesdomain.Role, error) {
	return rolesdomain.Role{}, nil
}

func (openAPIRolesService) CreateRole(context.Context, rolesservice.CreateRoleInput) (rolesdomain.Role, error) {
	return rolesdomain.Role{}, nil
}

func (openAPIRolesService) UpdateRole(context.Context, rolesservice.UpdateRoleInput) (rolesdomain.Role, error) {
	return rolesdomain.Role{}, nil
}

func (openAPIRolesService) DeleteRole(context.Context, string, string) error {
	return nil
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

func (openAPIProjectsService) ListMembers(context.Context, string, string) ([]projectsdomain.ProjectMemberDetail, error) {
	return nil, nil
}

func (openAPIProjectsService) AddMember(context.Context, projectservice.AddMemberInput) (projectsdomain.ProjectMember, error) {
	return projectsdomain.ProjectMember{}, nil
}

func (openAPIProjectsService) UpdateMemberRole(context.Context, projectservice.UpdateMemberRoleInput) (projectsdomain.ProjectMember, error) {
	return projectsdomain.ProjectMember{}, nil
}

func (openAPIProjectsService) RemoveMember(context.Context, string, string, string) error {
	return nil
}

func (openAPISystemEventsService) ListEvents(context.Context, systemeventsservice.ListEventsInput) (systemeventsdomain.ListEventsResult, error) {
	return systemeventsdomain.ListEventsResult{}, nil
}

func (openAPINotificationsService) List(context.Context, notificationsservice.ListInput) (notificationsdomain.ListResult, error) {
	return notificationsdomain.ListResult{}, nil
}

func (openAPINotificationsService) MarkRead(context.Context, uuid.UUID, uuid.UUID) (int64, error) {
	return 0, nil
}

func (openAPINotificationsService) MarkAllRead(context.Context, uuid.UUID) (int64, error) {
	return 0, nil
}
