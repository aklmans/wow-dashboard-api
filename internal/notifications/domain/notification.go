// Package domain contains notification domain types shared by the service and
// store adapters. A notification is a per-user message surfaced in the app's
// notification bell.
package domain

import (
	"time"

	"github.com/google/uuid"
)

type Notification struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	Type     string
	Title    string
	Body     string
	Metadata map[string]any
	// ReadAt is nil while the notification is unread.
	ReadAt    *time.Time
	CreatedAt time.Time
}

type ListInput struct {
	// UserID scopes the query to a single owner.
	UserID uuid.UUID
	// Limit is the SQL row limit to fetch. The service requests one extra row
	// beyond the page size to detect whether a further page exists.
	Limit int
	// UnreadOnly restricts the page to unread notifications.
	UnreadOnly bool
	// CursorCreatedAt / CursorID page strictly past a previous page's last row
	// under (created_at DESC, id DESC) ordering. Both are set together.
	CursorCreatedAt *time.Time
	CursorID        *uuid.UUID
}

type ListResult struct {
	Notifications []Notification
	Limit         int
	// HasMore reports whether more rows exist past this page (the store returned
	// the extra probe row).
	HasMore bool
	// UnreadCount is the owner's total unread count, independent of this page.
	UnreadCount int64
}

type CreateInput struct {
	UserID   uuid.UUID
	Type     string
	Title    string
	Body     string
	Metadata map[string]any
}
