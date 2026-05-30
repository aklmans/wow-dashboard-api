package service

import (
	"context"
	"log/slog"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/aklmans/wow-dashboard-api/internal/roles/domain"
)

// Stable system event types recorded for role management actions.
const (
	EventRoleCreated = "roles.role.created"
	EventRoleUpdated = "roles.role.updated"
	EventRoleDeleted = "roles.role.deleted"
)

// AuditMetadata is the safe, stable metadata stored for role audit events. It
// carries only stable identifiers and catalog permission strings — never the
// admin-authored role name or description.
type AuditMetadata struct {
	RoleID      string   `json:"role_id,omitempty"`
	ActorUserID string   `json:"actor_user_id,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
	RequestID   string   `json:"request_id,omitempty"`
}

// AuditEvent describes a role management audit event before persistence.
type AuditEvent struct {
	EventType string
	Message   string
	Metadata  AuditMetadata
}

// AuditRecorder records role management audit events. Implementations must be
// safe for concurrent use and treat persistence failures as recoverable.
type AuditRecorder interface {
	RecordRoleEvent(ctx context.Context, event AuditEvent) error
}

type noopAuditRecorder struct{}

func (noopAuditRecorder) RecordRoleEvent(context.Context, AuditEvent) error {
	return nil
}

// WithAuditRecorder configures best-effort role management audit recording.
func WithAuditRecorder(recorder AuditRecorder) Option {
	return func(s *Service) {
		if recorder != nil {
			s.auditRecorder = recorder
		}
	}
}

// buildRoleEvent assembles a role audit event, stamping the request id from
// context. Shared by the best-effort and transactional recording paths so both
// produce identical events.
func buildRoleEvent(ctx context.Context, eventType string, message string, role domain.Role, actorUserID string) AuditEvent {
	return AuditEvent{
		EventType: eventType,
		Message:   message,
		Metadata: AuditMetadata{
			RoleID:      role.ID.String(),
			ActorUserID: actorUserID,
			Permissions: role.Permissions,
			RequestID:   middleware.GetReqID(ctx),
		},
	}
}

func (s *Service) recordRoleEvent(ctx context.Context, eventType string, message string, role domain.Role, actorUserID string) {
	event := buildRoleEvent(ctx, eventType, message, role, actorUserID)
	if err := s.auditRecorder.RecordRoleEvent(ctx, event); err != nil {
		slog.ErrorContext(ctx, "failed to record roles audit event",
			"event_type", event.EventType,
			"error", err,
		)
	}
}
