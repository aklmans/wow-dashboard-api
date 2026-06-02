package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/app"
	authservice "github.com/aklmans/wow-dashboard-api/internal/auth/service"
	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
	"github.com/aklmans/wow-dashboard-api/internal/http/handlers"
	mfaservice "github.com/aklmans/wow-dashboard-api/internal/mfa/service"
)

func TestMfaDisableHandler(t *testing.T) {
	userID := uuid.New()
	enabledUser := func() *authservice.PublicUser {
		return &authservice.PublicUser{ID: userID.String(), Email: "demo@example.com", MfaEnabled: true}
	}

	t.Run("disables MFA with password + code", func(t *testing.T) {
		auth := &fakeMfaAuth{user: enabledUser()}
		svc := &fakeMfaSvc{}
		router := newMfaTestRouter(auth, svc)

		rec := deleteMfa(router, "Bearer access-token", `{"password":"@Password","code":"123456"}`)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var body struct {
			MfaEnabled bool `json:"mfaEnabled"`
		}
		decodeJSON(t, rec, &body)
		if body.MfaEnabled {
			t.Fatal("response mfaEnabled = true, want false")
		}
		if auth.verifiedPwd != "@Password" {
			t.Errorf("VerifyPassword password = %q, want @Password", auth.verifiedPwd)
		}
		if svc.disableCalls != 1 || svc.disableCode != "123456" || svc.disableUser != userID {
			t.Errorf("Disable(calls=%d, code=%q, user=%s), want 1, 123456, %s",
				svc.disableCalls, svc.disableCode, svc.disableUser, userID)
		}
	})

	t.Run("rejects when MFA is not enabled", func(t *testing.T) {
		auth := &fakeMfaAuth{user: &authservice.PublicUser{ID: userID.String(), MfaEnabled: false}}
		svc := &fakeMfaSvc{}
		router := newMfaTestRouter(auth, svc)

		rec := deleteMfa(router, "Bearer access-token", `{"password":"@Password","code":"123456"}`)

		assertAPIError(t, rec, http.StatusConflict, apierror.CodeConflict, "MFA is not enabled.")
		if svc.disableCalls != 0 {
			t.Fatal("Disable was called for a non-enabled account")
		}
	})

	t.Run("a wrong password is rejected and Disable is never reached", func(t *testing.T) {
		auth := &fakeMfaAuth{user: enabledUser(), verifyErr: authservice.ErrInvalidCredentials}
		svc := &fakeMfaSvc{}
		router := newMfaTestRouter(auth, svc)

		rec := deleteMfa(router, "Bearer access-token", `{"password":"wrong","code":"123456"}`)

		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized, "Invalid email or password.")
		if svc.disableCalls != 0 {
			t.Fatal("Disable was called despite a wrong password")
		}
	})

	t.Run("an invalid code surfaces a validation error", func(t *testing.T) {
		auth := &fakeMfaAuth{user: enabledUser()}
		svc := &fakeMfaSvc{disableErr: mfaservice.ErrInvalidCode}
		router := newMfaTestRouter(auth, svc)

		rec := deleteMfa(router, "Bearer access-token", `{"password":"@Password","code":"000000"}`)

		assertAPIError(t, rec, http.StatusUnprocessableEntity, apierror.CodeValidationFailed,
			"That code is not valid. Try the current code from your authenticator app.")
	})

	t.Run("blocked while impersonating", func(t *testing.T) {
		auth := &fakeMfaAuth{user: &authservice.PublicUser{
			ID: userID.String(), MfaEnabled: true, ImpersonatorID: uuid.New().String(),
		}}
		svc := &fakeMfaSvc{}
		router := newMfaTestRouter(auth, svc)

		rec := deleteMfa(router, "Bearer access-token", `{"password":"@Password","code":"123456"}`)

		assertAPIError(t, rec, http.StatusForbidden, apierror.CodeForbidden,
			"MFA cannot be managed while impersonating a user.")
		if svc.disableCalls != 0 {
			t.Fatal("Disable was called during impersonation")
		}
	})

	t.Run("missing bearer token is unauthorized", func(t *testing.T) {
		router := newMfaTestRouter(&fakeMfaAuth{}, &fakeMfaSvc{})

		rec := deleteMfa(router, "", `{"password":"@Password","code":"123456"}`)

		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
			"Authorization token missing or invalid.")
	})
}

// --- helpers + fakes ---

func newMfaTestRouter(auth handlers.MfaAuthenticator, svc handlers.MfaService) chi.Router {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	api := app.NewAPI(router, true)
	handlers.RegisterMfa(api, auth, svc)
	return router
}

func deleteMfa(router http.Handler, authHeader, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, "/api/auth/mfa", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

type fakeMfaAuth struct {
	user        *authservice.PublicUser
	currentErr  error
	verifyErr   error
	verifiedPwd string
}

func (f *fakeMfaAuth) CurrentUser(_ context.Context, _ string) (*authservice.PublicUser, error) {
	if f.currentErr != nil {
		return nil, f.currentErr
	}
	return f.user, nil
}

func (f *fakeMfaAuth) VerifyPassword(_ context.Context, _ uuid.UUID, rawPassword string) error {
	f.verifiedPwd = rawPassword
	return f.verifyErr
}

type fakeMfaSvc struct {
	setupResult  mfaservice.SetupResult
	setupErr     error
	confirmCodes []string
	confirmErr   error

	disableUser  uuid.UUID
	disableCode  string
	disableCalls int
	disableErr   error
}

func (f *fakeMfaSvc) Setup(_ context.Context, _ uuid.UUID, _ string) (mfaservice.SetupResult, error) {
	return f.setupResult, f.setupErr
}

func (f *fakeMfaSvc) Confirm(_ context.Context, _ uuid.UUID, _ string) ([]string, error) {
	return f.confirmCodes, f.confirmErr
}

func (f *fakeMfaSvc) Disable(_ context.Context, userID uuid.UUID, code string) error {
	f.disableCalls++
	f.disableUser = userID
	f.disableCode = code
	return f.disableErr
}
