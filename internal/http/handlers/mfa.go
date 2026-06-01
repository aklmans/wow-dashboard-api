package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	authservice "github.com/aklmans/wow-dashboard-api/internal/auth/service"
	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
	mfaservice "github.com/aklmans/wow-dashboard-api/internal/mfa/service"
)

// MfaAuthenticator resolves the bearer token to the current user.
type MfaAuthenticator interface {
	CurrentUser(ctx context.Context, rawAccessToken string) (*authservice.PublicUser, error)
}

// MfaService is the MFA enrollment use-case port.
type MfaService interface {
	Setup(ctx context.Context, userID uuid.UUID, accountName string) (mfaservice.SetupResult, error)
	Confirm(ctx context.Context, userID uuid.UUID, code string) ([]string, error)
}

type mfaSetupInput struct {
	Authorization string `header:"Authorization" doc:"Bearer access token"`
}

type mfaSetupBody struct {
	OtpauthURL string `json:"otpauthUrl" doc:"otpauth:// URI to render as a QR code in an authenticator app"`
	Secret     string `json:"secret" doc:"Base32 TOTP secret for manual entry as an alternative to the QR code"`
}

type mfaSetupResponse struct {
	Body mfaSetupBody
}

type mfaConfirmInput struct {
	Authorization string `header:"Authorization" doc:"Bearer access token"`
	Body          struct {
		Code string `json:"code" minLength:"6" maxLength:"16" example:"123456" doc:"The current code from the authenticator app"`
	}
}

type mfaConfirmBody struct {
	RecoveryCodes []string `json:"recoveryCodes" nullable:"false" doc:"One-time recovery codes — shown once, store them somewhere safe"`
}

type mfaConfirmResponse struct {
	Body mfaConfirmBody
}

// RegisterMfa registers the MFA enrollment endpoints. authMiddlewares carries
// the per-IP auth rate limiter, so the code-confirm endpoint is throttled.
func RegisterMfa(api huma.API, authSvc MfaAuthenticator, mfaSvc MfaService, authMiddlewares ...func(huma.Context, func(huma.Context))) {
	huma.Register(api, huma.Operation{
		OperationID: "post-auth-mfa-setup",
		Method:      http.MethodPost,
		Path:        "/api/auth/mfa/setup",
		Summary:     "Start MFA enrollment",
		Description: "Generates a new TOTP secret for the current user and returns an otpauth URI + the raw secret. MFA is not active until the code is confirmed.",
		Tags:        []string{"Auth"},
		Middlewares: huma.Middlewares(authMiddlewares),
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusConflict,
			http.StatusTooManyRequests,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *mfaSetupInput) (*mfaSetupResponse, error) {
		currentUser, authErr := authenticateForMfa(ctx, authSvc, input.Authorization)
		if authErr != nil {
			return nil, authErr
		}
		if currentUser.MfaEnabled {
			return nil, apierror.Conflict("MFA is already enabled. Disable it first to re-enroll.").ForContext(ctx)
		}
		userID, err := uuid.Parse(currentUser.ID)
		if err != nil {
			return nil, apierror.Unauthorized("Authorization token missing or invalid.").ForContext(ctx)
		}

		result, err := mfaSvc.Setup(ctx, userID, currentUser.Email)
		if err != nil {
			return nil, apierror.InternalError(err).ForContext(ctx)
		}
		return &mfaSetupResponse{Body: mfaSetupBody{OtpauthURL: result.OtpauthURL, Secret: result.Secret}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "post-auth-mfa-confirm",
		Method:      http.MethodPost,
		Path:        "/api/auth/mfa/confirm",
		Summary:     "Confirm MFA enrollment",
		Description: "Verifies the first authenticator code, turns MFA on, and returns one-time recovery codes (shown only once).",
		Tags:        []string{"Auth"},
		Middlewares: huma.Middlewares(authMiddlewares),
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusConflict,
			http.StatusTooManyRequests,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *mfaConfirmInput) (*mfaConfirmResponse, error) {
		currentUser, authErr := authenticateForMfa(ctx, authSvc, input.Authorization)
		if authErr != nil {
			return nil, authErr
		}
		if currentUser.MfaEnabled {
			return nil, apierror.Conflict("MFA is already enabled.").ForContext(ctx)
		}
		userID, err := uuid.Parse(currentUser.ID)
		if err != nil {
			return nil, apierror.Unauthorized("Authorization token missing or invalid.").ForContext(ctx)
		}

		codes, err := mfaSvc.Confirm(ctx, userID, input.Body.Code)
		if err != nil {
			return nil, mapMfaError(ctx, err)
		}
		return &mfaConfirmResponse{Body: mfaConfirmBody{RecoveryCodes: codes}}, nil
	})
}

func authenticateForMfa(ctx context.Context, authSvc MfaAuthenticator, authHeader string) (*authservice.PublicUser, huma.StatusError) {
	rawAccessToken, ok := parseBearerToken(authHeader)
	if !ok {
		return nil, apierror.Unauthorized("Authorization token missing or invalid.").ForContext(ctx)
	}
	user, err := authSvc.CurrentUser(ctx, rawAccessToken)
	if err != nil {
		return nil, mapAuthError(ctx, err)
	}
	if user == nil {
		return nil, apierror.Unauthorized("Authorization token missing or invalid.").ForContext(ctx)
	}
	return user, nil
}

func mapMfaError(ctx context.Context, err error) huma.StatusError {
	switch {
	case errors.Is(err, mfaservice.ErrInvalidCode):
		return apierror.ValidationFailed("That code is not valid. Try the current code from your authenticator app.").ForContext(ctx)
	case errors.Is(err, mfaservice.ErrNotEnrolling):
		return apierror.Conflict("Start MFA setup before confirming.").ForContext(ctx)
	default:
		return apierror.InternalError(err).ForContext(ctx)
	}
}
