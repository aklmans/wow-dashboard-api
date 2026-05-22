package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
	"github.com/aklmans/wow-dashboard-api/internal/systemevents/domain"
	systemeventsservice "github.com/aklmans/wow-dashboard-api/internal/systemevents/service"
)

type SystemEventsAuthenticator = UsersAuthenticator

type SystemEventsService interface {
	ListEvents(ctx context.Context, input systemeventsservice.ListEventsInput) (domain.ListEventsResult, error)
}

type listSystemEventsInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
	Limit         int    `query:"limit" default:"20" example:"20" doc:"Maximum number of recent system events to return; defaults to 20 and must not exceed 100"`
}

type systemEventsListResponse struct {
	Body systemEventsListBody
}

type systemEventsListBody struct {
	Events []systemEventItem `json:"events" nullable:"false" doc:"Recent system audit events"`
	Limit  int               `json:"limit" example:"20" doc:"Requested event limit after defaulting"`
}

type systemEventItem struct {
	ID        string         `json:"id" example:"c8a89c0b-8e75-4e61-9fa0-70fb83554e66" doc:"System event identifier"`
	EventType string         `json:"eventType" example:"projects.project.created" doc:"Stable system event type"`
	Message   string         `json:"message" example:"Project created." doc:"Safe human-readable audit message"`
	Metadata  map[string]any `json:"metadata" nullable:"false" doc:"Safe JSON object metadata recorded by audit writers"`
	CreatedAt time.Time      `json:"createdAt" doc:"Creation timestamp"`
}

func RegisterSystemEvents(api huma.API, authSvc SystemEventsAuthenticator, eventsSvc SystemEventsService) {
	huma.Register(api, huma.Operation{
		OperationID: "get-system-events",
		Method:      http.MethodGet,
		Path:        "/api/system-events",
		Summary:     "List system events",
		Description: "Returns recent system audit events. Admin role required; non-admin authenticated users receive 403.",
		Tags:        []string{"System Events"},
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *listSystemEventsInput) (*systemEventsListResponse, error) {
		rawAccessToken, ok := parseBearerToken(input.Authorization)
		if !ok {
			return nil, apierror.Unauthorized("Authorization token missing or invalid.").ForContext(ctx)
		}

		currentUser, err := authSvc.CurrentUser(ctx, rawAccessToken)
		if err != nil {
			return nil, mapAuthError(ctx, err)
		}
		if currentUser == nil {
			return nil, apierror.Unauthorized("Authorization token missing or invalid.").ForContext(ctx)
		}
		if err := requireAdmin(ctx, currentUser); err != nil {
			return nil, err
		}

		result, err := eventsSvc.ListEvents(ctx, systemeventsservice.ListEventsInput{
			Limit:         input.Limit,
			LimitProvided: true,
		})
		if err != nil {
			return nil, mapSystemEventsError(ctx, err)
		}
		return listSystemEventsResponseFromDomain(result), nil
	})
}

func listSystemEventsResponseFromDomain(result domain.ListEventsResult) *systemEventsListResponse {
	events := make([]systemEventItem, 0, len(result.Events))
	for _, event := range result.Events {
		events = append(events, systemEventItemFromDomain(event))
	}
	return &systemEventsListResponse{Body: systemEventsListBody{
		Events: events,
		Limit:  result.Limit,
	}}
}

func systemEventItemFromDomain(event domain.Event) systemEventItem {
	metadata := event.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	return systemEventItem{
		ID:        event.ID.String(),
		EventType: event.EventType,
		Message:   event.Message,
		Metadata:  metadata,
		CreatedAt: event.CreatedAt,
	}
}

func mapSystemEventsError(ctx context.Context, err error) huma.StatusError {
	switch {
	case errors.Is(err, systemeventsservice.ErrInvalidInput):
		return apierror.ValidationFailed("Invalid system events query.").ForContext(ctx)
	default:
		return apierror.InternalError(err).ForContext(ctx)
	}
}
