// Package securityalerts delivers "something security-relevant happened to your
// account" notices — an email (queued through the email transport) plus an
// in-app notification — for events like a password change or an MFA toggle. It
// is wired into the auth and MFA services as a best-effort side effect: a
// delivery failure is logged, never propagated, so it can't fail the security
// operation that triggered it.
package securityalerts

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	authdomain "github.com/aklmans/wow-dashboard-api/internal/auth/domain"
	"github.com/aklmans/wow-dashboard-api/internal/email"
	notificationsdomain "github.com/aklmans/wow-dashboard-api/internal/notifications/domain"
)

// UserLookup resolves a user's email + display name from their id, so callers
// (e.g. the MFA service, which only holds a user id) don't have to.
type UserLookup interface {
	GetUserByID(ctx context.Context, id uuid.UUID) (authdomain.User, error)
}

// NotificationCreator writes one in-app notification.
type NotificationCreator interface {
	Create(ctx context.Context, input notificationsdomain.CreateInput) (notificationsdomain.Notification, error)
}

// Notifier composes the user lookup, email transport, and notification writer to
// deliver a security alert through both channels.
type Notifier struct {
	users         UserLookup
	email         email.Sender
	notifications NotificationCreator
	appName       string
}

// NewNotifier builds a Notifier. appName is used in the email copy (e.g. "your
// WOW Dashboard account").
func NewNotifier(users UserLookup, sender email.Sender, notifications NotificationCreator, appName string) *Notifier {
	if strings.TrimSpace(appName) == "" {
		appName = "your account"
	}
	return &Notifier{users: users, email: sender, notifications: notifications, appName: appName}
}

// alert is the resolved content delivered across both channels.
type alert struct {
	subject    string // email subject
	body       string // email body, after the greeting; already mentions appName
	notifType  string // stable notification type
	notifTitle string
	notifBody  string
}

// PasswordChanged alerts the user that their password was changed.
func (n *Notifier) PasswordChanged(ctx context.Context, userID uuid.UUID) {
	n.deliver(ctx, userID, alert{
		subject: "Your password was changed",
		body: fmt.Sprintf("Your %s account password was just changed.\n\n"+
			"If this was you, no action is needed. If it wasn't, reset your password "+
			"immediately and review your active sessions in your account settings.", n.appName),
		notifType:  "auth.password.changed",
		notifTitle: "Password changed",
		notifBody:  "Your account password was changed.",
	})
}

// MfaEnabled alerts the user that two-factor authentication was turned on.
func (n *Notifier) MfaEnabled(ctx context.Context, userID uuid.UUID) {
	n.deliver(ctx, userID, alert{
		subject: "Two-factor authentication enabled",
		body: fmt.Sprintf("Two-factor authentication was just turned on for your %s account. "+
			"You'll be asked for a code from your authenticator app when you sign in.\n\n"+
			"If this wasn't you, reset your password immediately.", n.appName),
		notifType:  "auth.mfa.enabled",
		notifTitle: "Two-factor authentication enabled",
		notifBody:  "Two-factor authentication was turned on for your account.",
	})
}

// MfaDisabled alerts the user that two-factor authentication was turned off — an
// account-takeover signal worth a prominent notice.
func (n *Notifier) MfaDisabled(ctx context.Context, userID uuid.UUID) {
	n.deliver(ctx, userID, alert{
		subject: "Two-factor authentication disabled",
		body: fmt.Sprintf("Two-factor authentication was just turned off for your %s account. "+
			"Your account is now protected by your password alone.\n\n"+
			"If this wasn't you, your account may be compromised — reset your password "+
			"immediately and turn two-factor authentication back on.", n.appName),
		notifType:  "auth.mfa.disabled",
		notifTitle: "Two-factor authentication disabled",
		notifBody:  "Two-factor authentication was turned off for your account.",
	})
}

// NewSignIn alerts the user that their account was signed in to from a device
// (User-Agent) not seen among their other active sessions.
func (n *Notifier) NewSignIn(ctx context.Context, userID uuid.UUID, userAgent, ipAddress string) {
	device := strings.TrimSpace(userAgent)
	if device == "" {
		device = "an unrecognised device"
	}
	ipLine := ""
	if ip := strings.TrimSpace(ipAddress); ip != "" {
		ipLine = "\nIP address: " + ip
	}
	n.deliver(ctx, userID, alert{
		subject: "New sign-in to your account",
		body: fmt.Sprintf("Your %s account was just signed in to from a device we haven't seen before.\n\n"+
			"Device: %s%s\n\n"+
			"If this was you, no action is needed. If it wasn't, reset your password "+
			"immediately and review your active sessions in your account settings.",
			n.appName, device, ipLine),
		notifType:  "auth.sign_in.new_device",
		notifTitle: "New sign-in",
		notifBody:  "A new device signed in to your account.",
	})
}

func (n *Notifier) deliver(ctx context.Context, userID uuid.UUID, a alert) {
	user, err := n.users.GetUserByID(ctx, userID)
	if err != nil {
		slog.ErrorContext(ctx, "security alert: user lookup failed",
			"event", a.notifType, "error", err)
		return
	}

	if n.email != nil && strings.TrimSpace(user.Email) != "" {
		greeting := "Hi there,"
		if name := strings.TrimSpace(user.DisplayName); name != "" {
			greeting = "Hi " + name + ","
		}
		if err := n.email.Send(ctx, email.Message{
			To:      user.Email,
			Subject: a.subject,
			Body:    greeting + "\n\n" + a.body,
		}); err != nil {
			slog.ErrorContext(ctx, "security alert: email send failed",
				"event", a.notifType, "error", err)
		}
	}

	if n.notifications != nil {
		if _, err := n.notifications.Create(ctx, notificationsdomain.CreateInput{
			UserID: userID,
			Type:   a.notifType,
			Title:  a.notifTitle,
			Body:   a.notifBody,
		}); err != nil {
			slog.ErrorContext(ctx, "security alert: notification create failed",
				"event", a.notifType, "error", err)
		}
	}
}
