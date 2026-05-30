package service

import (
	"context"
	"log/slog"

	"github.com/go-chi/chi/v5/middleware"
)

// EventUserUpdated is the stable system event type recorded when an admin
// changes another user's role or status.
const EventUserUpdated = "users.user.updated"

// AuditMetadata is the safe, stable metadata shape stored for user management
// audit events. It carries only stable identifiers, the changed field names,
// and the resulting role/status — never email or other PII.
type AuditMetadata struct {
	TargetUserID  string   `json:"target_user_id,omitempty"`
	ActorUserID   string   `json:"actor_user_id,omitempty"`
	ChangedFields []string `json:"changed_fields,omitempty"`
	Status        string   `json:"status,omitempty"`
	RoleIDs       []string `json:"role_ids,omitempty"`
	RequestID     string   `json:"request_id,omitempty"`
}

// AuditEvent describes a user management audit event before persistence.
type AuditEvent struct {
	EventType string
	Message   string
	Metadata  AuditMetadata
}

// AuditRecorder records user management audit events. Implementations must be
// safe for concurrent use and treat persistence failures as recoverable — the
// service logs the error and keeps the successful update result.
type AuditRecorder interface {
	RecordUserEvent(ctx context.Context, event AuditEvent) error
}

type noopAuditRecorder struct{}

func (noopAuditRecorder) RecordUserEvent(context.Context, AuditEvent) error {
	return nil
}

// WithAuditRecorder configures best-effort user management audit recording.
func WithAuditRecorder(recorder AuditRecorder) Option {
	return func(s *Service) {
		if recorder != nil {
			s.auditRecorder = recorder
		}
	}
}

// buildUserUpdatedEvent assembles the user-updated audit event, stamping the
// request id from context when absent. Shared by the best-effort and
// transactional recording paths so both produce identical events.
func buildUserUpdatedEvent(ctx context.Context, metadata AuditMetadata) AuditEvent {
	if metadata.RequestID == "" {
		metadata.RequestID = middleware.GetReqID(ctx)
	}
	return AuditEvent{
		EventType: EventUserUpdated,
		Message:   "User updated.",
		Metadata:  metadata,
	}
}

func (s *Service) recordUserUpdated(ctx context.Context, metadata AuditMetadata) {
	event := buildUserUpdatedEvent(ctx, metadata)
	if err := s.auditRecorder.RecordUserEvent(ctx, event); err != nil {
		slog.ErrorContext(ctx, "failed to record users audit event",
			"event_type", event.EventType,
			"error", err,
		)
	}
}
