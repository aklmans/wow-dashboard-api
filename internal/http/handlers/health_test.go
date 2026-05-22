package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/aklmans/wow-dashboard-api/internal/app"
	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
	"github.com/aklmans/wow-dashboard-api/internal/http/handlers"
)

func TestHealthAndReadyEndpoints(t *testing.T) {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	api := app.NewAPI(router)
	app.RegisterRoutes(api, app.Dependencies{ReadyChecker: handlers.ReadyCheckerFunc(func(context.Context) error {
		return nil
	})})

	tests := []struct {
		name           string
		path           string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "healthz endpoint",
			path:           "/healthz",
			expectedStatus: http.StatusOK,
			expectedBody:   `{"status":"ok"}`,
		},
		{
			name:           "readyz endpoint",
			path:           "/readyz",
			expectedStatus: http.StatusOK,
			expectedBody:   `{"status":"ready"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			var body map[string]interface{}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			expectedJSON := make(map[string]interface{})
			if err := json.Unmarshal([]byte(tt.expectedBody), &expectedJSON); err != nil {
				t.Fatalf("failed to unmarshal expected body: %v", err)
			}

			for k, v := range expectedJSON {
				if body[k] != v {
					t.Errorf("expected key %q to have value %v, got %v", k, v, body[k])
				}
			}
		})
	}
}

func TestReadyEndpoint_DependencyFailure(t *testing.T) {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	api := app.NewAPI(router)
	app.RegisterRoutes(api, app.Dependencies{ReadyChecker: handlers.ReadyCheckerFunc(func(context.Context) error {
		return errors.New("pgx: failed to connect to postgres://user:secret@localhost:5432/spec")
	})})

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
	contentType := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}

	var body apierror.ResponseBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to unmarshal error response %q: %v", rec.Body.String(), err)
	}
	if body.Code != apierror.CodeServiceUnavailable {
		t.Errorf("code = %q, want %q", body.Code, apierror.CodeServiceUnavailable)
	}
	if body.Message != "Service is not ready." {
		t.Errorf("message = %q, want Service is not ready.", body.Message)
	}
	if body.RequestID == "" {
		t.Error("request_id is empty")
	}

	raw := strings.ToLower(rec.Body.String())
	for _, leak := range []string{"postgres://", "secret", "localhost", "pgx", "failed to connect"} {
		if strings.Contains(raw, strings.ToLower(leak)) {
			t.Errorf("readyz error response leaks %q: %s", leak, rec.Body.String())
		}
	}
}

func TestReadyOpenAPIIncludesServiceUnavailableAPIError(t *testing.T) {
	router := chi.NewRouter()
	api := app.NewAPI(router)
	app.RegisterRoutes(api, app.Dependencies{ReadyChecker: handlers.NoopReadyChecker{}})

	specJSON, err := json.Marshal(api.OpenAPI())
	if err != nil {
		t.Fatalf("failed to marshal OpenAPI spec: %v", err)
	}

	var spec map[string]any
	if err := json.Unmarshal(specJSON, &spec); err != nil {
		t.Fatalf("failed to unmarshal OpenAPI spec: %v", err)
	}

	readyz := objectAt(t, spec, "paths", "/readyz", "get", "responses")
	response := objectAt(t, readyz, "503")
	if ref, _ := response["$ref"].(string); ref != "#/components/responses/APIError" {
		t.Fatalf("readyz 503 ref = %q, want #/components/responses/APIError", ref)
	}
}
