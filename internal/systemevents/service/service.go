// Package service implements read-only system audit event use cases.
package service

import (
	"context"
	"errors"
	"fmt"

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
}

type EventStore interface {
	ListEvents(ctx context.Context, input domain.ListEventsInput) (domain.ListEventsResult, error)
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

	limit, err := normalizeLimit(input)
	if err != nil {
		return domain.ListEventsResult{}, err
	}

	result, err := s.store.ListEvents(ctx, domain.ListEventsInput{Limit: limit})
	if err != nil {
		return domain.ListEventsResult{}, err
	}
	result.Limit = limit
	if result.Events == nil {
		result.Events = []domain.Event{}
	}
	return result, nil
}

func normalizeLimit(input ListEventsInput) (int, error) {
	if !input.LimitProvided {
		return defaultListLimit, nil
	}
	if input.Limit < 1 || input.Limit > maxListLimit {
		return 0, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidInput, maxListLimit)
	}
	return input.Limit, nil
}
