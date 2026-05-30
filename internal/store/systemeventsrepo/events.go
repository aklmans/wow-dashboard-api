// Package systemeventsrepo is the PostgreSQL-backed adapter for system audit events.
package systemeventsrepo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/aklmans/wow-dashboard-api/internal/systemevents/domain"
)

type EventStore struct {
	queries *query.Queries
}

func NewEventStore(q *query.Queries) *EventStore {
	return &EventStore{queries: q}
}

func NewEventStoreFromDB(db query.DBTX) *EventStore {
	return NewEventStore(query.New(db))
}

func (s *EventStore) ListEvents(ctx context.Context, input domain.ListEventsInput) (domain.ListEventsResult, error) {
	if s.queries == nil {
		return domain.ListEventsResult{}, fmt.Errorf("systemeventsrepo: queries is nil")
	}

	rows, err := s.queries.ListSystemEventsPage(ctx, query.ListSystemEventsPageParams{
		EventType:       pgTextPtr(input.EventType),
		CreatedAfter:    pgTimestampPtr(input.CreatedAfter),
		CreatedBefore:   pgTimestampPtr(input.CreatedBefore),
		CursorCreatedAt: pgTimestampPtr(input.CursorCreatedAt),
		CursorID:        pgUUIDPtr(input.CursorID),
		RowLimit:        int32(input.Limit),
	})
	if err != nil {
		return domain.ListEventsResult{}, fmt.Errorf("systemeventsrepo: list events: %w", err)
	}

	events := make([]domain.Event, 0, len(rows))
	for _, row := range rows {
		event, err := eventFromRow(row)
		if err != nil {
			return domain.ListEventsResult{}, fmt.Errorf("systemeventsrepo: convert event: %w", err)
		}
		events = append(events, event)
	}

	// Limit and HasMore are part of the response contract owned by the service
	// layer, which requests an extra probe row and trims; the store only returns
	// the rows the query produced.
	return domain.ListEventsResult{Events: events}, nil
}

func pgTextPtr(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func pgTimestampPtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func pgUUIDPtr(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *id, Valid: true}
}

func eventFromRow(row query.SystemEvent) (domain.Event, error) {
	id, err := domainUUID(row.ID)
	if err != nil {
		return domain.Event{}, err
	}
	createdAt, err := domainTimestamp(row.CreatedAt)
	if err != nil {
		return domain.Event{}, err
	}
	return domain.Event{
		ID:        id,
		EventType: row.EventType,
		Message:   row.Message,
		Metadata:  metadataObject(row.Metadata),
		CreatedAt: createdAt,
	}, nil
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

func domainUUID(id pgtype.UUID) (uuid.UUID, error) {
	if !id.Valid {
		return uuid.Nil, fmt.Errorf("invalid system event id")
	}
	return uuid.UUID(id.Bytes), nil
}

func domainTimestamp(t pgtype.Timestamptz) (time.Time, error) {
	if !t.Valid {
		return time.Time{}, fmt.Errorf("invalid system event timestamp")
	}
	return t.Time, nil
}
