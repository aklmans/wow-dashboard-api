package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
	"github.com/aklmans/wow-dashboard-api/internal/http/handlers"
	rolesdomain "github.com/aklmans/wow-dashboard-api/internal/roles/domain"
	rolesservice "github.com/aklmans/wow-dashboard-api/internal/roles/service"
)

func TestRolesListHandler(t *testing.T) {
	t.Run("success returns roles", func(t *testing.T) {
		rolesSvc := &fakeRolesService{listResult: []rolesdomain.Role{
			{ID: uuid.New(), Name: "admin", IsSystem: true, Permissions: []string{"*"}, UserCount: 1},
			{ID: uuid.New(), Name: "auditor", Permissions: []string{"system_events:read"}},
		}}
		rec := doRolesRequest(t, &fakeUsersAuthService{currentUser: adminPublicUser()}, rolesSvc,
			http.MethodGet, "/api/roles", nil)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var body struct {
			Roles []struct {
				Name        string   `json:"name"`
				IsSystem    bool     `json:"isSystem"`
				Permissions []string `json:"permissions"`
			} `json:"roles"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if len(body.Roles) != 2 || body.Roles[0].Name != "admin" {
			t.Fatalf("roles = %#v, want admin then auditor", body.Roles)
		}
	})

	t.Run("missing roles:read returns 403", func(t *testing.T) {
		rolesSvc := &fakeRolesService{}
		rec := doRolesRequest(t, &fakeUsersAuthService{currentUser: plainPublicUser()}, rolesSvc,
			http.MethodGet, "/api/roles", nil)
		assertAPIError(t, rec, http.StatusForbidden, apierror.CodeForbidden, noPermissionMessage)
		if rolesSvc.listCalled {
			t.Fatal("service was called for a user without roles:read")
		}
	})

	t.Run("missing authorization returns 401", func(t *testing.T) {
		router := newRolesRouter(&fakeUsersAuthService{}, &fakeRolesService{})
		req := httptest.NewRequest(http.MethodGet, "/api/roles", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
			"Authorization token missing or invalid.")
	})
}

func TestRolesGetHandler(t *testing.T) {
	t.Run("missing role returns 404", func(t *testing.T) {
		rolesSvc := &fakeRolesService{getErr: rolesservice.ErrNotFound}
		rec := doRolesRequest(t, &fakeUsersAuthService{currentUser: adminPublicUser()}, rolesSvc,
			http.MethodGet, "/api/roles/"+uuid.New().String(), nil)
		assertAPIError(t, rec, http.StatusNotFound, apierror.CodeNotFound, "Role not found.")
	})
}

func TestRolesCreateHandler(t *testing.T) {
	t.Run("success forwards input and returns 201", func(t *testing.T) {
		admin := adminPublicUser()
		rolesSvc := &fakeRolesService{createResult: rolesdomain.Role{
			ID: uuid.New(), Name: "auditor", Permissions: []string{"system_events:read"},
		}}
		rec := doRolesRequest(t, &fakeUsersAuthService{currentUser: admin}, rolesSvc,
			http.MethodPost, "/api/roles",
			map[string]any{"name": "auditor", "permissions": []string{"system_events:read"}})

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
		}
		if rolesSvc.createInput.Name != "auditor" || rolesSvc.createInput.ActorUserID != admin.ID {
			t.Fatalf("create input = %#v", rolesSvc.createInput)
		}
		if len(rolesSvc.createInput.Permissions) != 1 {
			t.Fatalf("create permissions = %v, want one entry", rolesSvc.createInput.Permissions)
		}
	})

	t.Run("name conflict returns 409", func(t *testing.T) {
		rolesSvc := &fakeRolesService{createErr: rolesservice.ErrNameConflict}
		rec := doRolesRequest(t, &fakeUsersAuthService{currentUser: adminPublicUser()}, rolesSvc,
			http.MethodPost, "/api/roles", map[string]any{"name": "admin"})
		assertAPIError(t, rec, http.StatusConflict, apierror.CodeConflict,
			"A role with that name already exists.")
	})

	t.Run("invalid permission returns 422", func(t *testing.T) {
		rolesSvc := &fakeRolesService{createErr: rolesservice.ErrInvalidInput}
		rec := doRolesRequest(t, &fakeUsersAuthService{currentUser: adminPublicUser()}, rolesSvc,
			http.MethodPost, "/api/roles", map[string]any{"name": "x", "permissions": []string{"orders:read"}})
		assertAPIError(t, rec, http.StatusUnprocessableEntity, apierror.CodeValidationFailed,
			"Invalid roles request.")
	})

	t.Run("missing roles:manage returns 403", func(t *testing.T) {
		rolesSvc := &fakeRolesService{}
		rec := doRolesRequest(t, &fakeUsersAuthService{currentUser: plainPublicUser()}, rolesSvc,
			http.MethodPost, "/api/roles", map[string]any{"name": "auditor"})
		assertAPIError(t, rec, http.StatusForbidden, apierror.CodeForbidden, noPermissionMessage)
		if rolesSvc.createCalled {
			t.Fatal("service was called for a user without roles:manage")
		}
	})
}

func TestRolesUpdateHandler(t *testing.T) {
	t.Run("system role returns 409", func(t *testing.T) {
		rolesSvc := &fakeRolesService{updateErr: rolesservice.ErrSystemRole}
		rec := doRolesRequest(t, &fakeUsersAuthService{currentUser: adminPublicUser()}, rolesSvc,
			http.MethodPatch, "/api/roles/"+uuid.New().String(),
			map[string]any{"name": "renamed"})
		assertAPIError(t, rec, http.StatusConflict, apierror.CodeConflict,
			"Built-in system roles cannot be modified or deleted.")
	})

	t.Run("success forwards permissions", func(t *testing.T) {
		roleID := uuid.New()
		rolesSvc := &fakeRolesService{updateResult: rolesdomain.Role{ID: roleID, Name: "auditor"}}
		rec := doRolesRequest(t, &fakeUsersAuthService{currentUser: adminPublicUser()}, rolesSvc,
			http.MethodPatch, "/api/roles/"+roleID.String(),
			map[string]any{"permissions": []string{"users:read"}})
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if rolesSvc.updateInput.Permissions == nil || len(*rolesSvc.updateInput.Permissions) != 1 {
			t.Fatalf("update permissions = %v, want one entry", rolesSvc.updateInput.Permissions)
		}
	})
}

func TestRolesDeleteHandler(t *testing.T) {
	t.Run("success returns 200", func(t *testing.T) {
		roleID := uuid.New()
		rolesSvc := &fakeRolesService{}
		rec := doRolesRequest(t, &fakeUsersAuthService{currentUser: adminPublicUser()}, rolesSvc,
			http.MethodDelete, "/api/roles/"+roleID.String(), nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if !rolesSvc.deleteCalled || rolesSvc.deleteID != roleID.String() {
			t.Fatalf("delete not forwarded: called=%v id=%q", rolesSvc.deleteCalled, rolesSvc.deleteID)
		}
	})

	t.Run("role in use returns 409", func(t *testing.T) {
		rolesSvc := &fakeRolesService{deleteErr: rolesservice.ErrRoleInUse}
		rec := doRolesRequest(t, &fakeUsersAuthService{currentUser: adminPublicUser()}, rolesSvc,
			http.MethodDelete, "/api/roles/"+uuid.New().String(), nil)
		assertAPIError(t, rec, http.StatusConflict, apierror.CodeConflict,
			"This role is assigned to one or more users and cannot be deleted.")
	})
}

func TestPermissionsListHandler(t *testing.T) {
	rec := doRolesRequest(t, &fakeUsersAuthService{currentUser: adminPublicUser()}, &fakeRolesService{},
		http.MethodGet, "/api/permissions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	want := map[string]bool{"users:read": false, "users:manage": false, "roles:read": false, "roles:manage": false, "system_events:read": false, "projects:create": false}
	for _, p := range body.Permissions {
		if _, ok := want[p]; !ok {
			t.Fatalf("unexpected permission %q in catalog", p)
		}
		want[p] = true
	}
	for p, seen := range want {
		if !seen {
			t.Fatalf("permission %q missing from catalog response", p)
		}
	}
}

func newRolesRouter(authSvc handlers.UsersAuthenticator, rolesSvc handlers.RolesService) chi.Router {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	cfg := huma.DefaultConfig("Test API", "1.0.0")
	cfg.Transformers = append(cfg.Transformers, apierror.HumaErrorTransformer)
	api := humachi.New(router, cfg)
	handlers.RegisterRoles(api, authSvc, rolesSvc)
	return router
}

func doRolesRequest(t *testing.T, authSvc handlers.UsersAuthenticator, rolesSvc handlers.RolesService, method, path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	router := newRolesRouter(authSvc, rolesSvc)

	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	if authSvc != nil {
		req.Header.Set("Authorization", "Bearer access-token")
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

type fakeRolesService struct {
	listCalled bool
	listResult []rolesdomain.Role
	listErr    error

	getResult rolesdomain.Role
	getErr    error

	createCalled bool
	createInput  rolesservice.CreateRoleInput
	createResult rolesdomain.Role
	createErr    error

	updateInput  rolesservice.UpdateRoleInput
	updateResult rolesdomain.Role
	updateErr    error

	deleteCalled bool
	deleteID     string
	deleteErr    error
}

func (f *fakeRolesService) ListRoles(ctx context.Context) ([]rolesdomain.Role, error) {
	f.listCalled = true
	return f.listResult, f.listErr
}

func (f *fakeRolesService) GetRole(ctx context.Context, id string) (rolesdomain.Role, error) {
	if f.getErr != nil {
		return rolesdomain.Role{}, f.getErr
	}
	return f.getResult, nil
}

func (f *fakeRolesService) CreateRole(ctx context.Context, input rolesservice.CreateRoleInput) (rolesdomain.Role, error) {
	f.createCalled = true
	f.createInput = input
	if f.createErr != nil {
		return rolesdomain.Role{}, f.createErr
	}
	return f.createResult, nil
}

func (f *fakeRolesService) UpdateRole(ctx context.Context, input rolesservice.UpdateRoleInput) (rolesdomain.Role, error) {
	f.updateInput = input
	if f.updateErr != nil {
		return rolesdomain.Role{}, f.updateErr
	}
	return f.updateResult, nil
}

func (f *fakeRolesService) DeleteRole(ctx context.Context, actorUserID string, id string) error {
	f.deleteCalled = true
	f.deleteID = id
	return f.deleteErr
}
