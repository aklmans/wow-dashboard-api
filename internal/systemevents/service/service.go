// Package service implements read-only system audit event use cases.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/systemevents/domain"
)

var ErrInvalidInput = errors.New("systemevents: invalid input")

const (
	defaultListLimit = 20
	maxListLimit     = 100
)

type ListEventsInput struct {
	Limit         int
	LimitProvided bool
	// EventType filters to a single event type when non-empty.
	EventType string
	// CreatedAfter / CreatedBefore bound the created_at range.
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
	// CursorCreatedAt / CursorID continue from a previous page (decoded from the
	// opaque cursor by the handler). Both set together or both nil.
	CursorCreatedAt *time.Time
	CursorID        *uuid.UUID
}

// ListUserActivityInput scopes the activity feed to one user's own auth events.
type ListUserActivityInput struct {
	UserID          uuid.UUID
	Limit           int
	LimitProvided   bool
	CursorCreatedAt *time.Time
	CursorID        *uuid.UUID
}

type EventStore interface {
	ListEvents(ctx context.Context, input domain.ListEventsInput) (domain.ListEventsResult, error)
	ListUserActivity(ctx context.Context, input domain.ListUserActivityInput) (domain.ListEventsResult, error)
}

type Service struct {
	store EventStore
}

func NewService(store EventStore) *Service {
	return &Service{store: store}
}

func (s *Service) ListEvents(ctx context.Context, input ListEventsInput) (domain.ListEventsResult, error) {
	if s.store == nil {
		return domain.ListEventsResult{}, fmt.Errorf("systemevents: store is nil")
	}

	limit, err := normalizeLimit(input.Limit, input.LimitProvided)
	if err != nil {
		return domain.ListEventsResult{}, err
	}

	if input.CreatedAfter != nil && input.CreatedBefore != nil && !input.CreatedBefore.After(*input.CreatedAfter) {
		return domain.ListEventsResult{}, fmt.Errorf("%w: 'before' must be after 'after'", ErrInvalidInput)
	}

	// Fetch one extra row beyond the page size to detect whether a further page
	// exists, then trim it off the returned page.
	result, err := s.store.ListEvents(ctx, domain.ListEventsInput{
		Limit:           limit + 1,
		EventType:       normalizeEventType(input.EventType),
		CreatedAfter:    input.CreatedAfter,
		CreatedBefore:   input.CreatedBefore,
		CursorCreatedAt: input.CursorCreatedAt,
		CursorID:        input.CursorID,
	})
	if err != nil {
		return domain.ListEventsResult{}, err
	}

	if len(result.Events) > limit {
		result.Events = result.Events[:limit]
		result.HasMore = true
	}
	result.Limit = limit
	if result.Events == nil {
		result.Events = []domain.Event{}
	}
	return result, nil
}

// ListUserActivity returns one user's own auth audit events (their "security
// activity"), keyset-paginated newest first, with the same probe-row HasMore
// detection as ListEvents.
func (s *Service) ListUserActivity(ctx context.Context, input ListUserActivityInput) (domain.ListEventsResult, error) {
	if s.store == nil {
		return domain.ListEventsResult{}, fmt.Errorf("systemevents: store is nil")
	}

	limit, err := normalizeLimit(input.Limit, input.LimitProvided)
	if err != nil {
		return domain.ListEventsResult{}, err
	}

	result, err := s.store.ListUserActivity(ctx, domain.ListUserActivityInput{
		UserID:          input.UserID,
		Limit:           limit + 1,
		CursorCreatedAt: input.CursorCreatedAt,
		CursorID:        input.CursorID,
	})
	if err != nil {
		return domain.ListEventsResult{}, err
	}

	if len(result.Events) > limit {
		result.Events = result.Events[:limit]
		result.HasMore = true
	}
	result.Limit = limit
	if result.Events == nil {
		result.Events = []domain.Event{}
	}
	return result, nil
}

func normalizeEventType(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func normalizeLimit(limit int, provided bool) (int, error) {
	if !provided {
		return defaultListLimit, nil
	}
	if limit < 1 || limit > maxListLimit {
		return 0, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidInput, maxListLimit)
	}
	return limit, nil
}
