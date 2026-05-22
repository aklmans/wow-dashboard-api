package service

import (
	"context"
	"log/slog"

	"github.com/go-chi/chi/v5/middleware"
)

const (
	EventProjectCreated  = "projects.project.created"
	EventProjectUpdated  = "projects.project.updated"
	EventProjectArchived = "projects.project.archived"
)

// AuditMetadata is the safe, stable metadata shape stored for project audit
// events. It deliberately does NOT include project name, description, or any
// other free-form business text — only stable identifiers and the field names
// the caller provided.
type AuditMetadata struct {
	ProjectID     string   `json:"project_id,omitempty"`
	OwnerUserID   string   `json:"owner_user_id,omitempty"`
	Status        string   `json:"status,omitempty"`
	ChangedFields []string `json:"changed_fields,omitempty"`
	RequestID     string   `json:"request_id,omitempty"`
}

// AuditEvent describes a project audit event before persistence.
type AuditEvent struct {
	EventType string
	Message   string
	Metadata  AuditMetadata
}

// AuditRecorder records project audit events. Implementations must be safe
// for concurrent use and should treat persistence failures as recoverable —
// the service layer logs the error and continues.
type AuditRecorder interface {
	RecordProjectEvent(ctx context.Context, event AuditEvent) error
}

type noopAuditRecorder struct{}

func (noopAuditRecorder) RecordProjectEvent(context.Context, AuditEvent) error {
	return nil
}

// WithAuditRecorder configures best-effort projects audit event recording.
func WithAuditRecorder(recorder AuditRecorder) Option {
	return func(s *Service) {
		if recorder != nil {
			s.auditRecorder = recorder
		}
	}
}

func (s *Service) recordAudit(ctx context.Context, event AuditEvent) {
	recorder := s.auditRecorder
	if recorder == nil {
		recorder = noopAuditRecorder{}
	}
	event.Metadata = withAuditRequestID(ctx, event.Metadata)
	if err := recorder.RecordProjectEvent(ctx, event); err != nil {
		slog.ErrorContext(ctx, "failed to record projects audit event",
			"event_type", event.EventType,
			"error", err,
		)
	}
}

func (s *Service) recordProjectCreated(ctx context.Context, metadata AuditMetadata) {
	s.recordAudit(ctx, AuditEvent{
		EventType: EventProjectCreated,
		Message:   "Project created.",
		Metadata:  metadata,
	})
}

func (s *Service) recordProjectUpdated(ctx context.Context, metadata AuditMetadata) {
	s.recordAudit(ctx, AuditEvent{
		EventType: EventProjectUpdated,
		Message:   "Project updated.",
		Metadata:  metadata,
	})
}

func (s *Service) recordProjectArchived(ctx context.Context, metadata AuditMetadata) {
	s.recordAudit(ctx, AuditEvent{
		EventType: EventProjectArchived,
		Message:   "Project archived.",
		Metadata:  metadata,
	})
}

func withAuditRequestID(ctx context.Context, metadata AuditMetadata) AuditMetadata {
	if metadata.RequestID == "" {
		metadata.RequestID = middleware.GetReqID(ctx)
	}
	return metadata
}
