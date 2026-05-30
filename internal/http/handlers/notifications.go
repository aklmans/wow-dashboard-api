package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
	"github.com/aklmans/wow-dashboard-api/internal/notifications/domain"
	notificationsservice "github.com/aklmans/wow-dashboard-api/internal/notifications/service"
)

// NotificationsAuthenticator resolves the current user from a bearer token.
// Notifications are per-user and require only authentication — no permission.
type NotificationsAuthenticator = UsersAuthenticator

type NotificationsService interface {
	List(ctx context.Context, input notificationsservice.ListInput) (domain.ListResult, error)
	MarkRead(ctx context.Context, userID, id uuid.UUID) (int64, error)
	MarkAllRead(ctx context.Context, userID uuid.UUID) (int64, error)
}

type listNotificationsInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
	Limit         int    `query:"limit" default:"20" minimum:"1" maximum:"100" example:"20" doc:"Maximum number of notifications per page; defaults to 20 and must not exceed 100"`
	UnreadOnly    bool   `query:"unreadOnly" example:"false" doc:"When true, return only unread notifications"`
	Cursor        string `query:"cursor" doc:"Opaque pagination cursor from a previous response's nextCursor; returns the next page of older notifications"`
}

type markNotificationReadInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
	ID            string `path:"id" example:"c8a89c0b-8e75-4e61-9fa0-70fb83554e66" doc:"Notification identifier"`
}

type markAllNotificationsReadInput struct {
	Authorization string `header:"Authorization" example:"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." doc:"Bearer access token"`
}

type notificationsListResponse struct {
	Body notificationsListBody
}

type notificationsListBody struct {
	Notifications []notificationItem `json:"notifications" nullable:"false" doc:"The user's notifications, newest first"`
	Limit         int                `json:"limit" example:"20" doc:"Requested limit after defaulting"`
	UnreadCount   int64              `json:"unreadCount" example:"3" doc:"Total unread notifications for the user, independent of this page"`
	NextCursor    string             `json:"nextCursor,omitempty" doc:"Opaque cursor for the next page; absent when there are no more notifications"`
}

type unreadCountResponse struct {
	Body unreadCountBody
}

type unreadCountBody struct {
	UnreadCount int64 `json:"unreadCount" example:"2" doc:"The user's unread notification count after the operation"`
}

type notificationItem struct {
	ID        string         `json:"id" example:"c8a89c0b-8e75-4e61-9fa0-70fb83554e66" doc:"Notification identifier"`
	Type      string         `json:"type" example:"users.roles.updated" doc:"Stable notification type"`
	Title     string         `json:"title" example:"Your roles were updated" doc:"Short headline"`
	Body      string         `json:"body" example:"An administrator changed your roles." doc:"Optional longer text"`
	Metadata  map[string]any `json:"metadata" nullable:"false" doc:"Safe JSON object metadata"`
	ReadAt    *time.Time     `json:"readAt,omitempty" doc:"When the user read the notification; absent while unread"`
	CreatedAt time.Time      `json:"createdAt" doc:"Creation timestamp"`
}

func RegisterNotifications(api huma.API, authSvc NotificationsAuthenticator, notificationsSvc NotificationsService) {
	huma.Register(api, huma.Operation{
		OperationID: "list-notifications",
		Method:      http.MethodGet,
		Path:        "/api/notifications",
		Summary:     "List notifications",
		Description: "Returns the authenticated user's notifications, newest first, with their unread count.",
		Tags:        []string{"Notifications"},
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *listNotificationsInput) (*notificationsListResponse, error) {
		userID, authErr := authenticateNotifications(ctx, authSvc, input.Authorization)
		if authErr != nil {
			return nil, authErr
		}

		svcInput := notificationsservice.ListInput{
			UserID:        userID,
			Limit:         input.Limit,
			LimitProvided: true,
			UnreadOnly:    input.UnreadOnly,
		}
		if input.Cursor != "" {
			createdAt, id, err := decodeKeysetCursor(input.Cursor)
			if err != nil {
				return nil, apierror.ValidationFailed("Invalid pagination cursor.").ForContext(ctx)
			}
			svcInput.CursorCreatedAt = &createdAt
			svcInput.CursorID = &id
		}

		result, err := notificationsSvc.List(ctx, svcInput)
		if err != nil {
			return nil, mapNotificationsError(ctx, err)
		}
		return notificationsListResponseFromDomain(result), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "mark-notification-read",
		Method:      http.MethodPost,
		Path:        "/api/notifications/{id}/read",
		Summary:     "Mark a notification read",
		Description: "Marks one of the authenticated user's notifications read. Idempotent; returns the updated unread count.",
		Tags:        []string{"Notifications"},
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusUnprocessableEntity,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *markNotificationReadInput) (*unreadCountResponse, error) {
		userID, authErr := authenticateNotifications(ctx, authSvc, input.Authorization)
		if authErr != nil {
			return nil, authErr
		}
		id, err := uuid.Parse(input.ID)
		if err != nil {
			return nil, apierror.ValidationFailed("Invalid notification id.").ForContext(ctx)
		}

		unread, err := notificationsSvc.MarkRead(ctx, userID, id)
		if err != nil {
			return nil, mapNotificationsError(ctx, err)
		}
		return &unreadCountResponse{Body: unreadCountBody{UnreadCount: unread}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "mark-all-notifications-read",
		Method:      http.MethodPost,
		Path:        "/api/notifications/read-all",
		Summary:     "Mark all notifications read",
		Description: "Marks every unread notification for the authenticated user read.",
		Tags:        []string{"Notifications"},
		Responses: apiErrorResponses(api,
			http.StatusUnauthorized,
			http.StatusInternalServerError,
		),
	}, func(ctx context.Context, input *markAllNotificationsReadInput) (*unreadCountResponse, error) {
		userID, authErr := authenticateNotifications(ctx, authSvc, input.Authorization)
		if authErr != nil {
			return nil, authErr
		}
		unread, err := notificationsSvc.MarkAllRead(ctx, userID)
		if err != nil {
			return nil, mapNotificationsError(ctx, err)
		}
		return &unreadCountResponse{Body: unreadCountBody{UnreadCount: unread}}, nil
	})
}

// authenticateNotifications resolves the current user's id from the bearer
// token. Notifications need only authentication; ownership is enforced in the
// query (every statement is scoped by user_id), so there is no permission gate.
func authenticateNotifications(ctx context.Context, authSvc NotificationsAuthenticator, authHeader string) (uuid.UUID, huma.StatusError) {
	rawAccessToken, ok := parseBearerToken(authHeader)
	if !ok {
		return uuid.Nil, apierror.Unauthorized("Authorization token missing or invalid.").ForContext(ctx)
	}
	user, err := authSvc.CurrentUser(ctx, rawAccessToken)
	if err != nil {
		return uuid.Nil, mapAuthError(ctx, err)
	}
	if user == nil {
		return uuid.Nil, apierror.Unauthorized("Authorization token missing or invalid.").ForContext(ctx)
	}
	id, parseErr := uuid.Parse(user.ID)
	if parseErr != nil {
		return uuid.Nil, apierror.InternalError(parseErr).ForContext(ctx)
	}
	return id, nil
}

func notificationsListResponseFromDomain(result domain.ListResult) *notificationsListResponse {
	items := make([]notificationItem, 0, len(result.Notifications))
	for _, n := range result.Notifications {
		items = append(items, notificationItemFromDomain(n))
	}
	body := notificationsListBody{
		Notifications: items,
		Limit:         result.Limit,
		UnreadCount:   result.UnreadCount,
	}
	if result.HasMore && len(result.Notifications) > 0 {
		last := result.Notifications[len(result.Notifications)-1]
		body.NextCursor = encodeKeysetCursor(last.CreatedAt, last.ID)
	}
	return &notificationsListResponse{Body: body}
}

func notificationItemFromDomain(n domain.Notification) notificationItem {
	metadata := n.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	return notificationItem{
		ID:        n.ID.String(),
		Type:      n.Type,
		Title:     n.Title,
		Body:      n.Body,
		Metadata:  metadata,
		ReadAt:    n.ReadAt,
		CreatedAt: n.CreatedAt,
	}
}

func mapNotificationsError(ctx context.Context, err error) huma.StatusError {
	switch {
	case errors.Is(err, notificationsservice.ErrInvalidInput):
		return apierror.ValidationFailed("Invalid notifications request.").ForContext(ctx)
	default:
		return apierror.InternalError(err).ForContext(ctx)
	}
}
