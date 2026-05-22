package handlers_test

import (
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
		if authSvc.currentUserToken != "bad-token" {
			t.Fatalf("CurrentUser token = %q, want bad-token", authSvc.currentUserToken)
		}
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
					Role:        domain.UserRoleAdmin,
					CreatedAt:   createdAt,
					UpdatedAt:   createdAt.Add(time.Minute),
				}},
				Page:     2,
				PageSize: 5,
				Total:    12,
			},
		}
		router := newUsersTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{
				ID:     "user-123",
				Status: "active",
				Role:   "admin",
			},
		}, usersSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/users?page=2&pageSize=5&search=demo&role=admin&status=active", nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if usersSvc.input.Page != 2 || usersSvc.input.PageSize != 5 {
			t.Fatalf("service pagination = %d/%d, want 2/5", usersSvc.input.Page, usersSvc.input.PageSize)
		}
		if usersSvc.input.Search != "demo" || usersSvc.input.Role != "admin" || usersSvc.input.Status != "active" {
			t.Fatalf("service filters = %#v, want demo/admin/active", usersSvc.input)
		}

		var body usersListResponse
		decodeJSON(t, rec, &body)
		if body.Page != 2 || body.PageSize != 5 || body.Total != 12 {
			t.Fatalf("metadata = %#v, want page=2 pageSize=5 total=12", body)
		}
		if len(body.Users) != 1 {
			t.Fatalf("len(users) = %d, want 1", len(body.Users))
		}
		if body.Users[0].ID != userID.String() || body.Users[0].Email != "demo@minimals.cc" {
			t.Fatalf("user = %#v, want demo user", body.Users[0])
		}
	})

	t.Run("service rejection returns validation envelope", func(t *testing.T) {
		router := newUsersTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: "user-123", Status: "active", Role: "admin"},
		}, &fakeUsersService{err: userservice.ErrInvalidInput})

		req := httptest.NewRequest(http.MethodGet, "/api/users?pageSize=50", nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnprocessableEntity, apierror.CodeValidationFailed,
			"Invalid users query.")
	})

	t.Run("page size over maximum is rejected at the schema edge", func(t *testing.T) {
		router := newUsersTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: "user-123", Status: "active", Role: "admin"},
		}, &fakeUsersService{})

		req := httptest.NewRequest(http.MethodGet, "/api/users?pageSize=101", nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnprocessableEntity, apierror.CodeValidationFailed,
			"Invalid request.")
	})

	t.Run("non-admin authenticated user returns 403", func(t *testing.T) {
		usersSvc := &fakeUsersService{}
		router := newUsersTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: "user-123", Status: "active", Role: "user"},
		}, usersSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusForbidden, apierror.CodeForbidden, "Admin role required.")
		if usersSvc.input.Page != 0 || usersSvc.input.PageSize != 0 {
			t.Fatalf("service was called for non-admin: %#v", usersSvc.input)
		}
	})
}

func TestUserDetailHandler(t *testing.T) {
	t.Run("missing authorization returns safe unauthorized envelope", func(t *testing.T) {
		router := newUsersTestRouter(&fakeUsersAuthService{}, &fakeUsersService{})

		req := httptest.NewRequest(http.MethodGet, "/api/users/"+uuid.New().String(), nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
			"Authorization token missing or invalid.")
	})

	t.Run("invalid token returns safe unauthorized envelope", func(t *testing.T) {
		authSvc := &fakeUsersAuthService{currentUserErr: authservice.ErrInvalidToken}
		router := newUsersTestRouter(authSvc, &fakeUsersService{})

		req := httptest.NewRequest(http.MethodGet, "/api/users/"+uuid.New().String(), nil)
		req.Header.Set("Authorization", "Bearer bad-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
			"Authorization token missing or invalid.")
	})

	t.Run("invalid uuid returns 422 validation envelope", func(t *testing.T) {
		usersSvc := &fakeUsersService{}
		router := newUsersTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: "user-123", Status: "active", Role: "admin"},
		}, usersSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/users/not-a-uuid", nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnprocessableEntity, apierror.CodeValidationFailed,
			"Invalid request.")
		if usersSvc.getID != "" {
			t.Fatalf("service was called with %q for invalid UUID", usersSvc.getID)
		}
	})

	t.Run("missing user returns 404", func(t *testing.T) {
		usersSvc := &fakeUsersService{getErr: userservice.ErrNotFound}
		router := newUsersTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: "user-123", Status: "active", Role: "admin"},
		}, usersSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/users/"+uuid.New().String(), nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusNotFound, apierror.CodeNotFound, "User not found.")
	})

	t.Run("success returns user without password_hash", func(t *testing.T) {
		userID := uuid.New()
		createdAt := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
		usersSvc := &fakeUsersService{getResult: domain.User{
			ID:          userID,
			Email:       "demo@minimals.cc",
			DisplayName: "Demo User",
			Status:      domain.UserStatusActive,
			Role:        domain.UserRoleAdmin,
			CreatedAt:   createdAt,
			UpdatedAt:   createdAt.Add(time.Minute),
		}}
		router := newUsersTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: "user-123", Status: "active", Role: "admin"},
		}, usersSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/users/"+userID.String(), nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if usersSvc.getID != userID.String() {
			t.Fatalf("service got id = %q, want %q", usersSvc.getID, userID.String())
		}

		raw := rec.Body.Bytes()

		var body userDetailResponseBody
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode body: %v; body=%s", err, raw)
		}
		if body.User.ID != userID.String() || body.User.Email != "demo@minimals.cc" {
			t.Fatalf("user = %#v, want demo user", body.User)
		}

		var bag map[string]any
		if err := json.Unmarshal(raw, &bag); err != nil {
			t.Fatalf("decode bag: %v", err)
		}
		userObj, _ := bag["user"].(map[string]any)
		if _, exists := userObj["password_hash"]; exists {
			t.Fatal("response leaks password_hash")
		}
		if _, exists := userObj["passwordHash"]; exists {
			t.Fatal("response leaks passwordHash")
		}
	})

	t.Run("non-admin authenticated user returns 403", func(t *testing.T) {
		usersSvc := &fakeUsersService{}
		router := newUsersTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: "user-123", Status: "active", Role: "user"},
		}, usersSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/users/"+uuid.New().String(), nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusForbidden, apierror.CodeForbidden, "Admin role required.")
		if usersSvc.getID != "" {
			t.Fatalf("service was called for non-admin: id=%q", usersSvc.getID)
		}
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

func TestUsersDetailOpenAPIContract(t *testing.T) {
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

	operation := objectAt(t, spec, "paths", "/api/users/{id}", "get")

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
		t.Fatalf("path parameter id missing on /api/users/{id}")
	}
	if format, _ := idSchema["format"].(string); format != "uuid" {
		t.Fatalf("path parameter id schema format = %q, want \"uuid\"", format)
	}

	responses := objectAt(t, operation, "responses")
	for _, status := range []string{"401", "403", "404", "422", "500"} {
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
	return &authservice.PublicUser{ID: "user-123", Status: "active", Role: "user"}, nil
}

type fakeUsersService struct {
	input     userservice.ListUsersInput
	result    domain.ListUsersResult
	err       error
	getID     string
	getResult domain.User
	getErr    error
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
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type userDetailResponseBody struct {
	User usersListItem `json:"user"`
}
