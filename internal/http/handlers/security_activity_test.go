package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/app"
	"github.com/aklmans/wow-dashboard-api/internal/auth/service"
	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
	"github.com/aklmans/wow-dashboard-api/internal/http/handlers"
	systemeventsdomain "github.com/aklmans/wow-dashboard-api/internal/systemevents/domain"
	systemeventsservice "github.com/aklmans/wow-dashboard-api/internal/systemevents/service"
)

const securityActivityUserID = "00000000-0000-0000-0000-000000000123"

func TestSecurityActivityHandler(t *testing.T) {
	t.Run("lists the signed-in user's own activity, scoped to them", func(t *testing.T) {
		activitySvc := &fakeActivityService{
			result: systemeventsdomain.ListEventsResult{
				Events: []systemeventsdomain.Event{
					{
						ID:        uuid.New(),
						EventType: "auth.sign_in.succeeded",
						Message:   "Auth sign-in succeeded.",
						Metadata:  map[string]any{"masked_email": "d***@example.com"},
						CreatedAt: time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
					},
				},
				Limit: 20,
			},
		}
		authSvc := &fakeAuthService{currentUser: &service.PublicUser{ID: securityActivityUserID}}
		router := newSecurityActivityTestRouter(authSvc, activitySvc)

		req := httptest.NewRequest(http.MethodGet, "/api/auth/security-activity", nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if activitySvc.input.UserID.String() != securityActivityUserID {
			t.Errorf("ListUserActivity userID = %s, want the current user", activitySvc.input.UserID)
		}
		var body struct {
			Events []struct {
				EventType string `json:"eventType"`
			} `json:"events"`
		}
		decodeJSON(t, rec, &body)
		if len(body.Events) != 1 || body.Events[0].EventType != "auth.sign_in.succeeded" {
			t.Fatalf("events = %#v, want one sign-in event", body.Events)
		}
	})

	t.Run("requires authentication", func(t *testing.T) {
		router := newSecurityActivityTestRouter(&fakeAuthService{}, &fakeActivityService{})

		req := httptest.NewRequest(http.MethodGet, "/api/auth/security-activity", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
			"Authorization token missing or invalid.")
	})

	t.Run("blocked while impersonating", func(t *testing.T) {
		authSvc := &fakeAuthService{
			currentUser: &service.PublicUser{ID: securityActivityUserID, ImpersonatorID: "admin-1"},
		}
		activitySvc := &fakeActivityService{}
		router := newSecurityActivityTestRouter(authSvc, activitySvc)

		req := httptest.NewRequest(http.MethodGet, "/api/auth/security-activity", nil)
		req.Header.Set("Authorization", "Bearer impersonation-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusForbidden, apierror.CodeForbidden,
			"Security activity is unavailable while impersonating a user.")
		if activitySvc.called {
			t.Error("ListUserActivity was called during impersonation")
		}
	})

	t.Run("rejects a malformed cursor", func(t *testing.T) {
		authSvc := &fakeAuthService{currentUser: &service.PublicUser{ID: securityActivityUserID}}
		router := newSecurityActivityTestRouter(authSvc, &fakeActivityService{})

		req := httptest.NewRequest(http.MethodGet, "/api/auth/security-activity?cursor=!!!notbase64!!!", nil)
		req.Header.Set("Authorization", "Bearer access-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnprocessableEntity, apierror.CodeValidationFailed,
			"Invalid pagination cursor.")
	})
}

// --- helpers + fake ---

func newSecurityActivityTestRouter(authSvc handlers.AuthService, activitySvc handlers.SecurityActivityService) chi.Router {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	api := app.NewAPI(router, true)
	handlers.RegisterSecurityActivity(api, authSvc, activitySvc)
	return router
}

type fakeActivityService struct {
	called bool
	input  systemeventsservice.ListUserActivityInput
	result systemeventsdomain.ListEventsResult
	err    error
}

func (f *fakeActivityService) ListUserActivity(ctx context.Context, input systemeventsservice.ListUserActivityInput) (systemeventsdomain.ListEventsResult, error) {
	f.called = true
	f.input = input
	return f.result, f.err
}
