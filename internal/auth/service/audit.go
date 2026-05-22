package service

import (
	"context"
	"log/slog"

	"github.com/go-chi/chi/v5/middleware"
)

const (
	EventAuthSignUpSucceeded = "auth.sign_up.succeeded"
	EventAuthSignUpFailed    = "auth.sign_up.failed"
	EventAuthSignInSucceeded = "auth.sign_in.succeeded"
	EventAuthSignInFailed    = "auth.sign_in.failed"

	AuditReasonInvalidInput       = "invalid_input"
	AuditReasonEmailAlreadyExists = "email_already_exists"
	AuditReasonInvalidCredentials = "invalid_credentials"
	AuditReasonUserDisabled       = "user_disabled"
	AuditReasonInternalError      = "internal_error"
)

// AuditMetadata is the safe, stable metadata shape stored for auth audit events.
type AuditMetadata struct {
	Email     string `json:"email,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	Role      string `json:"role,omitempty"`
	Reason    string `json:"reason,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// AuditEvent describes an auth audit event before persistence.
type AuditEvent struct {
	EventType string
	Message   string
	Metadata  AuditMetadata
}

// AuditRecorder records auth audit events.
type AuditRecorder interface {
	RecordAuthEvent(ctx context.Context, event AuditEvent) error
}

type noopAuditRecorder struct{}

func (noopAuditRecorder) RecordAuthEvent(context.Context, AuditEvent) error {
	return nil
}

func (s *Service) recordAudit(ctx context.Context, event AuditEvent) {
	recorder := s.auditRecorder
	if recorder == nil {
		recorder = noopAuditRecorder{}
	}
	event.Metadata = withAuditRequestID(ctx, event.Metadata)
	if err := recorder.RecordAuthEvent(ctx, event); err != nil {
		slog.ErrorContext(ctx, "failed to record auth audit event",
			"event_type", event.EventType,
			"error", err,
		)
	}
}

func (s *Service) recordSignUpSucceeded(ctx context.Context, metadata AuditMetadata) {
	s.recordAudit(ctx, AuditEvent{
		EventType: EventAuthSignUpSucceeded,
		Message:   "Auth sign-up succeeded.",
		Metadata:  metadata,
	})
}

func (s *Service) recordSignUpFailed(ctx context.Context, metadata AuditMetadata) {
	s.recordAudit(ctx, AuditEvent{
		EventType: EventAuthSignUpFailed,
		Message:   "Auth sign-up failed.",
		Metadata:  metadata,
	})
}

func (s *Service) recordSignInSucceeded(ctx context.Context, metadata AuditMetadata) {
	s.recordAudit(ctx, AuditEvent{
		EventType: EventAuthSignInSucceeded,
		Message:   "Auth sign-in succeeded.",
		Metadata:  metadata,
	})
}

func (s *Service) recordSignInFailed(ctx context.Context, metadata AuditMetadata) {
	s.recordAudit(ctx, AuditEvent{
		EventType: EventAuthSignInFailed,
		Message:   "Auth sign-in failed.",
		Metadata:  metadata,
	})
}

func withAuditRequestID(ctx context.Context, metadata AuditMetadata) AuditMetadata {
	if metadata.RequestID == "" {
		metadata.RequestID = middleware.GetReqID(ctx)
	}
	return metadata
}
