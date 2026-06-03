package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
	"github.com/aklmans/wow-dashboard-api/internal/systemevents/domain"
	systemeventsservice "github.com/aklmans/wow-dashboard-api/internal/systemevents/service"
)

// SecurityActivityService lists a single user's own auth audit events.
type SecurityActivityService interface {
	ListUserActivity(ctx context.Context, input systemeventsservice.ListUserActivityInput) (domain.ListEventsResult, error)
}

type securityActivityInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
	Limit         int    `query:"limit" default:"20" minimum:"1" maximum:"100" example:"20" doc:"Maximum events per page; defaults to 20 and must not exceed 100"`
	Cursor        string `query:"cursor" doc:"Opaque pagination cursor from a previous response's nextCursor; returns the next page of older events"`
}

type securityActivityResponse struct {
	Body securityActivityBody
}

type securityActivityBody struct {
	Events     []securityActivityItem `json:"events" nullable:"false" doc:"Your recent security events, newest first"`
	Limit      int                    `json:"limit" example:"20" doc:"Requested event limit after defaulting"`
	NextCursor string                 `json:"nextCursor,omitempty" doc:"Opaque cursor for the next page; absent when there are no more events"`
}

type securityActivityItem struct {
	ID        string         `json:"id" example:"c8a89c0b-8e75-4e61-9fa0-70fb83554e66" doc:"Event identifier"`
	EventType string         `json:"eventType" example:"auth.sign_in.succeeded" doc:"Stable auth event type"`
	Message   string         `json:"message" example:"Auth sign-in succeeded." doc:"Safe human-readable message"`
	Metadata  map[string]any `json:"metadata" nullable:"false" doc:"Safe metadata: masked email, reason, request id"`
	CreatedAt time.Time      `json:"createdAt" doc:"When the event occurred"`
}

// RegisterSecurityActivity registers the per-user "recent security activity"
// endpoint. It is user-scoped (no permission gate) and behind the auth rate
// limiter via authMiddlewares.
func RegisterSecurityActivity(api huma.API, authSvc AuthService, activitySvc SecurityActivityService, authMiddlewares ...func(huma.Context, func(huma.Context))) {
	huma.Register(api, huma.Operation{
		OperationID: "get-auth-security-activity",
		Method:      http.MethodGet,
		Path:        "/api/auth/security-activity",
		Summary:     "List my recent security activity",
		Description: "Returns the signed-in user's own recent auth events — sign-ins (and failed attempts), password and MFA changes, and session revocations — newest first.",
		Tags:        []string{"Auth"},
		Middlewares: huma.Middlewares(authMiddlewares),
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusTooManyRequests,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *securityActivityInput) (*securityActivityResponse, error) {
		userID, authErr := currentUserID(ctx, authSvc, input.Authorization,
			"Security activity is unavailable while impersonating a user.")
		if authErr != nil {
			return nil, authErr
		}

		svcInput := systemeventsservice.ListUserActivityInput{
			UserID:        userID,
			Limit:         input.Limit,
			LimitProvided: true,
		}
		if input.Cursor != "" {
			createdAt, id, err := decodeKeysetCursor(input.Cursor)
			if err != nil {
				return nil, apierror.ValidationFailed("Invalid pagination cursor.").ForContext(ctx)
			}
			svcInput.CursorCreatedAt = &createdAt
			svcInput.CursorID = &id
		}

		result, err := activitySvc.ListUserActivity(ctx, svcInput)
		if err != nil {
			return nil, mapSystemEventsError(ctx, err)
		}
		return securityActivityResponseFromDomain(result), nil
	})
}

func securityActivityResponseFromDomain(result domain.ListEventsResult) *securityActivityResponse {
	events := make([]securityActivityItem, 0, len(result.Events))
	for _, event := range result.Events {
		metadata := event.Metadata
		if metadata == nil {
			metadata = map[string]any{}
		}
		events = append(events, securityActivityItem{
			ID:        event.ID.String(),
			EventType: event.EventType,
			Message:   event.Message,
			Metadata:  metadata,
			CreatedAt: event.CreatedAt,
		})
	}
	body := securityActivityBody{Events: events, Limit: result.Limit}
	if result.HasMore && len(result.Events) > 0 {
		last := result.Events[len(result.Events)-1]
		body.NextCursor = encodeKeysetCursor(last.CreatedAt, last.ID)
	}
	return &securityActivityResponse{Body: body}
}
