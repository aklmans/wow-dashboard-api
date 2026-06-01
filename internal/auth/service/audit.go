package service

import (
	"context"
	"fmt"
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
	// EventAuthPasswordResetRequested is recorded when a password reset link is
	// issued for an active account (the forgot-password flow). It gives operators
	// visibility into reset-request volume — e.g. an email-bombing attempt.
	EventAuthPasswordResetRequested = "auth.password.reset_requested"
	EventAuthPasswordReset          = "auth.password.reset"
	// EventAuthPasswordResetFailed is recorded when a reset token is rejected
	// (invalid, expired, or already used) — a signal of token brute-forcing.
	EventAuthPasswordResetFailed = "auth.password.reset_failed"
	EventAuthEmailVerified       = "auth.email.verified"
	// EventAuthEmailVerificationFailed is recorded when a verification token is
	// rejected — a signal of token brute-forcing.
	EventAuthEmailVerificationFailed = "auth.email.verification_failed"
	// EventAuthImpersonationStarted / Stopped bracket an admin acting as another
	// user. Together with their timestamps they make the impersonation window
	// auditable: which admin acted as which user, and when.
	EventAuthImpersonationStarted = "auth.impersonation.started"
	EventAuthImpersonationStopped = "auth.impersonation.stopped"
	// EventAuthOtherSessionsRevoked is recorded when a user signs out their other
	// sessions, keeping only the calling device.
	EventAuthOtherSessionsRevoked = "auth.sessions.revoked_others"

	AuditReasonInvalidInput       = "invalid_input"
	AuditReasonEmailAlreadyExists = "email_already_exists"
	AuditReasonInvalidCredentials = "invalid_credentials"
	AuditReasonUserDisabled       = "user_disabled"
	AuditReasonAccountLocked      = "account_locked"
	AuditReasonInvalidToken       = "invalid_token"
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
	// ActorUserID / TargetUserID identify the admin and the impersonated user on
	// impersonation events.
	ActorUserID  string `json:"actor_user_id,omitempty"`
	TargetUserID string `json:"target_user_id,omitempty"`
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

func (s *Service) recordPasswordResetRequested(ctx context.Context, metadata AuditMetadata) {
	s.recordAudit(ctx, AuditEvent{
		EventType: EventAuthPasswordResetRequested,
		Message:   "Auth password reset requested.",
		Metadata:  metadata,
	})
}

func (s *Service) recordPasswordReset(ctx context.Context, metadata AuditMetadata) {
	s.recordAudit(ctx, AuditEvent{
		EventType: EventAuthPasswordReset,
		Message:   "Auth password reset.",
		Metadata:  metadata,
	})
}

func (s *Service) recordPasswordResetFailed(ctx context.Context, metadata AuditMetadata) {
	s.recordAudit(ctx, AuditEvent{
		EventType: EventAuthPasswordResetFailed,
		Message:   "Auth password reset failed.",
		Metadata:  metadata,
	})
}

func (s *Service) recordEmailVerified(ctx context.Context, metadata AuditMetadata) {
	s.recordAudit(ctx, AuditEvent{
		EventType: EventAuthEmailVerified,
		Message:   "Auth email verified.",
		Metadata:  metadata,
	})
}

func (s *Service) recordEmailVerificationFailed(ctx context.Context, metadata AuditMetadata) {
	s.recordAudit(ctx, AuditEvent{
		EventType: EventAuthEmailVerificationFailed,
		Message:   "Auth email verification failed.",
		Metadata:  metadata,
	})
}

func (s *Service) recordOtherSessionsRevoked(ctx context.Context, metadata AuditMetadata) {
	s.recordAudit(ctx, AuditEvent{
		EventType: EventAuthOtherSessionsRevoked,
		Message:   "Other sessions revoked.",
		Metadata:  metadata,
	})
}

func withAuditRequestID(ctx context.Context, metadata AuditMetadata) AuditMetadata {
	if metadata.RequestID == "" {
		metadata.RequestID = middleware.GetReqID(ctx)
	}
	return metadata
}

// recordAuthEventTx records an auth event on a transaction-scoped recorder,
// stamping the request id and masking the email, and returns the error so a
// unit of work rolls back when the audit write fails. It is the transactional
// counterpart of recordAudit (which is best-effort and only logs), used for
// success events that must be atomic with their mutation.
func recordAuthEventTx(ctx context.Context, recorder AuditRecorder, eventType string, message string, metadata AuditMetadata) error {
	if recorder == nil {
		return fmt.Errorf("auth: unit of work missing audit recorder")
	}
	metadata = withAuditRequestID(ctx, metadata)
	metadata.Email = maskEmail(metadata.Email)
	return recorder.RecordAuthEvent(ctx, AuditEvent{
		EventType: eventType,
		Message:   message,
		Metadata:  metadata,
	})
}
