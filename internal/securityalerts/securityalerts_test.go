package securityalerts_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	authdomain "github.com/aklmans/wow-dashboard-api/internal/auth/domain"
	"github.com/aklmans/wow-dashboard-api/internal/email"
	notificationsdomain "github.com/aklmans/wow-dashboard-api/internal/notifications/domain"
	"github.com/aklmans/wow-dashboard-api/internal/securityalerts"
)

type fakeUserLookup struct {
	user authdomain.User
	err  error
}

func (f fakeUserLookup) GetUserByID(_ context.Context, _ uuid.UUID) (authdomain.User, error) {
	return f.user, f.err
}

type fakeEmail struct {
	sent []email.Message
	err  error
}

func (f *fakeEmail) Send(_ context.Context, msg email.Message) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, msg)
	return nil
}

type fakeNotifications struct {
	created []notificationsdomain.CreateInput
	err     error
}

func (f *fakeNotifications) Create(_ context.Context, input notificationsdomain.CreateInput) (notificationsdomain.Notification, error) {
	if f.err != nil {
		return notificationsdomain.Notification{}, f.err
	}
	f.created = append(f.created, input)
	return notificationsdomain.Notification{}, nil
}

func TestNotifierPasswordChanged(t *testing.T) {
	userID := uuid.New()
	users := fakeUserLookup{user: authdomain.User{ID: userID, Email: "demo@example.com", DisplayName: "Demo User"}}
	mailer := &fakeEmail{}
	notifs := &fakeNotifications{}
	n := securityalerts.NewNotifier(users, mailer, notifs, "WOW Dashboard")

	n.PasswordChanged(context.Background(), userID)

	if len(mailer.sent) != 1 {
		t.Fatalf("emails sent = %d, want 1", len(mailer.sent))
	}
	msg := mailer.sent[0]
	if msg.To != "demo@example.com" {
		t.Errorf("email To = %q, want demo@example.com", msg.To)
	}
	if msg.Subject != "Your password was changed" {
		t.Errorf("subject = %q", msg.Subject)
	}
	if !strings.Contains(msg.Body, "Hi Demo User,") {
		t.Errorf("body missing personalised greeting: %q", msg.Body)
	}
	if !strings.Contains(msg.Body, "WOW Dashboard") {
		t.Errorf("body missing app name: %q", msg.Body)
	}
	if len(notifs.created) != 1 ||
		notifs.created[0].Type != "auth.password.changed" ||
		notifs.created[0].UserID != userID {
		t.Fatalf("notification = %#v, want auth.password.changed for the user", notifs.created)
	}
}

func TestNotifierMfaDisabled(t *testing.T) {
	userID := uuid.New()
	mailer := &fakeEmail{}
	notifs := &fakeNotifications{}
	n := securityalerts.NewNotifier(
		fakeUserLookup{user: authdomain.User{ID: userID, Email: "demo@example.com"}},
		mailer, notifs, "WOW Dashboard")

	n.MfaDisabled(context.Background(), userID)

	if len(mailer.sent) != 1 || mailer.sent[0].Subject != "Two-factor authentication disabled" {
		t.Fatalf("email = %#v, want the MFA-disabled subject", mailer.sent)
	}
	// No display name → a generic greeting.
	if !strings.Contains(mailer.sent[0].Body, "Hi there,") {
		t.Errorf("body missing generic greeting: %q", mailer.sent[0].Body)
	}
	if len(notifs.created) != 1 || notifs.created[0].Type != "auth.mfa.disabled" {
		t.Fatalf("notification = %#v, want auth.mfa.disabled", notifs.created)
	}
}

func TestNotifierSkipsEverythingWhenUserLookupFails(t *testing.T) {
	mailer := &fakeEmail{}
	notifs := &fakeNotifications{}
	n := securityalerts.NewNotifier(fakeUserLookup{err: errors.New("not found")}, mailer, notifs, "WOW")

	n.PasswordChanged(context.Background(), uuid.New())

	if len(mailer.sent) != 0 || len(notifs.created) != 0 {
		t.Fatal("delivered an alert despite a failed user lookup")
	}
}

func TestNotifierStillNotifiesWithoutAnEmailAddress(t *testing.T) {
	mailer := &fakeEmail{}
	notifs := &fakeNotifications{}
	n := securityalerts.NewNotifier(fakeUserLookup{user: authdomain.User{Email: ""}}, mailer, notifs, "WOW")

	n.MfaEnabled(context.Background(), uuid.New())

	if len(mailer.sent) != 0 {
		t.Error("sent an email despite an empty address")
	}
	if len(notifs.created) != 1 {
		t.Error("did not create the in-app notification")
	}
}
