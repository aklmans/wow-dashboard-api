package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/riverqueue/river"

	"github.com/aklmans/wow-dashboard-api/internal/email"
)

// recordingSender is a test email.Sender that captures the last message and
// returns a configurable error.
type recordingSender struct {
	calls int
	last  email.Message
	err   error
}

func (r *recordingSender) Send(_ context.Context, msg email.Message) error {
	r.calls++
	r.last = msg
	return r.err
}

// TestSendEmailArgs_Kind locks in the persisted kind string so a rename does
// not silently strand jobs queued under the old name.
func TestSendEmailArgs_Kind(t *testing.T) {
	if got := (SendEmailArgs{}).Kind(); got != "send_email" {
		t.Fatalf("Kind() = %q, want %q", got, "send_email")
	}
}

func TestSendEmailWorker_DeliversMappedMessage(t *testing.T) {
	sender := &recordingSender{}
	w := &SendEmailWorker{sender: sender}

	job := &river.Job[SendEmailArgs]{Args: SendEmailArgs{
		To:      "user@example.test",
		Subject: "Reset your password",
		Body:    "Click here",
	}}
	if err := w.Work(context.Background(), job); err != nil {
		t.Fatalf("Work() returned error: %v", err)
	}

	if sender.calls != 1 {
		t.Fatalf("sender called %d times, want 1", sender.calls)
	}
	want := email.Message{To: "user@example.test", Subject: "Reset your password", Body: "Click here"}
	if sender.last != want {
		t.Fatalf("delivered message = %+v, want %+v", sender.last, want)
	}
}

func TestSendEmailWorker_PropagatesSenderError(t *testing.T) {
	sendErr := errors.New("smtp unavailable")
	w := &SendEmailWorker{sender: &recordingSender{err: sendErr}}

	err := w.Work(context.Background(), &river.Job[SendEmailArgs]{Args: SendEmailArgs{To: "user@example.test"}})
	if !errors.Is(err, sendErr) {
		t.Fatalf("Work() error = %v, want it to wrap %v", err, sendErr)
	}
}

func TestSendEmailWorker_NilSenderErrors(t *testing.T) {
	w := &SendEmailWorker{}

	if err := w.Work(context.Background(), &river.Job[SendEmailArgs]{}); err == nil {
		t.Fatal("Work() should error when no transport sender is configured")
	}
}

func TestAsyncEmailSender_NilClientErrors(t *testing.T) {
	// NewAsyncEmailSender tolerates a nil client (cmd/worker can boot degraded);
	// the failure must surface at Send time rather than panicking.
	s := NewAsyncEmailSender(nil)

	if err := s.Send(context.Background(), email.Message{To: "user@example.test"}); err == nil {
		t.Fatal("Send() should error when no River client is configured")
	}
}
