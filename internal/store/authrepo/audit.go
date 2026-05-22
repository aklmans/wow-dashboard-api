package authrepo

import (
	"context"
	"encoding/json"
	"time"

	authservice "github.com/aklmans/wow-dashboard-api/internal/auth/service"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/google/uuid"
)

type SystemEventStore interface {
	CreateSystemEvent(ctx context.Context, arg query.CreateSystemEventParams) (query.SystemEvent, error)
}

type systemEventRecorder struct {
	store SystemEventStore
}

type noopAuditRecorder struct{}

func (noopAuditRecorder) RecordAuthEvent(context.Context, authservice.AuditEvent) error {
	return nil
}

func NewSystemEventRecorder(store SystemEventStore) authservice.AuditRecorder {
	if store == nil {
		return noopAuditRecorder{}
	}
	return systemEventRecorder{store: store}
}

func (r systemEventRecorder) RecordAuthEvent(ctx context.Context, event authservice.AuditEvent) error {
	metadata, err := json.Marshal(event.Metadata)
	if err != nil {
		return err
	}

	_, err = r.store.CreateSystemEvent(ctx, query.CreateSystemEventParams{
		ID:        pgUUID(uuid.New()),
		EventType: event.EventType,
		Message:   event.Message,
		Metadata:  metadata,
		CreatedAt: pgTimestamp(time.Now().UTC().Truncate(time.Microsecond)),
	})
	return err
}
