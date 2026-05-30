package handlers

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/auth/rbac"
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
	Limit         int    `query:"limit" default:"20" minimum:"1" maximum:"100" example:"20" doc:"Maximum number of events to return per page; defaults to 20 and must not exceed 100"`
	EventType     string `query:"eventType" example:"projects.project.created" doc:"Filter to a single stable event type"`
	After         string `query:"after" example:"2026-01-01T00:00:00Z" doc:"Only events created at or after this RFC3339 timestamp"`
	Before        string `query:"before" example:"2026-02-01T00:00:00Z" doc:"Only events created strictly before this RFC3339 timestamp"`
	Cursor        string `query:"cursor" doc:"Opaque pagination cursor from a previous response's nextCursor; returns the next page of older events"`
}

type systemEventsListResponse struct {
	Body systemEventsListBody
}

type systemEventsListBody struct {
	Events     []systemEventItem `json:"events" nullable:"false" doc:"System audit events, newest first"`
	Limit      int               `json:"limit" example:"20" doc:"Requested event limit after defaulting"`
	NextCursor string            `json:"nextCursor,omitempty" doc:"Opaque cursor for the next page; absent when there are no more events"`
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
		Description: "Returns recent system audit events. Requires the system_events:read permission.",
		Tags:        []string{"System Events"},
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *listSystemEventsInput) (*systemEventsListResponse, error) {
		if _, err := authorizeWithPermission(ctx, authSvc, input.Authorization, rbac.PermissionSystemEventsRead); err != nil {
			return nil, err
		}

		svcInput := systemeventsservice.ListEventsInput{
			Limit:         input.Limit,
			LimitProvided: true,
			EventType:     input.EventType,
		}
		if input.After != "" {
			after, err := time.Parse(time.RFC3339, input.After)
			if err != nil {
				return nil, apierror.ValidationFailed("Invalid 'after' timestamp; expected RFC3339.").ForContext(ctx)
			}
			svcInput.CreatedAfter = &after
		}
		if input.Before != "" {
			before, err := time.Parse(time.RFC3339, input.Before)
			if err != nil {
				return nil, apierror.ValidationFailed("Invalid 'before' timestamp; expected RFC3339.").ForContext(ctx)
			}
			svcInput.CreatedBefore = &before
		}
		if input.Cursor != "" {
			createdAt, id, err := decodeEventsCursor(input.Cursor)
			if err != nil {
				return nil, apierror.ValidationFailed("Invalid pagination cursor.").ForContext(ctx)
			}
			svcInput.CursorCreatedAt = &createdAt
			svcInput.CursorID = &id
		}

		result, err := eventsSvc.ListEvents(ctx, svcInput)
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
	body := systemEventsListBody{
		Events: events,
		Limit:  result.Limit,
	}
	if result.HasMore && len(result.Events) > 0 {
		last := result.Events[len(result.Events)-1]
		body.NextCursor = encodeEventsCursor(last.CreatedAt, last.ID)
	}
	return &systemEventsListResponse{Body: body}
}

// encodeEventsCursor produces an opaque, URL-safe cursor from a page's last
// (created_at, id) keyset position.
func encodeEventsCursor(createdAt time.Time, id uuid.UUID) string {
	raw := createdAt.UTC().Format(time.RFC3339Nano) + "|" + id.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeEventsCursor reverses encodeEventsCursor, rejecting malformed input.
func decodeEventsCursor(cursor string) (time.Time, uuid.UUID, error) {
	data, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	parts := strings.SplitN(string(data), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, errors.New("handlers: malformed events cursor")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	return createdAt, id, nil
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
