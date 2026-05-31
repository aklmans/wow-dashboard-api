package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/auth/domain"
	"github.com/aklmans/wow-dashboard-api/internal/auth/service"
	"github.com/aklmans/wow-dashboard-api/internal/auth/token"
)

func activeTargetStore(target uuid.UUID, roles, perms []string) *fakeUserStore {
	return &fakeUserStore{
		user:        domain.User{ID: target, Email: "target@example.test", DisplayName: "Target", Status: domain.UserStatusActive},
		roles:       roles,
		permissions: perms,
	}
}

func TestImpersonate(t *testing.T) {
	target := uuid.New()
	adminID := uuid.New().String()

	t.Run("admin impersonates a non-admin active user", func(t *testing.T) {
		store := activeTargetStore(target, []string{"user"}, []string{"users:read"})
		tm := &fakeTokenManager{issuedToken: "imp-token"}
		audit := &fakeAuditRecorder{}
		svc := service.NewService(store, tm, service.WithAuditRecorder(audit))

		session, err := svc.Impersonate(
			context.Background(),
			&service.PublicUser{ID: adminID, Email: "admin@example.test"},
			target.String(),
		)
		if err != nil {
			t.Fatalf("Impersonate: %v", err)
		}
		if session.AccessToken != "imp-token" {
			t.Fatalf("access token = %q, want imp-token", session.AccessToken)
		}
		// Critical: no refresh token, so the admin's own session is preserved.
		if session.RefreshToken != "" {
			t.Fatal("impersonation must not issue a refresh token")
		}
		if session.User.ID != target.String() {
			t.Fatalf("session user = %s, want target %s", session.User.ID, target)
		}
		if session.User.ImpersonatorID != adminID {
			t.Fatalf("ImpersonatorID = %q, want %q", session.User.ImpersonatorID, adminID)
		}
		if tm.impersonationTarget != target.String() || tm.impersonationActor != adminID {
			t.Fatalf("token issued for target/actor = %q/%q", tm.impersonationTarget, tm.impersonationActor)
		}
		if len(audit.events) != 1 || audit.events[0].EventType != service.EventAuthImpersonationStarted {
			t.Fatalf("audit = %#v, want one impersonation.started", audit.events)
		}
		if audit.events[0].Metadata.ActorUserID != adminID || audit.events[0].Metadata.TargetUserID != target.String() {
			t.Fatalf("audit metadata = %#v", audit.events[0].Metadata)
		}
	})

	t.Run("cannot impersonate an administrator", func(t *testing.T) {
		// Wildcard permission.
		svc := service.NewService(activeTargetStore(target, []string{"user"}, []string{"*"}), &fakeTokenManager{issuedToken: "x"})
		if _, err := svc.Impersonate(context.Background(), &service.PublicUser{ID: adminID}, target.String()); !errors.Is(err, service.ErrCannotImpersonate) {
			t.Fatalf("wildcard target err = %v, want ErrCannotImpersonate", err)
		}
		// "admin" role name.
		svc2 := service.NewService(activeTargetStore(target, []string{"admin"}, []string{"users:read"}), &fakeTokenManager{issuedToken: "x"})
		if _, err := svc2.Impersonate(context.Background(), &service.PublicUser{ID: adminID}, target.String()); !errors.Is(err, service.ErrCannotImpersonate) {
			t.Fatalf("admin-role target err = %v, want ErrCannotImpersonate", err)
		}
	})

	t.Run("cannot impersonate yourself", func(t *testing.T) {
		svc := service.NewService(activeTargetStore(target, []string{"user"}, nil), &fakeTokenManager{issuedToken: "x"})
		if _, err := svc.Impersonate(context.Background(), &service.PublicUser{ID: target.String()}, target.String()); !errors.Is(err, service.ErrCannotImpersonate) {
			t.Fatalf("self err = %v, want ErrCannotImpersonate", err)
		}
	})

	t.Run("cannot impersonate a disabled user", func(t *testing.T) {
		store := &fakeUserStore{user: domain.User{ID: target, Status: domain.UserStatusDisabled}, roles: []string{"user"}}
		svc := service.NewService(store, &fakeTokenManager{issuedToken: "x"})
		if _, err := svc.Impersonate(context.Background(), &service.PublicUser{ID: adminID}, target.String()); !errors.Is(err, service.ErrCannotImpersonate) {
			t.Fatalf("disabled err = %v, want ErrCannotImpersonate", err)
		}
	})
}

func TestCurrentUserSurfacesImpersonation(t *testing.T) {
	target := uuid.New()
	adminID := uuid.New()

	store := activeTargetStore(target, []string{"user"}, []string{"users:read"})
	claims := &token.Claims{Act: adminID.String()}
	claims.Subject = target.String()
	svc := service.NewService(store, &fakeTokenManager{claims: claims})

	user, err := svc.CurrentUser(context.Background(), "raw-impersonation-token")
	if err != nil {
		t.Fatalf("CurrentUser: %v", err)
	}
	if user.ID != target.String() {
		t.Fatalf("resolved user = %s, want the subject/target %s", user.ID, target)
	}
	if user.ImpersonatorID != adminID.String() {
		t.Fatalf("ImpersonatorID = %q, want the act claim %q", user.ImpersonatorID, adminID)
	}
}
