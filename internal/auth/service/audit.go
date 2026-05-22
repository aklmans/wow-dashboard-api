package service

import (
	"context"
	"log/slog"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
)

const (
	EventAuthSignUpSucceeded = "auth.sign_up.succeeded"
	EventAuthSignUpFailed    = "auth.sign_up.failed"
	EventAuthSignInSucceeded = "auth.sign_in.succeeded"
	EventAuthSignInFailed    = "auth.sign_in.failed"
	EventAuthPasswordChanged = "auth.password.changed"

	AuditReasonInvalidInput       = "invalid_input"
	AuditReasonEmailAlreadyExists = "email_already_exists"
	AuditReasonInvalidCredentials = "invalid_credentials"
	AuditReasonUserDisabled       = "user_disabled"
	AuditReasonAccountLocked      = "account_locked"
	AuditReasonInternalError      = "internal_error"
)

// AuditMetadata is the safe, stable metadata shape stored for auth audit events.
type AuditMetadata struct {
	// Email is set by callers with the raw address. recordAudit masks it
	// before the event is persisted, so it is stored and serialized as
	// masked_email (e.g. "d***@example.com") — never the plaintext address.
	Email     string `json:"masked_email,omitempty"`
	UserID    string `json:"user_id,omitempty"`
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
	event.Metadata = withAuditRequestID(ctx, event.Metadata)
	event.Metadata.Email = maskEmail(event.Metadata.Email)
	if err := s.auditRecorder.RecordAuthEvent(ctx, event); err != nil {
		slog.ErrorContext(ctx, "failed to record auth audit event",
			"event_type", event.EventType,
			"error", err,
		)
	}
}

// maskEmail reduces an email address to a low-PII form safe for long-term
// audit storage: the first character of the local part, then "***", then the
// domain (e.g. "demo@example.com" -> "d***@example.com"). An empty value
// yields "" and a value without a usable local part yields "***".
func maskEmail(email string) string {
	if email == "" {
		return ""
	}
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 {
		return "***"
	}
	return email[:1] + "***" + email[at:]
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

func (s *Service) recordPasswordChanged(ctx context.Context, metadata AuditMetadata) {
	s.recordAudit(ctx, AuditEvent{
		EventType: EventAuthPasswordChanged,
		Message:   "Auth password changed.",
		Metadata:  metadata,
	})
}

func withAuditRequestID(ctx context.Context, metadata AuditMetadata) AuditMetadata {
	if metadata.RequestID == "" {
		metadata.RequestID = middleware.GetReqID(ctx)
	}
	return metadata
}
