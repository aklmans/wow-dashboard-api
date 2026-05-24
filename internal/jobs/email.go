package jobs

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/aklmans/wow-dashboard-api/internal/email"
)

// SendEmailArgs is the queue payload for a single email send. Field names
// match email.Message so the wrapper-to-worker boundary stays trivial.
type SendEmailArgs struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// Kind is the persisted job type. Renaming strands jobs already queued under
// the old name; bump only with a migration plan.
func (SendEmailArgs) Kind() string { return "send_email" }

// SendEmailWorker delivers each SendEmailArgs through the configured
// transport (SMTP in production, LogSender in dev/tests).
type SendEmailWorker struct {
	river.WorkerDefaults[SendEmailArgs]
	sender email.Sender
}

func (w *SendEmailWorker) Work(ctx context.Context, job *river.Job[SendEmailArgs]) error {
	if w.sender == nil {
		return fmt.Errorf("jobs: SendEmailWorker has no transport sender configured")
	}
	return w.sender.Send(ctx, email.Message{
		To:      job.Args.To,
		Subject: job.Args.Subject,
		Body:    job.Args.Body,
	})
}

// AsyncEmailSender implements email.Sender by enqueueing each send as a
// background job. The API process holds one of these so request handlers
// never block on SMTP latency or outages.
type AsyncEmailSender struct {
	client *river.Client[pgx.Tx]
}

// NewAsyncEmailSender wraps a River insert client so that calls to Send
// drop a SendEmailArgs job on the default queue.
func NewAsyncEmailSender(client *river.Client[pgx.Tx]) *AsyncEmailSender {
	return &AsyncEmailSender{client: client}
}

// Send enqueues msg. The actual delivery happens in cmd/worker.
func (s *AsyncEmailSender) Send(ctx context.Context, msg email.Message) error {
	if s.client == nil {
		return fmt.Errorf("jobs: AsyncEmailSender has no River client configured")
	}
	if _, err := s.client.Insert(ctx, SendEmailArgs{
		To:      msg.To,
		Subject: msg.Subject,
		Body:    msg.Body,
	}, nil); err != nil {
		return fmt.Errorf("jobs: enqueue send_email: %w", err)
	}
	return nil
}
