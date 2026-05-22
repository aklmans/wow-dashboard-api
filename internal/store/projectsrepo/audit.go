package projectsrepo

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	projectservice "github.com/aklmans/wow-dashboard-api/internal/projects/service"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
)

// SystemEventStore is the subset of generated queries required to persist a
// projects audit event into the system_events table.
type SystemEventStore interface {
	CreateSystemEvent(ctx context.Context, arg query.CreateSystemEventParams) (query.SystemEvent, error)
}

type systemEventRecorder struct {
	store SystemEventStore
}

type noopProjectAuditRecorder struct{}

func (noopProjectAuditRecorder) RecordProjectEvent(context.Context, projectservice.AuditEvent) error {
	return nil
}

// NewSystemEventRecorder returns a projectservice.AuditRecorder backed by the
// system_events table. Passing a nil store yields a no-op recorder so callers
// can wire the service even when audit persistence is intentionally disabled.
func NewSystemEventRecorder(store SystemEventStore) projectservice.AuditRecorder {
	if store == nil {
		return noopProjectAuditRecorder{}
	}
	return systemEventRecorder{store: store}
}

func (r systemEventRecorder) RecordProjectEvent(ctx context.Context, event projectservice.AuditEvent) error {
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
