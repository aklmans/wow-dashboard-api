package handlers_test

import (
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

	authservice "github.com/aklmans/wow-dashboard-api/internal/auth/service"
	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
	"github.com/aklmans/wow-dashboard-api/internal/http/handlers"
	"github.com/aklmans/wow-dashboard-api/internal/notifications/domain"
	notificationsservice "github.com/aklmans/wow-dashboard-api/internal/notifications/service"
)

type fakeNotificationsService struct {
	listInput    notificationsservice.ListInput
	listResult   domain.ListResult
	markReadUser uuid.UUID
	markReadID   uuid.UUID
	unread       int64
}

func (f *fakeNotificationsService) List(_ context.Context, input notificationsservice.ListInput) (domain.ListResult, error) {
	f.listInput = input
	return f.listResult, nil
}

func (f *fakeNotificationsService) MarkRead(_ context.Context, userID, id uuid.UUID) (int64, error) {
	f.markReadUser, f.markReadID = userID, id
	return f.unread, nil
}

func (f *fakeNotificationsService) MarkAllRead(_ context.Context, _ uuid.UUID) (int64, error) {
	return f.unread, nil
}

func newNotificationsTestRouter(authSvc handlers.UsersAuthenticator, notifSvc handlers.NotificationsService) chi.Router {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	cfg := huma.DefaultConfig("Test API", "1.0.0")
	cfg.Transformers = append(cfg.Transformers, apierror.HumaErrorTransformer)
	api := humachi.New(router, cfg)
	handlers.RegisterNotifications(api, authSvc, notifSvc)
	return router
}

func authedUser(id uuid.UUID) *fakeUsersAuthService {
	return &fakeUsersAuthService{currentUser: &authservice.PublicUser{ID: id.String(), Status: "active"}}
}

func TestNotificationsHandlerRequiresAuth(t *testing.T) {
	router := newNotificationsTestRouter(&fakeUsersAuthService{}, &fakeNotificationsService{})

	req := httptest.NewRequest(http.MethodGet, "/api/notifications", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
		"Authorization token missing or invalid.")
}

func TestNotificationsListReturnsArrayAndScopesToCurrentUser(t *testing.T) {
	uid := uuid.New()
	notifSvc := &fakeNotificationsService{listResult: domain.ListResult{
		Notifications: []domain.Notification{{
			ID:       uuid.New(),
			Type:     "users.roles.updated",
			Title:    "Your roles were updated",
			Metadata: map[string]any{},
		}},
		Limit:       20,
		UnreadCount: 2,
	}}
	router := newNotificationsTestRouter(authedUser(uid), notifSvc)

	req := httptest.NewRequest(http.MethodGet, "/api/notifications?unreadOnly=true", nil)
	req.Header.Set("Authorization", "Bearer access-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if notifSvc.listInput.UserID != uid {
		t.Fatalf("service UserID = %v, want the authenticated user %v", notifSvc.listInput.UserID, uid)
	}
	if !notifSvc.listInput.UnreadOnly {
		t.Fatal("unreadOnly query was not propagated to the service")
	}

	var body struct {
		Notifications []map[string]any `json:"notifications"`
		UnreadCount   int64            `json:"unreadCount"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, rec.Body.String())
	}
	if body.Notifications == nil {
		t.Fatal("notifications is nil, want a non-null array")
	}
	if len(body.Notifications) != 1 {
		t.Fatalf("len(notifications) = %d, want 1", len(body.Notifications))
	}
	if body.UnreadCount != 2 {
		t.Fatalf("unreadCount = %d, want 2", body.UnreadCount)
	}
}

func TestNotificationsMarkReadRejectsInvalidID(t *testing.T) {
	router := newNotificationsTestRouter(authedUser(uuid.New()), &fakeNotificationsService{})

	req := httptest.NewRequest(http.MethodPost, "/api/notifications/not-a-uuid/read", nil)
	req.Header.Set("Authorization", "Bearer access-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusUnprocessableEntity, apierror.CodeValidationFailed,
		"Invalid notification id.")
}

func TestNotificationsMarkReadScopesToUserAndReturnsCount(t *testing.T) {
	uid := uuid.New()
	nid := uuid.New()
	notifSvc := &fakeNotificationsService{unread: 1}
	router := newNotificationsTestRouter(authedUser(uid), notifSvc)

	req := httptest.NewRequest(http.MethodPost, "/api/notifications/"+nid.String()+"/read", nil)
	req.Header.Set("Authorization", "Bearer access-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if notifSvc.markReadUser != uid || notifSvc.markReadID != nid {
		t.Fatalf("MarkRead args = %v/%v, want %v/%v", notifSvc.markReadUser, notifSvc.markReadID, uid, nid)
	}

	var body struct {
		UnreadCount int64 `json:"unreadCount"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.UnreadCount != 1 {
		t.Fatalf("unreadCount = %d, want 1", body.UnreadCount)
	}
}
