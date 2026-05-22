package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	"github.com/aklmans/wow-dashboard-api/internal/users/domain"
	userservice "github.com/aklmans/wow-dashboard-api/internal/users/service"
)

// adminUser is an authenticated user holding the wildcard permission.
func adminPublicUser() *authservice.PublicUser {
	return &authservice.PublicUser{ID: uuid.New().String(), Status: "active", Permissions: []string{"*"}}
}

// plainPublicUser is an authenticated user with no admin permissions.
func plainPublicUser() *authservice.PublicUser {
	return &authservice.PublicUser{ID: uuid.New().String(), Status: "active", Permissions: []string{}}
}

const noPermissionMessage = "You do not have permission to perform this action."

func TestUsersHandler(t *testing.T) {
	t.Run("missing authorization returns safe unauthorized envelope", func(t *testing.T) {
		router := newUsersTestRouter(&fakeUsersAuthService{}, &fakeUsersService{})
		req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
			"Authorization token missing or invalid.")
	})

	t.Run("invalid token returns safe unauthorized envelope", func(t *testing.T) {
		authSvc := &fakeUsersAuthService{currentUserErr: authservice.ErrInvalidToken}
		router := newUsersTestRouter(authSvc, &fakeUsersService{})
		req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
		req.Header.Set("Authorization", "Bearer bad-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
			"Authorization token missing or invalid.")
	})

	t.Run("success returns paginated users response", func(t *testing.T) {
		userID := uuid.New()
		createdAt := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
		usersSvc := &fakeUsersService{
			result: domain.ListUsersResult{
				Users: []domain.User{{
					ID:          userID,
					Email:       "demo@minimals.cc",
					DisplayName: "Demo User",
					Status:      domain.UserStatusActive,
					Roles:       []string{"admin"},
					CreatedAt:   createdAt,
					UpdatedAt:   createdAt.Add(time.Minute),
				}},
				Page:     2,
				PageSize: 5,
				Total:    12,
			},
		}
		router := newUsersTestRouter(&fakeUsersAuthService{currentUser: adminPublicUser()}, usersSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/users?page=2&pageSize=5&search=demo&role=admin&status=active", nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if usersSvc.input.Search != "demo" || usersSvc.input.Role != "admin" || usersSvc.input.Status != "active" {
			t.Fatalf("service filters = %#v, want demo/admin/active", usersSvc.input)
		}

		var body usersListResponse
		decodeJSON(t, rec, &body)
		if len(body.Users) != 1 || body.Users[0].ID != userID.String() {
			t.Fatalf("users = %#v, want one demo user", body.Users)
		}
		if len(body.Users[0].Roles) != 1 || body.Users[0].Roles[0] != "admin" {
			t.Fatalf("user roles = %v, want [admin]", body.Users[0].Roles)
		}
	})

	t.Run("non-admin authenticated user returns 403", func(t *testing.T) {
		usersSvc := &fakeUsersService{}
		router := newUsersTestRouter(&fakeUsersAuthService{currentUser: plainPublicUser()}, usersSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusForbidden, apierror.CodeForbidden, noPermissionMessage)
		if usersSvc.input.Page != 0 {
			t.Fatalf("service was called for a user without users:read")
		}
	})
}

func TestUserDetailHandler(t *testing.T) {
	t.Run("missing user returns 404", func(t *testing.T) {
		usersSvc := &fakeUsersService{getErr: userservice.ErrNotFound}
		router := newUsersTestRouter(&fakeUsersAuthService{currentUser: adminPublicUser()}, usersSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/users/"+uuid.New().String(), nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusNotFound, apierror.CodeNotFound, "User not found.")
	})

	t.Run("success returns user with roles and without password_hash", func(t *testing.T) {
		userID := uuid.New()
		usersSvc := &fakeUsersService{getResult: domain.User{
			ID:          userID,
			Email:       "demo@minimals.cc",
			DisplayName: "Demo User",
			Status:      domain.UserStatusActive,
			Roles:       []string{"admin"},
		}}
		router := newUsersTestRouter(&fakeUsersAuthService{currentUser: adminPublicUser()}, usersSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/users/"+userID.String(), nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		raw := rec.Body.Bytes()
		var body userDetailResponseBody
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode body: %v; body=%s", err, raw)
		}
		if body.User.ID != userID.String() || len(body.User.Roles) != 1 || body.User.Roles[0] != "admin" {
			t.Fatalf("user = %#v, want demo admin", body.User)
		}
		var bag map[string]any
		if err := json.Unmarshal(raw, &bag); err != nil {
			t.Fatalf("decode bag: %v", err)
		}
		userObj, _ := bag["user"].(map[string]any)
		if _, exists := userObj["password_hash"]; exists {
			t.Fatal("response leaks password_hash")
		}
	})

	t.Run("non-admin authenticated user returns 403", func(t *testing.T) {
		usersSvc := &fakeUsersService{}
		router := newUsersTestRouter(&fakeUsersAuthService{currentUser: plainPublicUser()}, usersSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/users/"+uuid.New().String(), nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusForbidden, apierror.CodeForbidden, noPermissionMessage)
		if usersSvc.getID != "" {
			t.Fatalf("service was called for a user without users:read")
		}
	})
}

func TestUsersUpdateHandler(t *testing.T) {
	targetID := uuid.New()

	patch := func(t *testing.T, authSvc *fakeUsersAuthService, usersSvc *fakeUsersService, id string, body map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		router := newUsersTestRouter(authSvc, usersSvc)
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		req := httptest.NewRequest(http.MethodPatch, "/api/users/"+id, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer access-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	t.Run("admin updates status and roles", func(t *testing.T) {
		admin := adminPublicUser()
		roleID := uuid.New().String()
		usersSvc := &fakeUsersService{updateResult: domain.User{
			ID: targetID, Email: "demo@minimals.cc", DisplayName: "Demo User",
			Status: domain.UserStatusDisabled, Roles: []string{"admin"},
		}}
		rec := patch(t, &fakeUsersAuthService{currentUser: admin}, usersSvc, targetID.String(),
			map[string]any{"status": "disabled", "roleIds": []string{roleID}})

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if usersSvc.updateInput.TargetUserID != targetID.String() || usersSvc.updateInput.ActorUserID != admin.ID {
			t.Fatalf("ids = target %q actor %q", usersSvc.updateInput.TargetUserID, usersSvc.updateInput.ActorUserID)
		}
		if usersSvc.updateInput.Status == nil || *usersSvc.updateInput.Status != "disabled" {
			t.Fatalf("status = %v, want disabled", usersSvc.updateInput.Status)
		}
		if usersSvc.updateInput.RoleIDs == nil || len(*usersSvc.updateInput.RoleIDs) != 1 {
			t.Fatalf("roleIds = %v, want one id", usersSvc.updateInput.RoleIDs)
		}
	})

	t.Run("missing authorization returns 401", func(t *testing.T) {
		router := newUsersTestRouter(&fakeUsersAuthService{}, &fakeUsersService{})
		req := httptest.NewRequest(http.MethodPatch, "/api/users/"+targetID.String(), bytes.NewReader([]byte(`{"status":"disabled"}`)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
			"Authorization token missing or invalid.")
	})

	t.Run("user without users:manage returns 403", func(t *testing.T) {
		rec := patch(t, &fakeUsersAuthService{currentUser: plainPublicUser()},
			&fakeUsersService{}, targetID.String(), map[string]any{"status": "disabled"})
		assertAPIError(t, rec, http.StatusForbidden, apierror.CodeForbidden, noPermissionMessage)
	})

	t.Run("self modification returns 403", func(t *testing.T) {
		admin := adminPublicUser()
		usersSvc := &fakeUsersService{updateErr: userservice.ErrSelfModification}
		rec := patch(t, &fakeUsersAuthService{currentUser: admin}, usersSvc, admin.ID,
			map[string]any{"status": "disabled"})
		assertAPIError(t, rec, http.StatusForbidden, apierror.CodeForbidden,
			"Administrators cannot change their own status or roles.")
	})

	t.Run("missing user returns 404", func(t *testing.T) {
		usersSvc := &fakeUsersService{updateErr: userservice.ErrNotFound}
		rec := patch(t, &fakeUsersAuthService{currentUser: adminPublicUser()}, usersSvc, targetID.String(),
			map[string]any{"status": "disabled"})
		assertAPIError(t, rec, http.StatusNotFound, apierror.CodeNotFound, "User not found.")
	})

	t.Run("invalid status rejected at the schema edge", func(t *testing.T) {
		rec := patch(t, &fakeUsersAuthService{currentUser: adminPublicUser()}, &fakeUsersService{}, targetID.String(),
			map[string]any{"status": "bogus"})
		assertAPIError(t, rec, http.StatusUnprocessableEntity, apierror.CodeValidationFailed, "Invalid request.")
	})
}

func TestUsersOpenAPIErrorResponsesUseAPIErrorEnvelope(t *testing.T) {
	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("Test API", "1.0.0"))
	handlers.RegisterUsers(api, &fakeUsersAuthService{}, &fakeUsersService{})

	specJSON, err := json.Marshal(api.OpenAPI())
	if err != nil {
		t.Fatalf("failed to marshal OpenAPI spec: %v", err)
	}
	var spec map[string]any
	if err := json.Unmarshal(specJSON, &spec); err != nil {
		t.Fatalf("failed to unmarshal OpenAPI spec: %v", err)
	}

	operation := objectAt(t, spec, "paths", "/api/users", "get")
	responses := objectAt(t, operation, "responses")
	for _, status := range []string{"401", "403", "422", "500"} {
		response := dereferenceResponse(t, spec, objectAt(t, responses, status))
		content := objectAt(t, response, "content")
		mediaType := objectAt(t, content, "application/json")
		schema := dereferenceSchema(t, spec, objectAt(t, mediaType, "schema"))
		properties := objectAt(t, schema, "properties")
		for _, field := range []string{"code", "message", "request_id"} {
			if _, ok := properties[field]; !ok {
				t.Fatalf("status %s schema missing %q property", status, field)
			}
		}
	}
}

func TestUsersOpenAPIUsersArrayIsNonNullable(t *testing.T) {
	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("Test API", "1.0.0"))
	handlers.RegisterUsers(api, &fakeUsersAuthService{}, &fakeUsersService{})

	specJSON, err := json.Marshal(api.OpenAPI())
	if err != nil {
		t.Fatalf("failed to marshal OpenAPI spec: %v", err)
	}
	var spec map[string]any
	if err := json.Unmarshal(specJSON, &spec); err != nil {
		t.Fatalf("failed to unmarshal OpenAPI spec: %v", err)
	}

	usersSchema := objectAt(t, spec, "components", "schemas", "UsersListBody", "properties", "users")
	switch schemaType := usersSchema["type"].(type) {
	case string:
		if schemaType != "array" {
			t.Fatalf("users schema type = %q, want array", schemaType)
		}
	case []any:
		for _, value := range schemaType {
			if value == "null" {
				t.Fatalf("users schema type = %#v, want non-nullable array", schemaType)
			}
		}
	default:
		t.Fatalf("users schema type is %T, want string or array", schemaType)
	}
}

type fakeUsersAuthService struct {
	currentUserToken string
	currentUser      *authservice.PublicUser
	currentUserErr   error
}

func (f *fakeUsersAuthService) CurrentUser(ctx context.Context, rawAccessToken string) (*authservice.PublicUser, error) {
	f.currentUserToken = rawAccessToken
	if f.currentUserErr != nil {
		return nil, f.currentUserErr
	}
	if f.currentUser != nil {
		return f.currentUser, nil
	}
	return plainPublicUser(), nil
}

type fakeUsersService struct {
	input        userservice.ListUsersInput
	result       domain.ListUsersResult
	err          error
	getID        string
	getResult    domain.User
	getErr       error
	updateInput  userservice.UpdateUserInput
	updateResult domain.User
	updateErr    error
}

func (f *fakeUsersService) ListUsers(ctx context.Context, input userservice.ListUsersInput) (domain.ListUsersResult, error) {
	f.input = input
	if f.err != nil {
		return domain.ListUsersResult{}, f.err
	}
	return f.result, nil
}

func (f *fakeUsersService) GetUser(ctx context.Context, id string) (domain.User, error) {
	f.getID = id
	if f.getErr != nil {
		return domain.User{}, f.getErr
	}
	return f.getResult, nil
}

func (f *fakeUsersService) UpdateUser(ctx context.Context, input userservice.UpdateUserInput) (domain.User, error) {
	f.updateInput = input
	if f.updateErr != nil {
		return domain.User{}, f.updateErr
	}
	return f.updateResult, nil
}

func newUsersTestRouter(authSvc handlers.UsersAuthenticator, usersSvc handlers.UsersService) chi.Router {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	cfg := huma.DefaultConfig("Test API", "1.0.0")
	cfg.Transformers = append(cfg.Transformers, apierror.HumaErrorTransformer)
	api := humachi.New(router, cfg)
	handlers.RegisterUsers(api, authSvc, usersSvc)
	return router
}

type usersListResponse struct {
	Users    []usersListItem `json:"users"`
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
	Total    int             `json:"total"`
}

type usersListItem struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	Status      string    `json:"status"`
	Roles       []string  `json:"roles"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type userDetailResponseBody struct {
	User usersListItem `json:"user"`
}
