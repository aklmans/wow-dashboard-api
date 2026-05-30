// Package domain contains system-event domain types shared by service and store adapters.
package domain

import (
	"time"

	"github.com/google/uuid"
)

type Event struct {
	ID        uuid.UUID
	EventType string
	Message   string
	Metadata  map[string]any
	CreatedAt time.Time
}

type ListEventsInput struct {
	// Limit is the SQL row limit to fetch. The service requests one extra row
	// beyond the page size to detect whether a further page exists.
	Limit int
	// EventType, when set, filters to a single stable event type.
	EventType *string
	// CreatedAfter / CreatedBefore bound the created_at range (inclusive lower,
	// exclusive upper).
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
	// CursorCreatedAt / CursorID page strictly past a previous page's last row
	// under (created_at DESC, id DESC) ordering. Both are set together.
	CursorCreatedAt *time.Time
	CursorID        *uuid.UUID
}

type ListEventsResult struct {
	Events []Event
	Limit  int
	// HasMore reports whether more events exist past this page (i.e. the store
	// returned the extra probe row).
	HasMore bool
}
