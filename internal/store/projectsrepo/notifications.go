package projectsrepo

import (
	"context"

	"github.com/google/uuid"

	notificationsdomain "github.com/aklmans/wow-dashboard-api/internal/notifications/domain"
	"github.com/aklmans/wow-dashboard-api/internal/store/notificationsrepo"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
)

// txNotificationEmitter adapts the notifications store to the projects service's
// NotificationEmitter. Built from the unit of work's transaction-scoped
// queries, so a membership change and the recipient's notification share one
// transaction.
type txNotificationEmitter struct {
	store *notificationsrepo.NotificationStore
}

func newTxNotificationEmitter(q *query.Queries) txNotificationEmitter {
	return txNotificationEmitter{store: notificationsrepo.NewNotificationStore(q)}
}

func (e txNotificationEmitter) Emit(ctx context.Context, userID uuid.UUID, notificationType, title, body string, metadata map[string]any) error {
	_, err := e.store.Create(ctx, notificationsdomain.CreateInput{
		UserID:   userID,
		Type:     notificationType,
		Title:    title,
		Body:     body,
		Metadata: metadata,
	})
	return err
}
