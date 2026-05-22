package usersrepo

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	userservice "github.com/aklmans/wow-dashboard-api/internal/users/service"
)

// SystemEventStore is the subset of generated queries required to persist a
// users audit event into the system_events table.
type SystemEventStore interface {
	CreateSystemEvent(ctx context.Context, arg query.CreateSystemEventParams) (query.SystemEvent, error)
}

type systemEventRecorder struct {
	store SystemEventStore
}

type noopUserAuditRecorder struct{}

func (noopUserAuditRecorder) RecordUserEvent(context.Context, userservice.AuditEvent) error {
	return nil
}

// NewSystemEventRecorder returns a userservice.AuditRecorder backed by the
// system_events table. A nil store yields a no-op recorder so callers can wire
// the service even when audit persistence is intentionally disabled.
func NewSystemEventRecorder(store SystemEventStore) userservice.AuditRecorder {
	if store == nil {
		return noopUserAuditRecorder{}
	}
	return systemEventRecorder{store: store}
}

func (r systemEventRecorder) RecordUserEvent(ctx context.Context, event userservice.AuditEvent) error {
	metadata, err := json.Marshal(event.Metadata)
	if err != nil {
		return err
	}

	_, err = r.store.CreateSystemEvent(ctx, query.CreateSystemEventParams{
		ID:        pgtype.UUID{Bytes: uuid.New(), Valid: true},
		EventType: event.EventType,
		Message:   event.Message,
		Metadata:  metadata,
		CreatedAt: pgtype.Timestamptz{Time: time.Now().UTC().Truncate(time.Microsecond), Valid: true},
	})
	return err
}
