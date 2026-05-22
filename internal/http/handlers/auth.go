package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/aklmans/wow-dashboard-api/internal/auth/service"
	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
)

// AuthService is the auth use-case surface required by the HTTP handlers.
type AuthService interface {
	SignUp(ctx context.Context, input service.SignUpInput) (*service.Session, error)
	SignIn(ctx context.Context, input service.SignInInput) (*service.Session, error)
	Refresh(ctx context.Context, rawRefreshToken string) (*service.Session, error)
	SignOut(ctx context.Context, rawRefreshToken string) error
	CurrentUser(ctx context.Context, rawAccessToken string) (*service.PublicUser, error)
}

type authUser struct {
	ID          string `json:"id" example:"c8a89c0b-8e75-4e61-9fa0-70fb83554e66" doc:"User identifier"`
	Email       string `json:"email" example:"demo@minimals.cc" doc:"User email address"`
	DisplayName string `json:"displayName" example:"Demo User" doc:"User display name"`
}

// authMeUser extends the basic profile with the resolved roles and effective
// permissions a frontend uses to render menus and gate actions.
type authMeUser struct {
	ID          string   `json:"id" example:"c8a89c0b-8e75-4e61-9fa0-70fb83554e66" doc:"User identifier"`
	Email       string   `json:"email" example:"demo@minimals.cc" doc:"User email address"`
	DisplayName string   `json:"displayName" example:"Demo User" doc:"User display name"`
	Roles       []string `json:"roles" nullable:"false" doc:"Names of the roles assigned to the user"`
	Permissions []string `json:"permissions" nullable:"false" doc:"Effective permission strings granted by the user's roles"`
}

type authSessionBody struct {
	User        authUser `json:"user" doc:"Authenticated user profile"`
	AccessToken string   `json:"accessToken" doc:"JWT access token"`
}

type authSessionResponse struct {
	SetCookie []http.Cookie `header:"Set-Cookie"`
	Body      authSessionBody
}

type authSuccessBody struct {
	Success bool `json:"success" example:"true" doc:"Operation success flag"`
}

type authSuccessResponse struct {
	SetCookie []http.Cookie `header:"Set-Cookie"`
	Body      authSuccessBody
}

type authMeBody struct {
	User authMeUser `json:"user" doc:"Authenticated user profile with roles and permissions"`
}

type authMeResponse struct {
	Body authMeBody
}

type signUpInput struct {
	Body struct {
		Email     string `json:"email" format:"email" maxLength:"320" example:"hello@gmail.com" doc:"Email address"`
		Password  string `json:"password" minLength:"8" maxLength:"4096" example:"@2Minimal" doc:"Account password"`
		FirstName string `json:"firstName" maxLength:"100" example:"Hello" doc:"First name"`
		LastName  string `json:"lastName" maxLength:"100" example:"Friend" doc:"Last name"`
	}
}

type signInInput struct {
	Body struct {
		Email    string `json:"email" maxLength:"320" example:"demo@minimals.cc" doc:"Email address"`
		Password string `json:"password" maxLength:"4096" example:"@2Minimal" doc:"Account password"`
	}
}

type meInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
}

type refreshInput struct {
	Cookie string `header:"Cookie" doc:"Refresh token cookie"`
}

type signOutInput struct {
	Cookie string `header:"Cookie" doc:"Refresh token cookie"`
}

type RefreshCookieConfig struct {
	Name     string
	Path     string
	Secure   bool
	SameSite http.SameSite
	TTL      time.Duration
}

// RegisterAuth registers Starter-compatible JWT auth endpoints.
func RegisterAuth(api huma.API, authSvc AuthService, authMiddlewares ...func(huma.Context, func(huma.Context))) {
	RegisterAuthWithCookies(api, authSvc, DefaultRefreshCookieConfig(), authMiddlewares...)
}

// RegisterAuthWithCookies registers auth endpoints with explicit refresh cookie settings.
func RegisterAuthWithCookies(api huma.API, authSvc AuthService, refreshCookie RefreshCookieConfig, authMiddlewares ...func(huma.Context, func(huma.Context))) {
	refreshCookie = refreshCookie.withDefaults()

	huma.Register(api, huma.Operation{
		OperationID:   "post-auth-sign-up",
		Method:        http.MethodPost,
		Path:          "/api/auth/sign-up",
		Summary:       "Sign up",
		Description:   "Creates a user account, returns an access token, and sets the refresh token cookie.",
		Tags:          []string{"Auth"},
		Middlewares:   huma.Middlewares(authMiddlewares),
		DefaultStatus: http.StatusCreated,
		Responses: apiErrorResponses(api,
			http.StatusBadRequest,
			http.StatusConflict,
			http.StatusUnprocessableEntity,
			http.StatusTooManyRequests,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *signUpInput) (*authSessionResponse, error) {
		session, err := authSvc.SignUp(ctx, service.SignUpInput{
			Email:     input.Body.Email,
			Password:  input.Body.Password,
			FirstName: input.Body.FirstName,
			LastName:  input.Body.LastName,
		})
		if err != nil {
			return nil, mapAuthError(ctx, err)
		}
		return sessionResponse(session, refreshCookie), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "post-auth-sign-in",
		Method:      http.MethodPost,
		Path:        "/api/auth/sign-in",
		Summary:     "Sign in",
		Description: "Authenticates credentials, returns an access token, and sets the refresh token cookie.",
		Tags:        []string{"Auth"},
		Middlewares: huma.Middlewares(authMiddlewares),
		Responses: apiErrorResponses(api,
			http.StatusBadRequest,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusUnprocessableEntity,
			http.StatusTooManyRequests,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *signInInput) (*authSessionResponse, error) {
		session, err := authSvc.SignIn(ctx, service.SignInInput{
			Email:    input.Body.Email,
			Password: input.Body.Password,
		})
		if err != nil {
			return nil, mapAuthError(ctx, err)
		}
		return sessionResponse(session, refreshCookie), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "post-auth-refresh",
		Method:      http.MethodPost,
		Path:        "/api/auth/refresh",
		Summary:     "Refresh session",
		Description: "Rotates the refresh token cookie and returns a new access token.",
		Tags:        []string{"Auth"},
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *refreshInput) (*authSessionResponse, error) {
		rawRefreshToken, ok := parseCookieValue(input.Cookie, refreshCookie.Name)
		if !ok {
			return nil, apierror.Unauthorized("Authorization token missing or invalid.").ForContext(ctx)
		}

		session, err := authSvc.Refresh(ctx, rawRefreshToken)
		if err != nil {
			return nil, mapAuthError(ctx, err)
		}
		return sessionResponse(session, refreshCookie), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "post-auth-sign-out",
		Method:      http.MethodPost,
		Path:        "/api/auth/sign-out",
		Summary:     "Sign out",
		Description: "Revokes the current refresh token cookie when present.",
		Tags:        []string{"Auth"},
		Responses: apiErrorResponses(api,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *signOutInput) (*authSuccessResponse, error) {
		if rawRefreshToken, ok := parseCookieValue(input.Cookie, refreshCookie.Name); ok {
			if err := authSvc.SignOut(ctx, rawRefreshToken); err != nil {
				return nil, mapAuthError(ctx, err)
			}
		}

		return &authSuccessResponse{
			SetCookie: []http.Cookie{clearRefreshCookie(refreshCookie)},
			Body:      authSuccessBody{Success: true},
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-auth-me",
		Method:      http.MethodGet,
		Path:        "/api/auth/me",
		Summary:     "Current user",
		Description: "Returns the profile for the bearer access token.",
		Tags:        []string{"Auth"},
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *meInput) (*authMeResponse, error) {
		rawAccessToken, ok := parseBearerToken(input.Authorization)
		if !ok {
			return nil, apierror.Unauthorized("Authorization token missing or invalid.").ForContext(ctx)
		}

		user, err := authSvc.CurrentUser(ctx, rawAccessToken)
		if err != nil {
			return nil, mapAuthError(ctx, err)
		}
		return &authMeResponse{Body: authMeBody{User: meUserResponse(*user)}}, nil
	})
}

func sessionResponse(session *service.Session, refreshCookie RefreshCookieConfig) *authSessionResponse {
	resp := &authSessionResponse{
		Body: authSessionBody{
			User:        publicUserResponse(session.User),
			AccessToken: session.AccessToken,
		},
	}
	if session.RefreshToken != "" {
		resp.SetCookie = []http.Cookie{newRefreshCookie(refreshCookie, session.RefreshToken)}
	}
	return resp
}

func publicUserResponse(user service.PublicUser) authUser {
	return authUser{
		ID:          user.ID,
		Email:       user.Email,
		DisplayName: user.DisplayName,
	}
}

func meUserResponse(user service.PublicUser) authMeUser {
	roles := user.Roles
	if roles == nil {
		roles = []string{}
	}
	permissions := user.Permissions
	if permissions == nil {
		permissions = []string{}
	}
	return authMeUser{
		ID:          user.ID,
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Roles:       roles,
		Permissions: permissions,
	}
}

func parseBearerToken(header string) (string, bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", false
	}

	fields := strings.Fields(header)
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") || fields[1] == "" {
		return "", false
	}
	return fields[1], true
}

func parseCookieValue(header string, name string) (string, bool) {
	header = strings.TrimSpace(header)
	if header == "" || strings.TrimSpace(name) == "" {
		return "", false
	}
	cookies, err := http.ParseCookie(header)
	if err != nil {
		return "", false
	}
	for _, cookie := range cookies {
		if cookie.Name == name {
			if value := strings.TrimSpace(cookie.Value); value != "" {
				return value, true
			}
			return "", false
		}
	}
	return "", false
}

func DefaultRefreshCookieConfig() RefreshCookieConfig {
	return RefreshCookieConfig{
		Name:     "wow_dashboard_refresh_token",
		Path:     "/api/auth",
		SameSite: http.SameSiteLaxMode,
		TTL:      14 * 24 * time.Hour,
	}
}

func (cfg RefreshCookieConfig) withDefaults() RefreshCookieConfig {
	defaults := DefaultRefreshCookieConfig()
	if strings.TrimSpace(cfg.Name) == "" {
		cfg.Name = defaults.Name
	}
	if strings.TrimSpace(cfg.Path) == "" {
		cfg.Path = defaults.Path
	}
	if cfg.SameSite == http.SameSiteDefaultMode {
		cfg.SameSite = defaults.SameSite
	}
	if cfg.TTL <= 0 {
		cfg.TTL = defaults.TTL
	}
	return cfg
}

func newRefreshCookie(cfg RefreshCookieConfig, value string) http.Cookie {
	return http.Cookie{
		Name:     cfg.Name,
		Value:    value,
		Path:     cfg.Path,
		MaxAge:   int(cfg.TTL.Seconds()),
		HttpOnly: true,
		Secure:   cfg.Secure,
		SameSite: cfg.SameSite,
	}
}

func clearRefreshCookie(cfg RefreshCookieConfig) http.Cookie {
	return http.Cookie{
		Name:     cfg.Name,
		Value:    "",
		Path:     cfg.Path,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
		HttpOnly: true,
		Secure:   cfg.Secure,
		SameSite: cfg.SameSite,
	}
}

func mapAuthError(ctx context.Context, err error) huma.StatusError {
	switch {
	case errors.Is(err, service.ErrInvalidInput):
		return apierror.ValidationFailed("Invalid authentication request.").ForContext(ctx)
	case errors.Is(err, service.ErrEmailAlreadyExists):
		return apierror.Conflict("There already exists an account with the given email address.").ForContext(ctx)
	case errors.Is(err, service.ErrInvalidCredentials):
		return apierror.Unauthorized("Invalid email or password.").ForContext(ctx)
	case errors.Is(err, service.ErrUserDisabled):
		return apierror.Forbidden("User account is disabled.").ForContext(ctx)
	case errors.Is(err, service.ErrInvalidToken):
		return apierror.Unauthorized("Authorization token missing or invalid.").ForContext(ctx)
	case errors.Is(err, service.ErrUserNotFound):
		return apierror.Unauthorized("Invalid authorization token.").ForContext(ctx)
	default:
		return apierror.InternalError(err).ForContext(ctx)
	}
}
