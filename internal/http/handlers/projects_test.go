package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	authservice "github.com/aklmans/wow-dashboard-api/internal/auth/service"
	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
	"github.com/aklmans/wow-dashboard-api/internal/http/handlers"
	"github.com/aklmans/wow-dashboard-api/internal/projects/domain"
	projectservice "github.com/aklmans/wow-dashboard-api/internal/projects/service"
)

func TestProjectsNonAdminAuthenticatedUserCanListProjects(t *testing.T) {
	owner := uuid.New()
	projectsSvc := &fakeProjectsService{listResult: domain.ListProjectsResult{Page: 1, PageSize: 20, Total: 0}}
	router := newProjectsTestRouter(&fakeUsersAuthService{
		currentUser: &authservice.PublicUser{ID: owner.String(), Status: "active", Permissions: []string{"projects:create"}},
	}, projectsSvc)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	req.Header.Set("Authorization", "Bearer access-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s — projects must remain accessible to non-admin authenticated users", rec.Code, rec.Body.String())
	}
	if projectsSvc.listInput.UserID != owner.String() {
		t.Fatalf("service owner = %q, want %q", projectsSvc.listInput.UserID, owner.String())
	}
}

func TestProjectsListHandler(t *testing.T) {
	t.Run("missing authorization returns unauthorized envelope", func(t *testing.T) {
		router := newProjectsTestRouter(&fakeUsersAuthService{}, &fakeProjectsService{})

		req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
			"Authorization token missing or invalid.")
	})

	t.Run("invalid token returns unauthorized envelope", func(t *testing.T) {
		authSvc := &fakeUsersAuthService{currentUserErr: authservice.ErrInvalidToken}
		router := newProjectsTestRouter(authSvc, &fakeProjectsService{})

		req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
		req.Header.Set("Authorization", "Bearer bad-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
			"Authorization token missing or invalid.")
	})

	t.Run("success returns paginated projects response", func(t *testing.T) {
		owner := uuid.New()
		projectID := uuid.New()
		createdAt := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
		projectsSvc := &fakeProjectsService{
			listResult: domain.ListProjectsResult{
				Projects: []domain.Project{{
					ID:          projectID,
					Name:        "Demo",
					Description: "hello",
					Status:      domain.ProjectStatusActive,
					OwnerUserID: owner,
					CreatedAt:   createdAt,
					UpdatedAt:   createdAt.Add(time.Minute),
				}},
				Page:     2,
				PageSize: 5,
				Total:    12,
			},
		}
		router := newProjectsTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: owner.String(), Status: "active", Permissions: []string{"projects:create"}},
		}, projectsSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/projects?page=2&pageSize=5&search=demo&status=active", nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if projectsSvc.listInput.UserID != owner.String() {
			t.Fatalf("service owner = %q, want %q", projectsSvc.listInput.UserID, owner.String())
		}
		if projectsSvc.listInput.Page != 2 || projectsSvc.listInput.PageSize != 5 {
			t.Fatalf("pagination forwarded = %d/%d, want 2/5", projectsSvc.listInput.Page, projectsSvc.listInput.PageSize)
		}
		if projectsSvc.listInput.Search != "demo" || projectsSvc.listInput.Status != "active" {
			t.Fatalf("filters forwarded = %#v", projectsSvc.listInput)
		}

		var body projectsListResponseBody
		decodeJSON(t, rec, &body)
		if body.Page != 2 || body.PageSize != 5 || body.Total != 12 {
			t.Fatalf("metadata = %#v", body)
		}
		if len(body.Projects) != 1 || body.Projects[0].ID != projectID.String() {
			t.Fatalf("projects = %#v", body.Projects)
		}
		if body.Projects[0].OwnerUserID != owner.String() {
			t.Fatalf("ownerUserId = %q, want %q", body.Projects[0].OwnerUserID, owner.String())
		}
	})

	t.Run("service rejection returns validation envelope", func(t *testing.T) {
		router := newProjectsTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: uuid.New().String(), Status: "active", Permissions: []string{"projects:create"}},
		}, &fakeProjectsService{listErr: projectservice.ErrInvalidInput})

		req := httptest.NewRequest(http.MethodGet, "/api/projects?pageSize=50", nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnprocessableEntity, apierror.CodeValidationFailed,
			"Invalid projects request.")
	})

	t.Run("page size over maximum is rejected at the schema edge", func(t *testing.T) {
		router := newProjectsTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: uuid.New().String(), Status: "active", Permissions: []string{"projects:create"}},
		}, &fakeProjectsService{})

		req := httptest.NewRequest(http.MethodGet, "/api/projects?pageSize=101", nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnprocessableEntity, apierror.CodeValidationFailed,
			"Invalid request.")
	})
}

func TestProjectsDetailHandler(t *testing.T) {
	owner := uuid.New()

	t.Run("missing authorization returns unauthorized envelope", func(t *testing.T) {
		router := newProjectsTestRouter(&fakeUsersAuthService{}, &fakeProjectsService{})

		req := httptest.NewRequest(http.MethodGet, "/api/projects/"+uuid.New().String(), nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
			"Authorization token missing or invalid.")
	})

	t.Run("invalid uuid returns validation envelope", func(t *testing.T) {
		projectsSvc := &fakeProjectsService{}
		router := newProjectsTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: owner.String(), Status: "active", Permissions: []string{"projects:create"}},
		}, projectsSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/projects/not-a-uuid", nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnprocessableEntity, apierror.CodeValidationFailed,
			"Invalid request.")
		if projectsSvc.getID != "" {
			t.Fatalf("service called with %q for invalid uuid", projectsSvc.getID)
		}
	})

	t.Run("missing project returns 404", func(t *testing.T) {
		projectsSvc := &fakeProjectsService{getErr: projectservice.ErrNotFound}
		router := newProjectsTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: owner.String(), Status: "active", Permissions: []string{"projects:create"}},
		}, projectsSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/projects/"+uuid.New().String(), nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusNotFound, apierror.CodeNotFound, "Project not found.")
	})

	t.Run("success returns project body", func(t *testing.T) {
		projectID := uuid.New()
		createdAt := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
		projectsSvc := &fakeProjectsService{getResult: domain.Project{
			ID:          projectID,
			Name:        "Demo",
			Description: "hello",
			Status:      domain.ProjectStatusActive,
			OwnerUserID: owner,
			CreatedAt:   createdAt,
			UpdatedAt:   createdAt.Add(time.Minute),
		}}
		router := newProjectsTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: owner.String(), Status: "active", Permissions: []string{"projects:create"}},
		}, projectsSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID.String(), nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if projectsSvc.getOwnerID != owner.String() || projectsSvc.getID != projectID.String() {
			t.Fatalf("service got owner=%q id=%q", projectsSvc.getOwnerID, projectsSvc.getID)
		}

		var body projectDetailResponseBody
		decodeJSON(t, rec, &body)
		if body.Project.ID != projectID.String() || body.Project.Name != "Demo" {
			t.Fatalf("project = %#v", body.Project)
		}
	})
}

func TestProjectsCreateHandler(t *testing.T) {
	owner := uuid.New()

	t.Run("missing authorization returns unauthorized envelope", func(t *testing.T) {
		router := newProjectsTestRouter(&fakeUsersAuthService{}, &fakeProjectsService{})

		rec := postJSON(router, "/api/projects", map[string]string{"name": "demo"})

		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
			"Authorization token missing or invalid.")
	})

	t.Run("invalid body returns validation envelope", func(t *testing.T) {
		projectsSvc := &fakeProjectsService{createErr: projectservice.ErrInvalidInput}
		router := newProjectsTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: owner.String(), Status: "active", Permissions: []string{"projects:create"}},
		}, projectsSvc)

		req := httptest.NewRequest(http.MethodPost, "/api/projects",
			strings.NewReader(`{"name":"   "}`))
		req.Header.Set("Authorization", "Bearer access-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnprocessableEntity, apierror.CodeValidationFailed,
			"Invalid projects request.")
	})

	t.Run("name conflict returns conflict envelope", func(t *testing.T) {
		projectsSvc := &fakeProjectsService{createErr: projectservice.ErrNameConflict}
		router := newProjectsTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: owner.String(), Status: "active", Permissions: []string{"projects:create"}},
		}, projectsSvc)

		req := httptest.NewRequest(http.MethodPost, "/api/projects",
			strings.NewReader(`{"name":"Demo"}`))
		req.Header.Set("Authorization", "Bearer access-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusConflict, apierror.CodeConflict,
			"Project name already exists.")
	})

	t.Run("success returns 201 with project", func(t *testing.T) {
		created := domain.Project{
			ID:          uuid.New(),
			Name:        "Demo",
			Description: "hello",
			Status:      domain.ProjectStatusActive,
			OwnerUserID: owner,
			CreatedAt:   time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
			UpdatedAt:   time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
		}
		projectsSvc := &fakeProjectsService{createResult: created}
		router := newProjectsTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: owner.String(), Status: "active", Permissions: []string{"projects:create"}},
		}, projectsSvc)

		req := httptest.NewRequest(http.MethodPost, "/api/projects",
			strings.NewReader(`{"name":"Demo","description":"hello"}`))
		req.Header.Set("Authorization", "Bearer access-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
		}
		if projectsSvc.createInput.OwnerUserID != owner.String() {
			t.Fatalf("service owner = %q", projectsSvc.createInput.OwnerUserID)
		}
		if projectsSvc.createInput.Name != "Demo" || projectsSvc.createInput.Description != "hello" {
			t.Fatalf("service input = %#v", projectsSvc.createInput)
		}

		var body projectDetailResponseBody
		decodeJSON(t, rec, &body)
		if body.Project.ID != created.ID.String() {
			t.Fatalf("project id = %q", body.Project.ID)
		}
	})

	t.Run("without projects:create permission returns 403", func(t *testing.T) {
		projectsSvc := &fakeProjectsService{}
		router := newProjectsTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: owner.String(), Status: "active", Permissions: []string{}},
		}, projectsSvc)

		req := httptest.NewRequest(http.MethodPost, "/api/projects",
			strings.NewReader(`{"name":"Demo"}`))
		req.Header.Set("Authorization", "Bearer access-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusForbidden, apierror.CodeForbidden, noPermissionMessage)
		if projectsSvc.createInput.Name != "" {
			t.Fatal("service was called for a user without projects:create")
		}
	})
}

func TestProjectsUpdateHandler(t *testing.T) {
	owner := uuid.New()

	t.Run("missing authorization returns unauthorized envelope", func(t *testing.T) {
		router := newProjectsTestRouter(&fakeUsersAuthService{}, &fakeProjectsService{})

		req := httptest.NewRequest(http.MethodPatch, "/api/projects/"+uuid.New().String(),
			strings.NewReader(`{"name":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
			"Authorization token missing or invalid.")
	})

	t.Run("invalid uuid returns validation envelope", func(t *testing.T) {
		projectsSvc := &fakeProjectsService{}
		router := newProjectsTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: owner.String(), Status: "active", Permissions: []string{"projects:create"}},
		}, projectsSvc)

		req := httptest.NewRequest(http.MethodPatch, "/api/projects/not-a-uuid",
			strings.NewReader(`{"name":"x"}`))
		req.Header.Set("Authorization", "Bearer access-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnprocessableEntity, apierror.CodeValidationFailed,
			"Invalid request.")
		if projectsSvc.updateCalled {
			t.Fatal("service called for invalid uuid")
		}
	})

	t.Run("missing project returns 404", func(t *testing.T) {
		projectsSvc := &fakeProjectsService{updateErr: projectservice.ErrNotFound}
		router := newProjectsTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: owner.String(), Status: "active", Permissions: []string{"projects:create"}},
		}, projectsSvc)

		req := httptest.NewRequest(http.MethodPatch, "/api/projects/"+uuid.New().String(),
			strings.NewReader(`{"name":"x"}`))
		req.Header.Set("Authorization", "Bearer access-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusNotFound, apierror.CodeNotFound, "Project not found.")
	})

	t.Run("empty body returns validation envelope", func(t *testing.T) {
		projectsSvc := &fakeProjectsService{updateErr: projectservice.ErrInvalidInput}
		router := newProjectsTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: owner.String(), Status: "active", Permissions: []string{"projects:create"}},
		}, projectsSvc)

		req := httptest.NewRequest(http.MethodPatch, "/api/projects/"+uuid.New().String(),
			strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer access-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnprocessableEntity, apierror.CodeValidationFailed,
			"Invalid projects request.")
	})

	t.Run("invalid name returns validation envelope", func(t *testing.T) {
		projectsSvc := &fakeProjectsService{updateErr: projectservice.ErrInvalidInput}
		router := newProjectsTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: owner.String(), Status: "active", Permissions: []string{"projects:create"}},
		}, projectsSvc)

		req := httptest.NewRequest(http.MethodPatch, "/api/projects/"+uuid.New().String(),
			strings.NewReader(`{"name":"   "}`))
		req.Header.Set("Authorization", "Bearer access-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnprocessableEntity, apierror.CodeValidationFailed,
			"Invalid projects request.")
	})

	t.Run("name conflict returns conflict envelope", func(t *testing.T) {
		projectsSvc := &fakeProjectsService{updateErr: projectservice.ErrNameConflict}
		router := newProjectsTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: owner.String(), Status: "active", Permissions: []string{"projects:create"}},
		}, projectsSvc)

		req := httptest.NewRequest(http.MethodPatch, "/api/projects/"+uuid.New().String(),
			strings.NewReader(`{"name":"Demo"}`))
		req.Header.Set("Authorization", "Bearer access-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusConflict, apierror.CodeConflict,
			"Project name already exists.")
	})

	t.Run("success returns 200 with updated project", func(t *testing.T) {
		projectID := uuid.New()
		updated := domain.Project{
			ID:          projectID,
			Name:        "New Name",
			Description: "",
			Status:      domain.ProjectStatusArchived,
			OwnerUserID: owner,
			CreatedAt:   time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
			UpdatedAt:   time.Date(2026, 5, 21, 12, 5, 0, 0, time.UTC),
		}
		projectsSvc := &fakeProjectsService{updateResult: updated}
		router := newProjectsTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: owner.String(), Status: "active", Permissions: []string{"projects:create"}},
		}, projectsSvc)

		req := httptest.NewRequest(http.MethodPatch, "/api/projects/"+projectID.String(),
			strings.NewReader(`{"name":"New Name","description":"","status":"archived"}`))
		req.Header.Set("Authorization", "Bearer access-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if !projectsSvc.updateCalled {
			t.Fatal("service was not called")
		}
		if projectsSvc.updateInput.UserID != owner.String() ||
			projectsSvc.updateInput.ID != projectID.String() {
			t.Fatalf("service ids = user=%q id=%q", projectsSvc.updateInput.UserID, projectsSvc.updateInput.ID)
		}
		if projectsSvc.updateInput.Name == nil || *projectsSvc.updateInput.Name != "New Name" {
			t.Fatalf("service name = %v", projectsSvc.updateInput.Name)
		}
		if projectsSvc.updateInput.Description == nil || *projectsSvc.updateInput.Description != "" {
			t.Fatalf("service description = %v, want empty string pointer", projectsSvc.updateInput.Description)
		}
		if projectsSvc.updateInput.Status == nil || *projectsSvc.updateInput.Status != "archived" {
			t.Fatalf("service status = %v", projectsSvc.updateInput.Status)
		}

		var body projectDetailResponseBody
		decodeJSON(t, rec, &body)
		if body.Project.ID != projectID.String() || body.Project.Name != "New Name" {
			t.Fatalf("project = %#v", body.Project)
		}
	})
}

func TestProjectsArchiveHandler(t *testing.T) {
	owner := uuid.New()

	t.Run("missing authorization returns unauthorized envelope", func(t *testing.T) {
		router := newProjectsTestRouter(&fakeUsersAuthService{}, &fakeProjectsService{})

		req := httptest.NewRequest(http.MethodDelete, "/api/projects/"+uuid.New().String(), nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
			"Authorization token missing or invalid.")
	})

	t.Run("invalid uuid returns validation envelope", func(t *testing.T) {
		projectsSvc := &fakeProjectsService{}
		router := newProjectsTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: owner.String(), Status: "active", Permissions: []string{"projects:create"}},
		}, projectsSvc)

		req := httptest.NewRequest(http.MethodDelete, "/api/projects/not-a-uuid", nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnprocessableEntity, apierror.CodeValidationFailed,
			"Invalid request.")
		if projectsSvc.archiveCalled {
			t.Fatal("service called for invalid uuid")
		}
	})

	t.Run("missing project returns 404", func(t *testing.T) {
		projectsSvc := &fakeProjectsService{archiveErr: projectservice.ErrNotFound}
		router := newProjectsTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: owner.String(), Status: "active", Permissions: []string{"projects:create"}},
		}, projectsSvc)

		req := httptest.NewRequest(http.MethodDelete, "/api/projects/"+uuid.New().String(), nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusNotFound, apierror.CodeNotFound, "Project not found.")
	})

	t.Run("success returns 200 with archived project", func(t *testing.T) {
		projectID := uuid.New()
		archived := domain.Project{
			ID:          projectID,
			Name:        "Demo",
			Description: "hello",
			Status:      domain.ProjectStatusArchived,
			OwnerUserID: owner,
			CreatedAt:   time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
			UpdatedAt:   time.Date(2026, 5, 21, 12, 5, 0, 0, time.UTC),
		}
		projectsSvc := &fakeProjectsService{archiveResult: archived}
		router := newProjectsTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: owner.String(), Status: "active", Permissions: []string{"projects:create"}},
		}, projectsSvc)

		req := httptest.NewRequest(http.MethodDelete, "/api/projects/"+projectID.String(), nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if !projectsSvc.archiveCalled {
			t.Fatal("service was not called")
		}
		if projectsSvc.archiveOwnerID != owner.String() || projectsSvc.archiveID != projectID.String() {
			t.Fatalf("service ids = owner=%q id=%q", projectsSvc.archiveOwnerID, projectsSvc.archiveID)
		}

		var body projectDetailResponseBody
		decodeJSON(t, rec, &body)
		if body.Project.ID != projectID.String() {
			t.Fatalf("project id = %q", body.Project.ID)
		}
		if body.Project.Status != string(domain.ProjectStatusArchived) {
			t.Fatalf("project status = %q, want archived", body.Project.Status)
		}
	})
}

func TestProjectsOpenAPIErrorResponses(t *testing.T) {
	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("Test API", "1.0.0"))
	handlers.RegisterProjects(api, &fakeUsersAuthService{}, &fakeProjectsService{})

	specJSON, err := json.Marshal(api.OpenAPI())
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	var spec map[string]any
	if err := json.Unmarshal(specJSON, &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}

	for _, endpoint := range []struct {
		path     string
		method   string
		statuses []string
	}{
		{path: "/api/projects", method: "get", statuses: []string{"401", "403", "422", "500"}},
		{path: "/api/projects/{id}", method: "get", statuses: []string{"401", "403", "404", "422", "500"}},
		{path: "/api/projects", method: "post", statuses: []string{"400", "401", "403", "409", "422", "500"}},
		{path: "/api/projects/{id}", method: "patch", statuses: []string{"400", "401", "403", "404", "409", "422", "500"}},
		{path: "/api/projects/{id}", method: "delete", statuses: []string{"401", "403", "404", "422", "500"}},
	} {
		t.Run(endpoint.method+" "+endpoint.path, func(t *testing.T) {
			operation := objectAt(t, spec, "paths", endpoint.path, endpoint.method)
			responses := objectAt(t, operation, "responses")
			for _, status := range endpoint.statuses {
				response := dereferenceResponse(t, spec, objectAt(t, responses, status))
				content := objectAt(t, response, "content")
				if _, ok := content["application/problem+json"]; ok {
					t.Fatalf("status %s exposes application/problem+json", status)
				}
				mediaType := objectAt(t, content, "application/json")
				schema := dereferenceSchema(t, spec, objectAt(t, mediaType, "schema"))
				properties := objectAt(t, schema, "properties")
				for _, field := range []string{"code", "message", "request_id"} {
					if _, ok := properties[field]; !ok {
						t.Fatalf("status %s schema missing %q property", status, field)
					}
				}
			}
		})
	}
}

func TestProjectsListOpenAPIProjectsArrayIsNonNullable(t *testing.T) {
	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("Test API", "1.0.0"))
	handlers.RegisterProjects(api, &fakeUsersAuthService{}, &fakeProjectsService{})

	specJSON, err := json.Marshal(api.OpenAPI())
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	var spec map[string]any
	if err := json.Unmarshal(specJSON, &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}

	projectsSchema := objectAt(t, spec, "components", "schemas", "ProjectsListBody", "properties", "projects")
	switch schemaType := projectsSchema["type"].(type) {
	case string:
		if schemaType != "array" {
			t.Fatalf("projects schema type = %q, want array", schemaType)
		}
	case []any:
		for _, value := range schemaType {
			if value == "null" {
				t.Fatalf("projects schema type = %#v, want non-nullable", schemaType)
			}
		}
	default:
		t.Fatalf("projects schema type is %T", schemaType)
	}
}

func TestProjectsDetailOpenAPIPathIDFormatUUID(t *testing.T) {
	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("Test API", "1.0.0"))
	handlers.RegisterProjects(api, &fakeUsersAuthService{}, &fakeProjectsService{})

	specJSON, err := json.Marshal(api.OpenAPI())
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	var spec map[string]any
	if err := json.Unmarshal(specJSON, &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}

	operation := objectAt(t, spec, "paths", "/api/projects/{id}", "get")
	parameters, _ := operation["parameters"].([]any)
	var idSchema map[string]any
	for _, p := range parameters {
		param, _ := p.(map[string]any)
		if param["name"] == "id" && param["in"] == "path" {
			idSchema, _ = param["schema"].(map[string]any)
			break
		}
	}
	if idSchema == nil {
		t.Fatal("path parameter id missing on /api/projects/{id}")
	}
	if format, _ := idSchema["format"].(string); format != "uuid" {
		t.Fatalf("path parameter id schema format = %q, want uuid", format)
	}

	patchOp := objectAt(t, spec, "paths", "/api/projects/{id}", "patch")
	patchParameters, _ := patchOp["parameters"].([]any)
	var patchIDSchema map[string]any
	for _, p := range patchParameters {
		param, _ := p.(map[string]any)
		if param["name"] == "id" && param["in"] == "path" {
			patchIDSchema, _ = param["schema"].(map[string]any)
			break
		}
	}
	if patchIDSchema == nil {
		t.Fatal("path parameter id missing on PATCH /api/projects/{id}")
	}
	if format, _ := patchIDSchema["format"].(string); format != "uuid" {
		t.Fatalf("PATCH path parameter id schema format = %q, want uuid", format)
	}

	deleteOp := objectAt(t, spec, "paths", "/api/projects/{id}", "delete")
	deleteParameters, _ := deleteOp["parameters"].([]any)
	var deleteIDSchema map[string]any
	for _, p := range deleteParameters {
		param, _ := p.(map[string]any)
		if param["name"] == "id" && param["in"] == "path" {
			deleteIDSchema, _ = param["schema"].(map[string]any)
			break
		}
	}
	if deleteIDSchema == nil {
		t.Fatal("path parameter id missing on DELETE /api/projects/{id}")
	}
	if format, _ := deleteIDSchema["format"].(string); format != "uuid" {
		t.Fatalf("DELETE path parameter id schema format = %q, want uuid", format)
	}
}

type fakeProjectsService struct {
	listInput  projectservice.ListProjectsInput
	listResult domain.ListProjectsResult
	listErr    error

	getOwnerID string
	getID      string
	getResult  domain.Project
	getErr     error

	createInput  projectservice.CreateProjectInput
	createResult domain.Project
	createErr    error

	updateCalled bool
	updateInput  projectservice.UpdateProjectInput
	updateResult domain.Project
	updateErr    error

	archiveCalled  bool
	archiveOwnerID string
	archiveID      string
	archiveResult  domain.Project
	archiveErr     error

	listMembersUserID    string
	listMembersProjectID string
	listMembersResult    []domain.ProjectMemberDetail
	listMembersErr       error

	addMemberInput  projectservice.AddMemberInput
	addMemberResult domain.ProjectMember
	addMemberErr    error

	updateMemberInput  projectservice.UpdateMemberRoleInput
	updateMemberResult domain.ProjectMember
	updateMemberErr    error

	removeMemberCalled    bool
	removeMemberProjectID string
	removeMemberTargetID  string
	removeMemberErr       error
}

func (f *fakeProjectsService) ListProjects(ctx context.Context, input projectservice.ListProjectsInput) (domain.ListProjectsResult, error) {
	f.listInput = input
	if f.listErr != nil {
		return domain.ListProjectsResult{}, f.listErr
	}
	return f.listResult, nil
}

func (f *fakeProjectsService) GetProject(ctx context.Context, ownerUserID string, id string) (domain.Project, error) {
	f.getOwnerID = ownerUserID
	f.getID = id
	if f.getErr != nil {
		return domain.Project{}, f.getErr
	}
	return f.getResult, nil
}

func (f *fakeProjectsService) CreateProject(ctx context.Context, input projectservice.CreateProjectInput) (domain.Project, error) {
	f.createInput = input
	if f.createErr != nil {
		return domain.Project{}, f.createErr
	}
	return f.createResult, nil
}

func (f *fakeProjectsService) UpdateProject(ctx context.Context, input projectservice.UpdateProjectInput) (domain.Project, error) {
	f.updateCalled = true
	f.updateInput = input
	if f.updateErr != nil {
		return domain.Project{}, f.updateErr
	}
	return f.updateResult, nil
}

func (f *fakeProjectsService) ArchiveProject(ctx context.Context, ownerUserID string, id string) (domain.Project, error) {
	f.archiveCalled = true
	f.archiveOwnerID = ownerUserID
	f.archiveID = id
	if f.archiveErr != nil {
		return domain.Project{}, f.archiveErr
	}
	return f.archiveResult, nil
}

func (f *fakeProjectsService) ListMembers(ctx context.Context, userID string, projectID string) ([]domain.ProjectMemberDetail, error) {
	f.listMembersUserID = userID
	f.listMembersProjectID = projectID
	if f.listMembersErr != nil {
		return nil, f.listMembersErr
	}
	return f.listMembersResult, nil
}

func (f *fakeProjectsService) AddMember(ctx context.Context, input projectservice.AddMemberInput) (domain.ProjectMember, error) {
	f.addMemberInput = input
	if f.addMemberErr != nil {
		return domain.ProjectMember{}, f.addMemberErr
	}
	return f.addMemberResult, nil
}

func (f *fakeProjectsService) UpdateMemberRole(ctx context.Context, input projectservice.UpdateMemberRoleInput) (domain.ProjectMember, error) {
	f.updateMemberInput = input
	if f.updateMemberErr != nil {
		return domain.ProjectMember{}, f.updateMemberErr
	}
	return f.updateMemberResult, nil
}

func (f *fakeProjectsService) RemoveMember(ctx context.Context, requestingUserID string, projectID string, targetUserID string) error {
	f.removeMemberCalled = true
	f.removeMemberProjectID = projectID
	f.removeMemberTargetID = targetUserID
	return f.removeMemberErr
}

func newProjectsTestRouter(authSvc handlers.ProjectsAuthenticator, projectsSvc handlers.ProjectsService) chi.Router {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	cfg := huma.DefaultConfig("Test API", "1.0.0")
	cfg.Transformers = append(cfg.Transformers, apierror.HumaErrorTransformer)
	api := humachi.New(router, cfg)
	handlers.RegisterProjects(api, authSvc, projectsSvc)
	return router
}

type projectsListResponseBody struct {
	Projects []projectResponseItem `json:"projects"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"pageSize"`
	Total    int                   `json:"total"`
}

type projectDetailResponseBody struct {
	Project projectResponseItem `json:"project"`
}

type projectResponseItem struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	OwnerUserID string    `json:"ownerUserId"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}
