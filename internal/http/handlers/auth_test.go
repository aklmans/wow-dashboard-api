package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/aklmans/wow-dashboard-api/internal/app"
	"github.com/aklmans/wow-dashboard-api/internal/auth/service"
	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
	"github.com/aklmans/wow-dashboard-api/internal/http/handlers"
	httpmiddleware "github.com/aklmans/wow-dashboard-api/internal/http/middleware"
)

func TestAuthHandlers(t *testing.T) {
	t.Run("sign-up success returns starter session response", func(t *testing.T) {
		authSvc := &fakeAuthService{
			signUpSession: testSession(),
		}
		router := newAuthTestRouter(authSvc)

		rec := postJSON(router, "/api/auth/sign-up", map[string]string{
			"email":     "hello@gmail.com",
			"password":  "@2Minimal",
			"firstName": "Hello",
			"lastName":  "Friend",
		})

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
		}

		var body authSessionResponse
		decodeJSON(t, rec, &body)
		assertStarterUser(t, body.User)
		if body.AccessToken != "access-token-123" {
			t.Errorf("accessToken = %q, want access-token-123", body.AccessToken)
		}
		if authSvc.signUpInput.Email != "hello@gmail.com" {
			t.Errorf("SignUp email = %q, want hello@gmail.com", authSvc.signUpInput.Email)
		}
		if authSvc.signUpInput.Password != "@2Minimal" {
			t.Errorf("SignUp password was not forwarded")
		}
		assertRefreshCookie(t, rec, "refresh-token-123")
	})

	t.Run("sign-in success returns starter session response", func(t *testing.T) {
		authSvc := &fakeAuthService{
			signInSession: testSession(),
		}
		router := newAuthTestRouter(authSvc)

		rec := postJSON(router, "/api/auth/sign-in", map[string]string{
			"email":    "demo@minimals.cc",
			"password": "@2Minimal",
		})

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}

		var body authSessionResponse
		decodeJSON(t, rec, &body)
		assertStarterUser(t, body.User)
		if body.AccessToken != "access-token-123" {
			t.Errorf("accessToken = %q, want access-token-123", body.AccessToken)
		}
		if authSvc.signInInput.Email != "demo@minimals.cc" {
			t.Errorf("SignIn email = %q, want demo@minimals.cc", authSvc.signInInput.Email)
		}
		assertRefreshCookie(t, rec, "refresh-token-123")
	})

	t.Run("refresh missing cookie returns safe unauthorized envelope", func(t *testing.T) {
		router := newAuthTestRouter(&fakeAuthService{})

		rec := postNoBody(router, "/api/auth/refresh")

		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
			"Authorization token missing or invalid.")
	})

	t.Run("refresh success returns session and rotates refresh cookie", func(t *testing.T) {
		authSvc := &fakeAuthService{
			refreshSession: &service.Session{
				User: service.PublicUser{
					ID:          "user-123",
					Email:       "demo@minimals.cc",
					DisplayName: "Demo User",
					Status:      "active",
				},
				AccessToken:  "new-access-token",
				RefreshToken: "new-refresh-token",
			},
		}
		router := newAuthTestRouter(authSvc)

		rec := postNoBodyWithCookie(router, "/api/auth/refresh", "wow_dashboard_refresh_token", "old-refresh-token")

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if authSvc.refreshToken != "old-refresh-token" {
			t.Fatalf("Refresh token = %q, want old-refresh-token", authSvc.refreshToken)
		}
		var body authSessionResponse
		decodeJSON(t, rec, &body)
		if body.AccessToken != "new-access-token" {
			t.Fatalf("accessToken = %q, want new-access-token", body.AccessToken)
		}
		assertStarterUser(t, body.User)
		assertRefreshCookie(t, rec, "new-refresh-token")
	})

	t.Run("sign-out clears refresh cookie", func(t *testing.T) {
		authSvc := &fakeAuthService{}
		router := newAuthTestRouter(authSvc)

		rec := postNoBodyWithCookie(router, "/api/auth/sign-out", "wow_dashboard_refresh_token", "old-refresh-token")

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if authSvc.signOutToken != "old-refresh-token" {
			t.Fatalf("SignOut token = %q, want old-refresh-token", authSvc.signOutToken)
		}
		assertClearedRefreshCookie(t, rec)
	})

	t.Run("sign-out missing cookie is idempotent and clears refresh cookie", func(t *testing.T) {
		authSvc := &fakeAuthService{}
		router := newAuthTestRouter(authSvc)

		rec := postNoBody(router, "/api/auth/sign-out")

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if authSvc.signOutToken != "" {
			t.Fatalf("SignOut token = %q, want empty for missing cookie", authSvc.signOutToken)
		}
		assertClearedRefreshCookie(t, rec)
	})

	t.Run("sign-out revoke failure returns internal error and keeps cookie untouched", func(t *testing.T) {
		authSvc := &fakeAuthService{signOutErr: errors.New("database unavailable")}
		router := newAuthTestRouter(authSvc)

		rec := postNoBodyWithCookie(router, "/api/auth/sign-out", "wow_dashboard_refresh_token", "old-refresh-token")

		assertAPIError(t, rec, http.StatusInternalServerError, apierror.CodeInternalError,
			"An internal error occurred. Please try again later.", "old-refresh-token", "database unavailable")
		if authSvc.signOutToken != "old-refresh-token" {
			t.Fatalf("SignOut token = %q, want old-refresh-token", authSvc.signOutToken)
		}
		if got := rec.Result().Header.Values("Set-Cookie"); len(got) != 0 {
			t.Fatalf("Set-Cookie headers = %v, want none on revoke failure", got)
		}
	})

	t.Run("me success returns starter user response and forwards raw token", func(t *testing.T) {
		authSvc := &fakeAuthService{
			currentUser: &testSession().User,
		}
		router := newAuthTestRouter(authSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
		req.Header.Set("Authorization", "  Bearer   raw-token-123  ")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}

		var body authMeResponse
		decodeJSON(t, rec, &body)
		assertStarterUser(t, body.User)
		if authSvc.currentUserToken != "raw-token-123" {
			t.Errorf("CurrentUser token = %q, want raw-token-123", authSvc.currentUserToken)
		}
	})

	t.Run("sign-in invalid credentials returns safe unauthorized envelope", func(t *testing.T) {
		authSvc := &fakeAuthService{
			signInErr: service.ErrInvalidCredentials,
		}
		router := newAuthTestRouter(authSvc)

		rec := postJSON(router, "/api/auth/sign-in", map[string]string{
			"email":    "demo@minimals.cc",
			"password": "@2Minimal",
		})

		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized, "Invalid email or password.",
			"demo@minimals.cc", "@2Minimal")
	})

	t.Run("sign-up duplicate email returns safe conflict envelope", func(t *testing.T) {
		authSvc := &fakeAuthService{
			signUpErr: service.ErrEmailAlreadyExists,
		}
		router := newAuthTestRouter(authSvc)

		rec := postJSON(router, "/api/auth/sign-up", map[string]string{
			"email":     "hello@gmail.com",
			"password":  "@2Minimal",
			"firstName": "Hello",
			"lastName":  "Friend",
		})

		assertAPIError(t, rec, http.StatusConflict, apierror.CodeConflict,
			"There already exists an account with the given email address.",
			"hello@gmail.com", "duplicate key", "sqlstate", "users_email")
	})

	t.Run("me missing authorization returns safe unauthorized envelope", func(t *testing.T) {
		router := newAuthTestRouter(&fakeAuthService{})

		req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
			"Authorization token missing or invalid.")
	})

	t.Run("me malformed authorization returns safe unauthorized envelope", func(t *testing.T) {
		router := newAuthTestRouter(&fakeAuthService{})

		req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
		req.Header.Set("Authorization", "Basic abc123")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
			"Authorization token missing or invalid.", "Basic abc123")
	})

	t.Run("me invalid token returns safe unauthorized envelope", func(t *testing.T) {
		const rawToken = "raw.invalid.token"
		authSvc := &fakeAuthService{
			currentUserErr: service.ErrInvalidToken,
		}
		router := newAuthTestRouter(authSvc)

		req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
			"Authorization token missing or invalid.", rawToken)
	})

	t.Run("service unknown error returns generic internal envelope", func(t *testing.T) {
		authSvc := &fakeAuthService{
			signInErr: errors.New("database exploded with password_hash and SQLSTATE 23505"),
		}
		router := newAuthTestRouter(authSvc)

		rec := postJSON(router, "/api/auth/sign-in", map[string]string{
			"email":    "demo@minimals.cc",
			"password": "@2Minimal",
		})

		assertAPIError(t, rec, http.StatusInternalServerError, apierror.CodeInternalError,
			"An internal error occurred. Please try again later.",
			"password_hash", "SQLSTATE", "database exploded", "demo@minimals.cc", "@2Minimal")
	})
}

func TestAuthRateLimitMiddlewareAppliesToSignInAndSignUpOnly(t *testing.T) {
	authSvc := &fakeAuthService{
		signUpSession: testSession(),
		signInSession: testSession(),
		currentUser:   &testSession().User,
	}
	router := newProjectAuthTestRouterWithMiddlewares(authSvc, func(ctx huma.Context, next func(huma.Context)) {
		err := apierror.RateLimited("Too many authentication attempts. Please try again later.").
			WithRequestID(apierror.RequestIDFromContext(ctx.Context()))
		ctx.SetStatus(http.StatusTooManyRequests)
		ctx.SetHeader("Content-Type", "application/json")
		if encodeErr := json.NewEncoder(ctx.BodyWriter()).Encode(err.Body()); encodeErr != nil {
			t.Fatalf("failed to encode rate limit response: %v", encodeErr)
		}
	})

	signUpRec := postJSON(router, "/api/auth/sign-up", map[string]string{
		"email":     "hello@gmail.com",
		"password":  "@2Minimal",
		"firstName": "Hello",
		"lastName":  "Friend",
	})
	assertAPIError(t, signUpRec, http.StatusTooManyRequests, apierror.CodeRateLimited,
		"Too many authentication attempts. Please try again later.")

	signInRec := postJSON(router, "/api/auth/sign-in", map[string]string{
		"email":    "demo@minimals.cc",
		"password": "@2Minimal",
	})
	assertAPIError(t, signInRec, http.StatusTooManyRequests, apierror.CodeRateLimited,
		"Too many authentication attempts. Please try again later.")

	meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meReq.Header.Set("Authorization", "Bearer access-token-123")
	meRec := httptest.NewRecorder()
	router.ServeHTTP(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("me status = %d, want %d; body=%s", meRec.Code, http.StatusOK, meRec.Body.String())
	}
}

func TestAuthRateLimitIgnoresForwardedHeaders(t *testing.T) {
	authSvc := &fakeAuthService{
		signInSession: testSession(),
	}
	limiter := httpmiddleware.NewIPRateLimiter(httpmiddleware.RateLimitConfig{
		Enabled:  true,
		Requests: 1,
		Window:   time.Minute,
		Burst:    1,
	})
	router := newProjectAuthTestRouterWithMiddlewares(authSvc, httpmiddleware.AuthRateLimit(limiter))

	first := postJSONFromRemote(router, "/api/auth/sign-in", map[string]string{
		"email":    "demo@minimals.cc",
		"password": "@2Minimal",
	}, "198.51.100.10:1111", map[string]string{
		"X-Forwarded-For": "203.0.113.1",
		"X-Real-IP":       "203.0.113.2",
	})
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d; body=%s", first.Code, http.StatusOK, first.Body.String())
	}

	second := postJSONFromRemote(router, "/api/auth/sign-in", map[string]string{
		"email":    "demo@minimals.cc",
		"password": "@2Minimal",
	}, "198.51.100.10:2222", map[string]string{
		"X-Forwarded-For": "203.0.113.99",
		"X-Real-IP":       "203.0.113.100",
	})
	assertAPIError(t, second, http.StatusTooManyRequests, apierror.CodeRateLimited,
		"Too many authentication attempts. Please try again later.")
}

func TestAuthOpenAPIErrorResponsesUseAPIErrorEnvelope(t *testing.T) {
	router := chi.NewRouter()
	api := humachi.New(router, huma.DefaultConfig("Test API", "1.0.0"))
	handlers.RegisterAuth(api, &fakeAuthService{})

	specJSON, err := json.Marshal(api.OpenAPI())
	if err != nil {
		t.Fatalf("failed to marshal OpenAPI spec: %v", err)
	}

	var spec map[string]any
	if err := json.Unmarshal(specJSON, &spec); err != nil {
		t.Fatalf("failed to unmarshal OpenAPI spec: %v", err)
	}

	for _, endpoint := range []struct {
		path     string
		method   string
		statuses []string
	}{
		{path: "/api/auth/sign-up", method: "post", statuses: []string{"400", "409", "422", "429", "500"}},
		{path: "/api/auth/sign-in", method: "post", statuses: []string{"400", "401", "403", "422", "429", "500"}},
		{path: "/api/auth/refresh", method: "post", statuses: []string{"401", "403", "500"}},
		{path: "/api/auth/sign-out", method: "post", statuses: []string{"500"}},
		{path: "/api/auth/me", method: "get", statuses: []string{"401", "403", "500"}},
	} {
		t.Run(endpoint.method+" "+endpoint.path, func(t *testing.T) {
			operation := objectAt(t, spec, "paths", endpoint.path, endpoint.method)
			responses := objectAt(t, operation, "responses")

			if _, ok := responses["default"]; ok {
				t.Fatal("auth operation exposes default Huma ErrorModel response")
			}

			for _, status := range endpoint.statuses {
				response := dereferenceResponse(t, spec, objectAt(t, responses, status))
				content := objectAt(t, response, "content")
				if _, ok := content["application/problem+json"]; ok {
					t.Fatalf("status %s exposes application/problem+json", status)
				}

				mediaType := objectAt(t, content, "application/json")
				schema := objectAt(t, mediaType, "schema")
				if ref, _ := schema["$ref"].(string); ref == "#/components/schemas/ErrorModel" {
					t.Fatalf("status %s references Huma ErrorModel", status)
				}

				schema = dereferenceSchema(t, spec, schema)
				properties := objectAt(t, schema, "properties")
				for _, field := range []string{"code", "message", "request_id"} {
					if _, ok := properties[field]; !ok {
						t.Fatalf("status %s schema missing %q property", status, field)
					}
				}

				required := stringSliceAt(t, schema, "required")
				for _, field := range []string{"code", "message", "request_id"} {
					if !containsString(required, field) {
						t.Fatalf("status %s schema does not require %q; required=%v", status, field, required)
					}
				}
			}
		})
	}
}

func TestAuthPreHandlerErrorsUseAPIErrorEnvelope(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		body           string
		wantStatus     int
		wantCode       apierror.Code
		forbiddenLeaks []string
	}{
		{
			name:       "sign-in empty body",
			path:       "/api/auth/sign-in",
			body:       "",
			wantStatus: http.StatusBadRequest,
			wantCode:   apierror.CodeBadRequest,
		},
		{
			name:       "sign-in malformed JSON",
			path:       "/api/auth/sign-in",
			body:       `{"email":"demo@minimals.cc","password":"@2Minimal"`,
			wantStatus: http.StatusBadRequest,
			wantCode:   apierror.CodeBadRequest,
			forbiddenLeaks: []string{
				"demo@minimals.cc",
				"@2Minimal",
			},
		},
		{
			name:       "sign-up missing required fields",
			path:       "/api/auth/sign-up",
			body:       `{"email":"hello@gmail.com","password":"@2Minimal"}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierror.CodeValidationFailed,
			forbiddenLeaks: []string{
				"hello@gmail.com",
				"@2Minimal",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newProjectAuthTestRouter(&fakeAuthService{})

			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assertAPIError(t, rec, tt.wantStatus, tt.wantCode, "Invalid request.", tt.forbiddenLeaks...)

			contentType := rec.Header().Get("Content-Type")
			if !strings.HasPrefix(contentType, "application/json") {
				t.Fatalf("Content-Type = %q, want application/json", contentType)
			}

			var raw map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
				t.Fatalf("failed to decode response body %q: %v", rec.Body.String(), err)
			}
			for _, field := range []string{"title", "detail", "errors", "status", "type", "instance"} {
				if _, ok := raw[field]; ok {
					t.Fatalf("response contains Huma problem field %q: %s", field, rec.Body.String())
				}
			}
			if rawDetails, ok := raw["details"]; ok {
				details, ok := rawDetails.([]any)
				if !ok {
					t.Fatalf("details is %T, want array", rawDetails)
				}
				for _, rawDetail := range details {
					detail, ok := rawDetail.(map[string]any)
					if !ok {
						t.Fatalf("detail item is %T, want object", rawDetail)
					}
					if _, ok := detail["message"].(string); !ok {
						t.Fatalf("detail item missing string message: %#v", detail)
					}
					if rawField, ok := detail["field"]; ok {
						if _, ok := rawField.(string); !ok {
							t.Fatalf("detail item field is %T, want string: %#v", rawField, detail)
						}
					}
					if _, ok := detail["location"]; ok {
						t.Fatalf("detail item contains Huma location field: %#v", detail)
					}
					if _, ok := detail["value"]; ok {
						t.Fatalf("detail item contains Huma value field: %#v", detail)
					}
				}
			}
		})
	}
}

type fakeAuthService struct {
	signUpInput   service.SignUpInput
	signUpSession *service.Session
	signUpErr     error

	signInInput   service.SignInInput
	signInSession *service.Session
	signInErr     error

	currentUserToken string
	currentUser      *service.PublicUser
	currentUserErr   error

	changePasswordToken   string
	changePasswordCurrent string
	changePasswordNew     string
	changePasswordErr     error

	updateMyProfileToken  string
	updateMyProfileInput  service.UpdateMyProfileInput
	updateMyProfileResult *service.PublicUser
	updateMyProfileErr    error

	forgotPasswordEmail string
	forgotPasswordErr   error

	resetPasswordToken string
	resetPasswordNew   string
	resetPasswordErr   error

	verifyEmailToken string
	verifyEmailErr   error

	resendVerificationToken string
	resendVerificationErr   error

	refreshToken   string
	refreshSession *service.Session
	refreshErr     error

	signOutToken string
	signOutErr   error
}

func (f *fakeAuthService) SignUp(ctx context.Context, input service.SignUpInput) (*service.Session, error) {
	f.signUpInput = input
	if f.signUpErr != nil {
		return nil, f.signUpErr
	}
	return f.signUpSession, nil
}

func (f *fakeAuthService) SignIn(ctx context.Context, input service.SignInInput) (*service.Session, error) {
	f.signInInput = input
	if f.signInErr != nil {
		return nil, f.signInErr
	}
	return f.signInSession, nil
}

func (f *fakeAuthService) CurrentUser(ctx context.Context, rawAccessToken string) (*service.PublicUser, error) {
	f.currentUserToken = rawAccessToken
	if f.currentUserErr != nil {
		return nil, f.currentUserErr
	}
	return f.currentUser, nil
}

func (f *fakeAuthService) ChangePassword(ctx context.Context, rawAccessToken string, currentPassword string, newPassword string) error {
	f.changePasswordToken = rawAccessToken
	f.changePasswordCurrent = currentPassword
	f.changePasswordNew = newPassword
	return f.changePasswordErr
}

func (f *fakeAuthService) UpdateMyProfile(ctx context.Context, rawAccessToken string, input service.UpdateMyProfileInput) (*service.PublicUser, error) {
	f.updateMyProfileToken = rawAccessToken
	f.updateMyProfileInput = input
	if f.updateMyProfileErr != nil {
		return nil, f.updateMyProfileErr
	}
	if f.updateMyProfileResult != nil {
		return f.updateMyProfileResult, nil
	}
	return &service.PublicUser{ID: "stub-id", Email: "stub@example.com"}, nil
}

func (f *fakeAuthService) ForgotPassword(ctx context.Context, email string) error {
	f.forgotPasswordEmail = email
	return f.forgotPasswordErr
}

func (f *fakeAuthService) ResetPassword(ctx context.Context, rawToken string, newPassword string) error {
	f.resetPasswordToken = rawToken
	f.resetPasswordNew = newPassword
	return f.resetPasswordErr
}

func (f *fakeAuthService) VerifyEmail(ctx context.Context, rawToken string) error {
	f.verifyEmailToken = rawToken
	return f.verifyEmailErr
}

func (f *fakeAuthService) ResendVerification(ctx context.Context, rawAccessToken string) error {
	f.resendVerificationToken = rawAccessToken
	return f.resendVerificationErr
}

func (f *fakeAuthService) Refresh(ctx context.Context, rawRefreshToken string) (*service.Session, error) {
	f.refreshToken = rawRefreshToken
	if f.refreshErr != nil {
		return nil, f.refreshErr
	}
	return f.refreshSession, nil
}

func (f *fakeAuthService) SignOut(ctx context.Context, rawRefreshToken string) error {
	f.signOutToken = rawRefreshToken
	return f.signOutErr
}

func newAuthTestRouter(authSvc handlers.AuthService) chi.Router {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	api := humachi.New(router, huma.DefaultConfig("Test API", "1.0.0"))
	handlers.RegisterAuth(api, authSvc)
	return router
}

func newProjectAuthTestRouter(authSvc handlers.AuthService) chi.Router {
	return newProjectAuthTestRouterWithMiddlewares(authSvc)
}

func newProjectAuthTestRouterWithMiddlewares(authSvc handlers.AuthService, authMiddlewares ...func(huma.Context, func(huma.Context))) chi.Router {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	api := app.NewAPI(router)
	handlers.RegisterAuth(api, authSvc, authMiddlewares...)
	return router
}

func postJSON(router http.Handler, path string, body any) *httptest.ResponseRecorder {
	data, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func postJSONFromRemote(router http.Handler, path string, body any, remoteAddr string, headers map[string]string) *httptest.ResponseRecorder {
	data, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	req.RemoteAddr = remoteAddr
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func postNoBody(router http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func postNoBodyWithCookie(router http.Handler, path string, name string, value string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.AddCookie(&http.Cookie{Name: name, Value: value})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(dst); err != nil {
		t.Fatalf("failed to decode response body %q: %v", rec.Body.String(), err)
	}
}

func assertAPIError(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode apierror.Code, wantMessage string, forbidden ...string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, wantStatus, rec.Body.String())
	}

	raw := rec.Body.String()
	for _, secret := range forbidden {
		if secret != "" && strings.Contains(strings.ToLower(raw), strings.ToLower(secret)) {
			t.Errorf("error response leaks %q: %s", secret, raw)
		}
	}

	var body apierror.ResponseBody
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("failed to decode error body %q: %v", raw, err)
	}
	if body.Code != wantCode {
		t.Errorf("code = %q, want %q", body.Code, wantCode)
	}
	if body.Message != wantMessage {
		t.Errorf("message = %q, want %q", body.Message, wantMessage)
	}
	if body.RequestID == "" {
		t.Error("request_id is empty")
	}
}

type authSessionResponse struct {
	User        starterUser `json:"user"`
	AccessToken string      `json:"accessToken"`
}

type authMeResponse struct {
	User starterUser `json:"user"`
}

type starterUser struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
}

func testSession() *service.Session {
	return &service.Session{
		User: service.PublicUser{
			ID:          "user-123",
			Email:       "demo@minimals.cc",
			DisplayName: "Demo User",
			Status:      "active",
		},
		AccessToken:  "access-token-123",
		RefreshToken: "refresh-token-123",
	}
}

func assertRefreshCookie(t *testing.T, rec *httptest.ResponseRecorder, wantValue string) {
	t.Helper()
	cookie := cookieByName(t, rec, "wow_dashboard_refresh_token")
	if cookie.Value != wantValue {
		t.Fatalf("refresh cookie value = %q, want %q", cookie.Value, wantValue)
	}
	if !cookie.HttpOnly {
		t.Fatal("refresh cookie is not HttpOnly")
	}
	if cookie.Path != "/api/auth" {
		t.Fatalf("refresh cookie path = %q, want /api/auth", cookie.Path)
	}
	raw := strings.Join(rec.Result().Header.Values("Set-Cookie"), "\n")
	for _, want := range []string{"HttpOnly", "SameSite=Lax", "Path=/api/auth"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("Set-Cookie %q missing %q", raw, want)
		}
	}
}

func assertClearedRefreshCookie(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	cookie := cookieByName(t, rec, "wow_dashboard_refresh_token")
	if cookie.Value != "" {
		t.Fatalf("cleared refresh cookie value = %q, want empty", cookie.Value)
	}
	if cookie.MaxAge >= 0 {
		t.Fatalf("cleared refresh cookie MaxAge = %d, want negative", cookie.MaxAge)
	}
	if !cookie.HttpOnly {
		t.Fatal("cleared refresh cookie is not HttpOnly")
	}
}

func cookieByName(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("missing cookie %q; Set-Cookie=%v", name, rec.Result().Header.Values("Set-Cookie"))
	return nil
}

func assertStarterUser(t *testing.T, user starterUser) {
	t.Helper()
	if user.ID != "user-123" {
		t.Errorf("user.id = %q, want user-123", user.ID)
	}
	if user.Email != "demo@minimals.cc" {
		t.Errorf("user.email = %q, want demo@minimals.cc", user.Email)
	}
	if user.DisplayName != "Demo User" {
		t.Errorf("user.displayName = %q, want Demo User", user.DisplayName)
	}
}

func objectAt(t *testing.T, root map[string]any, path ...string) map[string]any {
	t.Helper()

	current := root
	for _, key := range path {
		next, ok := current[key]
		if !ok {
			t.Fatalf("missing OpenAPI key %q in path %v", key, path)
		}
		nextObject, ok := next.(map[string]any)
		if !ok {
			t.Fatalf("OpenAPI key %q in path %v is %T, want object", key, path, next)
		}
		current = nextObject
	}
	return current
}

func stringSliceAt(t *testing.T, root map[string]any, key string) []string {
	t.Helper()

	raw, ok := root[key]
	if !ok {
		t.Fatalf("missing OpenAPI key %q", key)
	}
	rawSlice, ok := raw.([]any)
	if !ok {
		t.Fatalf("OpenAPI key %q is %T, want array", key, raw)
	}

	result := make([]string, 0, len(rawSlice))
	for _, item := range rawSlice {
		value, ok := item.(string)
		if !ok {
			t.Fatalf("OpenAPI key %q contains %T, want string", key, item)
		}
		result = append(result, value)
	}
	return result
}

func dereferenceSchema(t *testing.T, spec map[string]any, schema map[string]any) map[string]any {
	t.Helper()

	ref, _ := schema["$ref"].(string)
	if ref == "" {
		return schema
	}

	const prefix = "#/components/schemas/"
	if !strings.HasPrefix(ref, prefix) {
		t.Fatalf("unsupported schema ref %q", ref)
	}
	return objectAt(t, spec, "components", "schemas", strings.TrimPrefix(ref, prefix))
}

func dereferenceResponse(t *testing.T, spec map[string]any, response map[string]any) map[string]any {
	t.Helper()

	ref, _ := response["$ref"].(string)
	if ref == "" {
		return response
	}

	const prefix = "#/components/responses/"
	if !strings.HasPrefix(ref, prefix) {
		t.Fatalf("unsupported response ref %q", ref)
	}
	return objectAt(t, spec, "components", "responses", strings.TrimPrefix(ref, prefix))
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestChangePasswordHandler(t *testing.T) {
	const body = `{"currentPassword":"old-password","newPassword":"new-password-123"}`

	t.Run("success returns 200 and forwards the input", func(t *testing.T) {
		authSvc := &fakeAuthService{}
		router := newAuthTestRouter(authSvc)

		req := httptest.NewRequest(http.MethodPost, "/api/auth/change-password", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer access-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if authSvc.changePasswordToken != "access-token" {
			t.Fatalf("forwarded token = %q, want access-token", authSvc.changePasswordToken)
		}
		if authSvc.changePasswordCurrent != "old-password" || authSvc.changePasswordNew != "new-password-123" {
			t.Fatalf("forwarded passwords = %q / %q", authSvc.changePasswordCurrent, authSvc.changePasswordNew)
		}
	})

	t.Run("missing authorization returns 401", func(t *testing.T) {
		router := newAuthTestRouter(&fakeAuthService{})
		req := httptest.NewRequest(http.MethodPost, "/api/auth/change-password", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
			"Authorization token missing or invalid.")
	})

	t.Run("wrong current password maps to 401", func(t *testing.T) {
		authSvc := &fakeAuthService{changePasswordErr: service.ErrInvalidCredentials}
		router := newAuthTestRouter(authSvc)
		req := httptest.NewRequest(http.MethodPost, "/api/auth/change-password", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer access-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized, "Invalid email or password.")
	})
}

func TestEmailAuthFlowHandlers(t *testing.T) {
	post := func(t *testing.T, authSvc *fakeAuthService, path, requestBody, bearer string) *httptest.ResponseRecorder {
		t.Helper()
		router := newAuthTestRouter(authSvc)
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(requestBody))
		req.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	t.Run("forgot-password forwards the email and returns 200", func(t *testing.T) {
		authSvc := &fakeAuthService{}
		rec := post(t, authSvc, "/api/auth/forgot-password", `{"email":"demo@minimals.cc"}`, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if authSvc.forgotPasswordEmail != "demo@minimals.cc" {
			t.Fatalf("forwarded email = %q", authSvc.forgotPasswordEmail)
		}
	})

	t.Run("reset-password forwards the token and password", func(t *testing.T) {
		authSvc := &fakeAuthService{}
		rec := post(t, authSvc, "/api/auth/reset-password", `{"token":"tok","newPassword":"new-password-123"}`, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if authSvc.resetPasswordToken != "tok" || authSvc.resetPasswordNew != "new-password-123" {
			t.Fatalf("forwarded = %q / %q", authSvc.resetPasswordToken, authSvc.resetPasswordNew)
		}
	})

	t.Run("reset-password maps an invalid token to 401", func(t *testing.T) {
		authSvc := &fakeAuthService{resetPasswordErr: service.ErrInvalidToken}
		rec := post(t, authSvc, "/api/auth/reset-password", `{"token":"bad","newPassword":"new-password-123"}`, "")
		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
			"Authorization token missing or invalid.")
	})

	t.Run("verify-email forwards the token", func(t *testing.T) {
		authSvc := &fakeAuthService{}
		rec := post(t, authSvc, "/api/auth/verify-email", `{"token":"vtok"}`, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if authSvc.verifyEmailToken != "vtok" {
			t.Fatalf("forwarded token = %q", authSvc.verifyEmailToken)
		}
	})

	t.Run("resend-verification requires authorization", func(t *testing.T) {
		rec := post(t, &fakeAuthService{}, "/api/auth/resend-verification", "", "")
		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
			"Authorization token missing or invalid.")
	})

	t.Run("resend-verification forwards the access token", func(t *testing.T) {
		authSvc := &fakeAuthService{}
		rec := post(t, authSvc, "/api/auth/resend-verification", "", "access-token")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if authSvc.resendVerificationToken != "access-token" {
			t.Fatalf("forwarded token = %q", authSvc.resendVerificationToken)
		}
	})
}
