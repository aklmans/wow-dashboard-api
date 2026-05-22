// Package email defines the transactional email port and a development
// sender. The auth flows depend only on the Sender interface, so a real
// provider (SMTP, SES, …) can be wired in later without touching them.
package email

import (
	"context"
	"log/slog"
)

// Message is a transactional email to deliver.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Sender delivers transactional email. Implementations must be safe for
// concurrent use.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// LogSender is a development Sender that logs each message instead of
// delivering it. It lets the password-reset and email-verification flows run
// end to end before a real provider is configured.
type LogSender struct{}

// Send logs the message and reports success.
func (LogSender) Send(ctx context.Context, msg Message) error {
	slog.InfoContext(ctx, "transactional email (log sender — not delivered)",
		"to", msg.To,
		"subject", msg.Subject,
		"body", msg.Body,
	)
	return nil
}
