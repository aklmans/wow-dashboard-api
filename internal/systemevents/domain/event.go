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
	Limit int
}

type ListEventsResult struct {
	Events []Event
	Limit  int
}
