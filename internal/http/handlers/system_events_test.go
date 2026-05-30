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
	"github.com/aklmans/wow-dashboard-api/internal/systemevents/domain"
	systemeventsservice "github.com/aklmans/wow-dashboard-api/internal/systemevents/service"
)

func TestSystemEventsHandler(t *testing.T) {
	t.Run("missing authorization returns unauthorized envelope", func(t *testing.T) {
		router := newSystemEventsTestRouter(&fakeUsersAuthService{}, &fakeSystemEventsService{})

		req := httptest.NewRequest(http.MethodGet, "/api/system-events", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
			"Authorization token missing or invalid.")
	})

	t.Run("non-admin authenticated user returns 403", func(t *testing.T) {
		eventsSvc := &fakeSystemEventsService{}
		router := newSystemEventsTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: "user-123", Status: "active", Permissions: []string{}},
		}, eventsSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/system-events", nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusForbidden, apierror.CodeForbidden, noPermissionMessage)
		if eventsSvc.called {
			t.Fatal("service was called for non-admin user")
		}
	})

	t.Run("admin success returns non-null events array", func(t *testing.T) {
		eventsSvc := &fakeSystemEventsService{}
		router := newSystemEventsTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: "admin-123", Status: "active", Permissions: []string{"*"}},
		}, eventsSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/system-events", nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if eventsSvc.input.Limit != 20 || !eventsSvc.input.LimitProvided {
			t.Fatalf("service limit = %#v, want provided default 20", eventsSvc.input)
		}

		raw := rec.Body.Bytes()

		var body systemEventsListResponseBody
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode body: %v; body=%s", err, raw)
		}
		if body.Events == nil {
			t.Fatal("events is nil, want empty non-null array")
		}
		if len(body.Events) != 0 {
			t.Fatalf("len(events) = %d, want 0", len(body.Events))
		}
		if body.Limit != 20 {
			t.Fatalf("limit = %d, want 20", body.Limit)
		}
	})

	t.Run("admin success returns metadata as object", func(t *testing.T) {
		eventID := uuid.New()
		createdAt := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
		eventsSvc := &fakeSystemEventsService{
			result: domain.ListEventsResult{
				Events: []domain.Event{{
					ID:        eventID,
					EventType: "projects.project.created",
					Message:   "Project created.",
					Metadata:  map[string]any{"project_id": "project-1"},
					CreatedAt: createdAt,
				}},
				Limit: 5,
			},
		}
		router := newSystemEventsTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: "admin-123", Status: "active", Permissions: []string{"*"}},
		}, eventsSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/system-events?limit=5", nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if eventsSvc.input.Limit != 5 || !eventsSvc.input.LimitProvided {
			t.Fatalf("service limit = %#v, want provided 5", eventsSvc.input)
		}

		rawBytes := rec.Body.Bytes()

		var body systemEventsListResponseBody
		if err := json.Unmarshal(rawBytes, &body); err != nil {
			t.Fatalf("decode body: %v; body=%s", err, rawBytes)
		}
		if len(body.Events) != 1 {
			t.Fatalf("len(events) = %d, want 1", len(body.Events))
		}
		if body.Events[0].ID != eventID.String() || body.Events[0].EventType != "projects.project.created" {
			t.Fatalf("event = %#v, want project event", body.Events[0])
		}
		if body.Events[0].Metadata["project_id"] != "project-1" {
			t.Fatalf("metadata = %#v, want project_id object", body.Events[0].Metadata)
		}

		var raw map[string]any
		if err := json.Unmarshal(rawBytes, &raw); err != nil {
			t.Fatalf("decode raw response: %v", err)
		}
		events, _ := raw["events"].([]any)
		event, _ := events[0].(map[string]any)
		if _, ok := event["metadata"].(map[string]any); !ok {
			t.Fatalf("metadata raw type = %T, want object", event["metadata"])
		}
	})

	t.Run("over-limit is rejected by the contract", func(t *testing.T) {
		// limit > 100 is now bounded by the OpenAPI maximum, so Huma rejects it
		// before the handler with the framework's validation envelope.
		router := newSystemEventsTestRouter(&fakeUsersAuthService{
			currentUser: &authservice.PublicUser{ID: "admin-123", Status: "active", Permissions: []string{"*"}},
		}, &fakeSystemEventsService{})

		req := httptest.NewRequest(http.MethodGet, "/api/system-events?limit=101", nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnprocessableEntity, apierror.CodeValidationFailed,
			"Invalid request.")
	})
}

func TestSystemEventsCursorRoundTrip(t *testing.T) {
	lastID := uuid.MustParse("c8a89c0b-8e75-4e61-9fa0-70fb83554e66")
	lastCreatedAt := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
	fake := &fakeSystemEventsService{result: domain.ListEventsResult{
		Events: []domain.Event{
			{ID: uuid.New(), EventType: "a", Metadata: map[string]any{}, CreatedAt: lastCreatedAt.Add(time.Minute)},
			{ID: lastID, EventType: "b", Metadata: map[string]any{}, CreatedAt: lastCreatedAt},
		},
		Limit:   20,
		HasMore: true,
	}}
	router := newSystemEventsTestRouter(&fakeUsersAuthService{
		currentUser: &authservice.PublicUser{ID: "admin-123", Status: "active", Permissions: []string{"*"}},
	}, fake)

	get := func(path string) struct {
		NextCursor string `json:"nextCursor"`
	} {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var body struct {
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		return body
	}

	page1 := get("/api/system-events")
	if page1.NextCursor == "" {
		t.Fatal("page 1 missing nextCursor when HasMore is true")
	}

	// Sending the cursor back must decode to the previous page's last keyset.
	_ = get("/api/system-events?cursor=" + page1.NextCursor)
	if fake.input.CursorCreatedAt == nil || !fake.input.CursorCreatedAt.Equal(lastCreatedAt) {
		t.Fatalf("decoded cursor created_at = %v, want %v", fake.input.CursorCreatedAt, lastCreatedAt)
	}
	if fake.input.CursorID == nil || *fake.input.CursorID != lastID {
		t.Fatalf("decoded cursor id = %v, want %v", fake.input.CursorID, lastID)
	}
}

func TestSystemEventsOpenAPIErrorResponsesUseAPIErrorEnvelope(t *testing.T) {
	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("Test API", "1.0.0"))
	handlers.RegisterSystemEvents(api, &fakeUsersAuthService{}, &fakeSystemEventsService{})

	specJSON, err := json.Marshal(api.OpenAPI())
	if err != nil {
		t.Fatalf("failed to marshal OpenAPI spec: %v", err)
	}

	var spec map[string]any
	if err := json.Unmarshal(specJSON, &spec); err != nil {
		t.Fatalf("failed to unmarshal OpenAPI spec: %v", err)
	}

	operation := objectAt(t, spec, "paths", "/api/system-events", "get")
	if operationID, _ := operation["operationId"].(string); operationID != "get-system-events" {
		t.Fatalf("operationId = %q, want get-system-events", operationID)
	}
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

func TestSystemEventsOpenAPIEventsArrayIsNonNullable(t *testing.T) {
	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("Test API", "1.0.0"))
	handlers.RegisterSystemEvents(api, &fakeUsersAuthService{}, &fakeSystemEventsService{})

	specJSON, err := json.Marshal(api.OpenAPI())
	if err != nil {
		t.Fatalf("failed to marshal OpenAPI spec: %v", err)
	}

	var spec map[string]any
	if err := json.Unmarshal(specJSON, &spec); err != nil {
		t.Fatalf("failed to unmarshal OpenAPI spec: %v", err)
	}

	eventsSchema := objectAt(t, spec, "components", "schemas", "SystemEventsListBody", "properties", "events")
	switch schemaType := eventsSchema["type"].(type) {
	case string:
		if schemaType != "array" {
			t.Fatalf("events schema type = %q, want array", schemaType)
		}
	case []any:
		for _, value := range schemaType {
			if value == "null" {
				t.Fatalf("events schema type = %#v, want non-nullable array", schemaType)
			}
		}
	default:
		t.Fatalf("events schema type is %T, want string or array", schemaType)
	}
}

type fakeSystemEventsService struct {
	called bool
	input  systemeventsservice.ListEventsInput
	result domain.ListEventsResult
	err    error
}

func (f *fakeSystemEventsService) ListEvents(ctx context.Context, input systemeventsservice.ListEventsInput) (domain.ListEventsResult, error) {
	f.called = true
	f.input = input
	if f.err != nil {
		return domain.ListEventsResult{}, f.err
	}
	if f.result.Limit == 0 {
		f.result.Limit = 20
	}
	return f.result, nil
}

func newSystemEventsTestRouter(authSvc handlers.UsersAuthenticator, eventsSvc handlers.SystemEventsService) chi.Router {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	cfg := huma.DefaultConfig("Test API", "1.0.0")
	cfg.Transformers = append(cfg.Transformers, apierror.HumaErrorTransformer)
	api := humachi.New(router, cfg)
	handlers.RegisterSystemEvents(api, authSvc, eventsSvc)
	return router
}

type systemEventsListResponseBody struct {
	Events []systemEventResponseItem `json:"events"`
	Limit  int                       `json:"limit"`
}

type systemEventResponseItem struct {
	ID        string         `json:"id"`
	EventType string         `json:"eventType"`
	Message   string         `json:"message"`
	Metadata  map[string]any `json:"metadata"`
	CreatedAt time.Time      `json:"createdAt"`
}
