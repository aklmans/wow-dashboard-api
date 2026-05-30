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
	UpdateMyProfile(ctx context.Context, rawAccessToken string, input service.UpdateMyProfileInput) (*service.PublicUser, error)
	ChangePassword(ctx context.Context, rawAccessToken string, currentPassword string, newPassword string) error
	ForgotPassword(ctx context.Context, email string) error
	ResetPassword(ctx context.Context, rawToken string, newPassword string) error
	VerifyEmail(ctx context.Context, rawToken string) error
	ResendVerification(ctx context.Context, rawAccessToken string) error
}

type authUser struct {
	ID          string `json:"id" example:"c8a89c0b-8e75-4e61-9fa0-70fb83554e66" doc:"User identifier"`
	Email       string `json:"email" example:"demo@wow-dashboard.test" doc:"User email address"`
	DisplayName string `json:"displayName" example:"Demo User" doc:"User display name"`
}

// authMeUser extends the basic profile with the resolved roles and effective
// permissions a frontend uses to render menus and gate actions.
type authMeUser struct {
	ID            string     `json:"id" example:"c8a89c0b-8e75-4e61-9fa0-70fb83554e66" doc:"User identifier"`
	Email         string     `json:"email" example:"demo@wow-dashboard.test" doc:"User email address"`
	DisplayName   string     `json:"displayName" example:"Demo User" doc:"User display name"`
	EmailVerified bool       `json:"emailVerified" example:"true" doc:"Whether the user has confirmed their email address"`
	AvatarURL     string     `json:"avatarUrl" example:"" doc:"User avatar image URL; empty when unset"`
	Phone         string     `json:"phone" example:"" doc:"User phone number; empty when unset"`
	JobTitle      string     `json:"jobTitle" example:"" doc:"User job title; empty when unset"`
	Company       string     `json:"company" example:"" doc:"User company; empty when unset"`
	LastLoginAt   *time.Time `json:"lastLoginAt,omitempty" doc:"Last successful sign-in time; null if the user has never signed in"`
	Roles         []string   `json:"roles" nullable:"false" doc:"Names of the roles assigned to the user"`
	Permissions   []string   `json:"permissions" nullable:"false" doc:"Effective permission strings granted by the user's roles"`
}

type authSessionBody struct {
	User authUser `json:"user" doc:"Authenticated user profile"`
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
		Password  string `json:"password" minLength:"8" maxLength:"4096" example:"@Password" doc:"Account password"`
		FirstName string `json:"firstName" maxLength:"100" example:"Hello" doc:"First name"`
		LastName  string `json:"lastName" maxLength:"100" example:"Friend" doc:"Last name"`
	}
}

type signInInput struct {
	Body struct {
		Email    string `json:"email" maxLength:"320" example:"demo@wow-dashboard.test" doc:"Email address"`
		Password string `json:"password" maxLength:"4096" example:"@Password" doc:"Account password"`
	}
}

type meInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
}

type changePasswordInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
	Body          struct {
		CurrentPassword string `json:"currentPassword" minLength:"1" doc:"The user's current password"`
		NewPassword     string `json:"newPassword" minLength:"8" maxLength:"4096" doc:"The new password (minimum 8 characters)"`
	}
}

type forgotPasswordInput struct {
	Body struct {
		Email string `json:"email" format:"email" example:"demo@wow-dashboard.test" doc:"Email address to send a reset link to"`
	}
}

type resetPasswordInput struct {
	Body struct {
		Token       string `json:"token" minLength:"1" doc:"The reset token from the email"`
		NewPassword string `json:"newPassword" minLength:"8" maxLength:"4096" doc:"The new password (minimum 8 characters)"`
	}
}

type verifyEmailInput struct {
	Body struct {
		Token string `json:"token" minLength:"1" doc:"The verification token from the email"`
	}
}

type resendVerificationInput struct {
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

// AccessCookieConfig configures the HttpOnly cookie that carries the JWT access
// token. Unlike the refresh cookie it uses Path "/" so the browser sends it on
// every API call and a same-site edge middleware can see it.
type AccessCookieConfig struct {
	Name     string
	Path     string
	Domain   string
	Secure   bool
	SameSite http.SameSite
	TTL      time.Duration
}

// RegisterAuth registers Starter-compatible JWT auth endpoints.
func RegisterAuth(api huma.API, authSvc AuthService, authMiddlewares ...func(huma.Context, func(huma.Context))) {
	RegisterAuthWithCookies(api, authSvc, DefaultRefreshCookieConfig(), DefaultAccessCookieConfig(), authMiddlewares...)
}

// RegisterAuthWithCookies registers auth endpoints with explicit cookie settings.
func RegisterAuthWithCookies(api huma.API, authSvc AuthService, refreshCookie RefreshCookieConfig, accessCookie AccessCookieConfig, authMiddlewares ...func(huma.Context, func(huma.Context))) {
	refreshCookie = refreshCookie.withDefaults()
	accessCookie = accessCookie.withDefaults()

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
		return sessionResponse(session, refreshCookie, accessCookie), nil
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
		return sessionResponse(session, refreshCookie, accessCookie), nil
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
		return sessionResponse(session, refreshCookie, accessCookie), nil
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
			SetCookie: []http.Cookie{clearRefreshCookie(refreshCookie), clearAccessCookie(accessCookie)},
			Body:      authSuccessBody{Success: true},
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "post-auth-change-password",
		Method:      http.MethodPost,
		Path:        "/api/auth/change-password",
		Middlewares: huma.Middlewares(authMiddlewares),
		Summary:     "Change password",
		Description: "Changes the signed-in user's password after verifying the current one, then revokes every refresh token so all sessions must re-authenticate.",
		Tags:        []string{"Auth"},
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *changePasswordInput) (*authSuccessResponse, error) {
		rawAccessToken, ok := parseBearerToken(input.Authorization)
		if !ok {
			return nil, apierror.Unauthorized("Authorization token missing or invalid.").ForContext(ctx)
		}

		if err := authSvc.ChangePassword(ctx, rawAccessToken, input.Body.CurrentPassword, input.Body.NewPassword); err != nil {
			return nil, mapAuthError(ctx, err)
		}

		// The change revoked every refresh token, so clear the now-dead cookie.
		return &authSuccessResponse{
			SetCookie: []http.Cookie{clearRefreshCookie(refreshCookie), clearAccessCookie(accessCookie)},
			Body:      authSuccessBody{Success: true},
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "post-auth-forgot-password",
		Method:      http.MethodPost,
		Path:        "/api/auth/forgot-password",
		Middlewares: huma.Middlewares(authMiddlewares),
		Summary:     "Request a password reset",
		Description: "Emails a password-reset link if the address matches an active account. Always succeeds, so the response cannot be used to probe which emails are registered.",
		Tags:        []string{"Auth"},
		Responses: apiErrorResponses(api,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *forgotPasswordInput) (*authSuccessResponse, error) {
		if err := authSvc.ForgotPassword(ctx, input.Body.Email); err != nil {
			return nil, mapAuthError(ctx, err)
		}
		return &authSuccessResponse{Body: authSuccessBody{Success: true}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "post-auth-reset-password",
		Method:      http.MethodPost,
		Path:        "/api/auth/reset-password",
		Middlewares: huma.Middlewares(authMiddlewares),
		Summary:     "Reset a password with a token",
		Description: "Sets a new password using a token from the forgot-password email, then revokes every session for the account.",
		Tags:        []string{"Auth"},
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *resetPasswordInput) (*authSuccessResponse, error) {
		if err := authSvc.ResetPassword(ctx, input.Body.Token, input.Body.NewPassword); err != nil {
			return nil, mapAuthError(ctx, err)
		}
		return &authSuccessResponse{Body: authSuccessBody{Success: true}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "post-auth-verify-email",
		Method:      http.MethodPost,
		Path:        "/api/auth/verify-email",
		Middlewares: huma.Middlewares(authMiddlewares),
		Summary:     "Verify an email address",
		Description: "Confirms an email address using a verification token from the sign-up email.",
		Tags:        []string{"Auth"},
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *verifyEmailInput) (*authSuccessResponse, error) {
		if err := authSvc.VerifyEmail(ctx, input.Body.Token); err != nil {
			return nil, mapAuthError(ctx, err)
		}
		return &authSuccessResponse{Body: authSuccessBody{Success: true}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "post-auth-resend-verification",
		Method:      http.MethodPost,
		Path:        "/api/auth/resend-verification",
		Middlewares: huma.Middlewares(authMiddlewares),
		Summary:     "Resend the email verification link",
		Description: "Issues a fresh email-verification link for the signed-in user. A no-op when the email is already verified.",
		Tags:        []string{"Auth"},
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *resendVerificationInput) (*authSuccessResponse, error) {
		rawAccessToken, ok := parseBearerToken(input.Authorization)
		if !ok {
			return nil, apierror.Unauthorized("Authorization token missing or invalid.").ForContext(ctx)
		}
		if err := authSvc.ResendVerification(ctx, rawAccessToken); err != nil {
			return nil, mapAuthError(ctx, err)
		}
		return &authSuccessResponse{Body: authSuccessBody{Success: true}}, nil
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

	huma.Register(api, huma.Operation{
		OperationID: "patch-auth-me",
		Method:      http.MethodPatch,
		Path:        "/api/auth/me",
		Summary:     "Update own profile",
		Description: "Updates the calling user's own profile fields (displayName, avatarUrl, phone, jobTitle, company). Status and role assignments are not editable through this path — those require an admin via PATCH /api/users/{id}.",
		Tags:        []string{"Auth"},
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *patchMeInput) (*authMeResponse, error) {
		rawAccessToken, ok := parseBearerToken(input.Authorization)
		if !ok {
			return nil, apierror.Unauthorized("Authorization token missing or invalid.").ForContext(ctx)
		}
		user, err := authSvc.UpdateMyProfile(ctx, rawAccessToken, service.UpdateMyProfileInput{
			DisplayName: input.Body.DisplayName,
			AvatarURL:   input.Body.AvatarURL,
			Phone:       input.Body.Phone,
			JobTitle:    input.Body.JobTitle,
			Company:     input.Body.Company,
		})
		if err != nil {
			return nil, mapAuthError(ctx, err)
		}
		return &authMeResponse{Body: authMeBody{User: meUserResponse(*user)}}, nil
	})
}

type patchMeInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
	Body          struct {
		DisplayName *string `json:"displayName,omitempty" minLength:"1" maxLength:"256" doc:"New display name; omit to leave unchanged"`
		AvatarURL   *string `json:"avatarUrl,omitempty" maxLength:"262144" doc:"New avatar URL or inline data URL; omit to leave unchanged"`
		Phone       *string `json:"phone,omitempty" maxLength:"256" doc:"New phone number; omit to leave unchanged"`
		JobTitle    *string `json:"jobTitle,omitempty" maxLength:"256" doc:"New job title; omit to leave unchanged"`
		Company     *string `json:"company,omitempty" maxLength:"256" doc:"New company; omit to leave unchanged"`
	}
}

func sessionResponse(session *service.Session, refreshCookie RefreshCookieConfig, accessCookie AccessCookieConfig) *authSessionResponse {
	resp := &authSessionResponse{
		Body: authSessionBody{
			User: publicUserResponse(session.User),
		},
	}
	// The access token now ships only as an HttpOnly cookie — never in the JSON
	// body — so it is never reachable from JavaScript.
	if session.AccessToken != "" {
		resp.SetCookie = append(resp.SetCookie, newAccessCookie(accessCookie, session.AccessToken))
	}
	if session.RefreshToken != "" {
		resp.SetCookie = append(resp.SetCookie, newRefreshCookie(refreshCookie, session.RefreshToken))
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
		ID:            user.ID,
		Email:         user.Email,
		DisplayName:   user.DisplayName,
		EmailVerified: user.EmailVerified,
		AvatarURL:     user.AvatarURL,
		Phone:         user.Phone,
		JobTitle:      user.JobTitle,
		Company:       user.Company,
		LastLoginAt:   user.LastLoginAt,
		Roles:         roles,
		Permissions:   permissions,
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

func DefaultAccessCookieConfig() AccessCookieConfig {
	return AccessCookieConfig{
		Name:     "wow_dashboard_access_token",
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		TTL:      15 * time.Minute,
	}
}

func (cfg AccessCookieConfig) withDefaults() AccessCookieConfig {
	defaults := DefaultAccessCookieConfig()
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

func newAccessCookie(cfg AccessCookieConfig, value string) http.Cookie {
	return http.Cookie{
		Name:     cfg.Name,
		Value:    value,
		Path:     cfg.Path,
		Domain:   cfg.Domain,
		MaxAge:   int(cfg.TTL.Seconds()),
		HttpOnly: true,
		Secure:   cfg.Secure,
		SameSite: cfg.SameSite,
	}
}

func clearAccessCookie(cfg AccessCookieConfig) http.Cookie {
	return http.Cookie{
		Name:     cfg.Name,
		Value:    "",
		Path:     cfg.Path,
		Domain:   cfg.Domain,
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
