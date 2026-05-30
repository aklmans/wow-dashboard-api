// Package service implements per-user notification use cases: listing a user's
// feed, the unread count, marking read, and emitting new notifications.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/notifications/domain"
)

var ErrInvalidInput = errors.New("notifications: invalid input")

const (
	defaultListLimit = 20
	maxListLimit     = 100
)

type ListInput struct {
	UserID          uuid.UUID
	Limit           int
	LimitProvided   bool
	UnreadOnly      bool
	CursorCreatedAt *time.Time
	CursorID        *uuid.UUID
}

type Store interface {
	ListNotifications(ctx context.Context, input domain.ListInput) (domain.ListResult, error)
	CountUnread(ctx context.Context, userID uuid.UUID) (int64, error)
	MarkRead(ctx context.Context, userID, id uuid.UUID) (int64, error)
	MarkAllRead(ctx context.Context, userID uuid.UUID) (int64, error)
	Create(ctx context.Context, input domain.CreateInput) (domain.Notification, error)
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

// List returns a page of the user's notifications plus their total unread count.
func (s *Service) List(ctx context.Context, input ListInput) (domain.ListResult, error) {
	if s.store == nil {
		return domain.ListResult{}, fmt.Errorf("notifications: store is nil")
	}
	if input.UserID == uuid.Nil {
		return domain.ListResult{}, fmt.Errorf("%w: user id is required", ErrInvalidInput)
	}

	limit, err := normalizeLimit(input)
	if err != nil {
		return domain.ListResult{}, err
	}

	// Fetch one extra row beyond the page size to detect a further page, then
	// trim it off the returned page.
	result, err := s.store.ListNotifications(ctx, domain.ListInput{
		UserID:          input.UserID,
		Limit:           limit + 1,
		UnreadOnly:      input.UnreadOnly,
		CursorCreatedAt: input.CursorCreatedAt,
		CursorID:        input.CursorID,
	})
	if err != nil {
		return domain.ListResult{}, err
	}

	if len(result.Notifications) > limit {
		result.Notifications = result.Notifications[:limit]
		result.HasMore = true
	}
	result.Limit = limit
	if result.Notifications == nil {
		result.Notifications = []domain.Notification{}
	}

	unread, err := s.store.CountUnread(ctx, input.UserID)
	if err != nil {
		return domain.ListResult{}, err
	}
	result.UnreadCount = unread
	return result, nil
}

// MarkRead marks one notification read and returns the user's updated unread
// count. It is idempotent — marking a missing or already-read row is not an
// error (the count simply reflects reality).
func (s *Service) MarkRead(ctx context.Context, userID, id uuid.UUID) (int64, error) {
	if s.store == nil {
		return 0, fmt.Errorf("notifications: store is nil")
	}
	if userID == uuid.Nil || id == uuid.Nil {
		return 0, fmt.Errorf("%w: user id and notification id are required", ErrInvalidInput)
	}
	if _, err := s.store.MarkRead(ctx, userID, id); err != nil {
		return 0, err
	}
	return s.store.CountUnread(ctx, userID)
}

// MarkAllRead marks every unread notification read and returns the updated
// unread count (0 on success).
func (s *Service) MarkAllRead(ctx context.Context, userID uuid.UUID) (int64, error) {
	if s.store == nil {
		return 0, fmt.Errorf("notifications: store is nil")
	}
	if userID == uuid.Nil {
		return 0, fmt.Errorf("%w: user id is required", ErrInvalidInput)
	}
	if _, err := s.store.MarkAllRead(ctx, userID); err != nil {
		return 0, err
	}
	return s.store.CountUnread(ctx, userID)
}

// Create emits a notification for a user. Domain code calls this when something
// notable happens to that user.
func (s *Service) Create(ctx context.Context, input domain.CreateInput) (domain.Notification, error) {
	if s.store == nil {
		return domain.Notification{}, fmt.Errorf("notifications: store is nil")
	}
	if input.UserID == uuid.Nil {
		return domain.Notification{}, fmt.Errorf("%w: user id is required", ErrInvalidInput)
	}
	if strings.TrimSpace(input.Type) == "" || strings.TrimSpace(input.Title) == "" {
		return domain.Notification{}, fmt.Errorf("%w: type and title are required", ErrInvalidInput)
	}
	return s.store.Create(ctx, input)
}

func normalizeLimit(input ListInput) (int, error) {
	if !input.LimitProvided {
		return defaultListLimit, nil
	}
	if input.Limit < 1 || input.Limit > maxListLimit {
		return 0, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidInput, maxListLimit)
	}
	return input.Limit, nil
}
