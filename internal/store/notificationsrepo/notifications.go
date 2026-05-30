// Package notificationsrepo is the PostgreSQL-backed adapter for per-user notifications.
package notificationsrepo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/aklmans/wow-dashboard-api/internal/notifications/domain"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
)

type NotificationStore struct {
	queries *query.Queries
}

func NewNotificationStore(q *query.Queries) *NotificationStore {
	return &NotificationStore{queries: q}
}

func NewNotificationStoreFromDB(db query.DBTX) *NotificationStore {
	return NewNotificationStore(query.New(db))
}

// ListNotifications returns one page of a user's notifications. Limit, HasMore,
// and the unread count are owned by the service; the store only returns the
// rows the query produced.
func (s *NotificationStore) ListNotifications(ctx context.Context, input domain.ListInput) (domain.ListResult, error) {
	if s.queries == nil {
		return domain.ListResult{}, fmt.Errorf("notificationsrepo: queries is nil")
	}

	rows, err := s.queries.ListNotificationsPage(ctx, query.ListNotificationsPageParams{
		UserID:          pgUUID(input.UserID),
		UnreadOnly:      input.UnreadOnly,
		CursorCreatedAt: pgTimestampPtr(input.CursorCreatedAt),
		CursorID:        pgUUIDPtr(input.CursorID),
		RowLimit:        int32(input.Limit),
	})
	if err != nil {
		return domain.ListResult{}, fmt.Errorf("notificationsrepo: list notifications: %w", err)
	}

	notifications := make([]domain.Notification, 0, len(rows))
	for _, row := range rows {
		n, err := notificationFromRow(row)
		if err != nil {
			return domain.ListResult{}, fmt.Errorf("notificationsrepo: convert notification: %w", err)
		}
		notifications = append(notifications, n)
	}
	return domain.ListResult{Notifications: notifications}, nil
}

// CountUnread returns the user's total number of unread notifications.
func (s *NotificationStore) CountUnread(ctx context.Context, userID uuid.UUID) (int64, error) {
	if s.queries == nil {
		return 0, fmt.Errorf("notificationsrepo: queries is nil")
	}
	count, err := s.queries.CountUnreadNotifications(ctx, pgUUID(userID))
	if err != nil {
		return 0, fmt.Errorf("notificationsrepo: count unread: %w", err)
	}
	return count, nil
}

// MarkRead marks one of the user's notifications read and reports how many rows
// changed (0 when the id does not exist, is already read, or belongs to another
// user). The user_id predicate makes cross-user writes impossible.
func (s *NotificationStore) MarkRead(ctx context.Context, userID, id uuid.UUID) (int64, error) {
	if s.queries == nil {
		return 0, fmt.Errorf("notificationsrepo: queries is nil")
	}
	affected, err := s.queries.MarkNotificationRead(ctx, query.MarkNotificationReadParams{
		ID:     pgUUID(id),
		UserID: pgUUID(userID),
	})
	if err != nil {
		return 0, fmt.Errorf("notificationsrepo: mark read: %w", err)
	}
	return affected, nil
}

// MarkAllRead marks every unread notification for the user read and reports how
// many rows changed.
func (s *NotificationStore) MarkAllRead(ctx context.Context, userID uuid.UUID) (int64, error) {
	if s.queries == nil {
		return 0, fmt.Errorf("notificationsrepo: queries is nil")
	}
	affected, err := s.queries.MarkAllNotificationsRead(ctx, pgUUID(userID))
	if err != nil {
		return 0, fmt.Errorf("notificationsrepo: mark all read: %w", err)
	}
	return affected, nil
}

// Create inserts a notification, generating its id and created_at. Domain code
// calls this to emit a notification for a user.
func (s *NotificationStore) Create(ctx context.Context, input domain.CreateInput) (domain.Notification, error) {
	if s.queries == nil {
		return domain.Notification{}, fmt.Errorf("notificationsrepo: queries is nil")
	}

	metadata, err := metadataBytes(input.Metadata)
	if err != nil {
		return domain.Notification{}, fmt.Errorf("notificationsrepo: marshal metadata: %w", err)
	}

	row, err := s.queries.CreateNotification(ctx, query.CreateNotificationParams{
		ID:        pgUUID(uuid.New()),
		UserID:    pgUUID(input.UserID),
		Type:      input.Type,
		Title:     input.Title,
		Body:      input.Body,
		Metadata:  metadata,
		CreatedAt: pgTimestamp(time.Now().UTC().Truncate(time.Microsecond)),
	})
	if err != nil {
		return domain.Notification{}, fmt.Errorf("notificationsrepo: create notification: %w", err)
	}
	return notificationFromRow(row)
}

func notificationFromRow(row query.Notification) (domain.Notification, error) {
	id, err := domainUUID(row.ID)
	if err != nil {
		return domain.Notification{}, err
	}
	userID, err := domainUUID(row.UserID)
	if err != nil {
		return domain.Notification{}, err
	}
	createdAt, err := domainTimestamp(row.CreatedAt)
	if err != nil {
		return domain.Notification{}, err
	}
	return domain.Notification{
		ID:        id,
		UserID:    userID,
		Type:      row.Type,
		Title:     row.Title,
		Body:      row.Body,
		Metadata:  metadataObject(row.Metadata),
		ReadAt:    domainTimestampPtr(row.ReadAt),
		CreatedAt: createdAt,
	}, nil
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func pgUUIDPtr(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *id, Valid: true}
}

func pgTimestamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func pgTimestampPtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func domainUUID(id pgtype.UUID) (uuid.UUID, error) {
	if !id.Valid {
		return uuid.Nil, fmt.Errorf("invalid notification uuid")
	}
	return uuid.UUID(id.Bytes), nil
}

func domainTimestamp(t pgtype.Timestamptz) (time.Time, error) {
	if !t.Valid {
		return time.Time{}, fmt.Errorf("invalid notification timestamp")
	}
	return t.Time, nil
}

func domainTimestampPtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	out := t.Time
	return &out
}

func metadataObject(raw []byte) map[string]any {
	out := map[string]any{}
	if len(raw) == 0 {
		return out
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return out
	}
	object, ok := decoded.(map[string]any)
	if !ok || object == nil {
		return out
	}
	return object
}

func metadataBytes(metadata map[string]any) ([]byte, error) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	return json.Marshal(metadata)
}
