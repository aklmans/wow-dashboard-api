//go:build integration

package usersrepo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/aklmans/wow-dashboard-api/internal/store/storetest"
	"github.com/aklmans/wow-dashboard-api/internal/store/usersrepo"
	"github.com/aklmans/wow-dashboard-api/internal/users/domain"
	userservice "github.com/aklmans/wow-dashboard-api/internal/users/service"
)

// TestUsersUnitOfWorkIntegration proves the users unit of work is genuinely
// transactional against a real database: an error inside the work function
// rolls back the mutation, and a successful function commits the mutation and
// its audit event together.
func TestUsersUnitOfWorkIntegration(t *testing.T) {
	ctx := context.Background()
	pool := storetest.NewPostgresPool(t, ctx, "test_users_uow_db", "../../../migrations")
	queries := query.New(pool)

	now := time.Now().UTC().Truncate(time.Microsecond)
	userID := uuid.New()
	var pgID pgtype.UUID
	if err := pgID.Scan(userID.String()); err != nil {
		t.Fatalf("scan user id: %v", err)
	}
	if _, err := queries.CreateUser(ctx, query.CreateUserParams{
		ID:           pgID,
		Email:        "uow@example.com",
		DisplayName:  "UoW User",
		PasswordHash: "placeholder-hash",
		Status:       "active",
		CreatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
		UpdatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
	}); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	uow := usersrepo.NewUnitOfWork(pool)
	disabled := domain.UserStatusDisabled
	update := domain.UpdateUserInput{ID: userID, Status: &disabled, UpdatedAt: now}

	statusOf := func() string {
		t.Helper()
		var status string
		if err := pool.QueryRow(ctx, "SELECT status FROM users WHERE id = $1", pgID).Scan(&status); err != nil {
			t.Fatalf("read user status: %v", err)
		}
		return status
	}
	auditCount := func() int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM system_events WHERE event_type = $1", userservice.EventUserUpdated).Scan(&n); err != nil {
			t.Fatalf("count audit events: %v", err)
		}
		return n
	}

	// --- An error inside the work function rolls back the mutation ---
	t.Run("rollback", func(t *testing.T) {
		wantErr := errors.New("boom")
		err := uow.Do(ctx, func(ctx context.Context, deps userservice.WorkDeps) error {
			if _, err := deps.Users.UpdateUser(ctx, update); err != nil {
				return err
			}
			return wantErr // simulate an audit failure after the mutation
		})
		if !errors.Is(err, wantErr) {
			t.Fatalf("Do error = %v, want %v", err, wantErr)
		}
		if got := statusOf(); got != "active" {
			t.Fatalf("status = %q after rollback, want unchanged %q", got, "active")
		}
		if n := auditCount(); n != 0 {
			t.Fatalf("audit events = %d after rollback, want 0", n)
		}
	})

	// --- A successful function commits the mutation and its audit together ---
	t.Run("commit", func(t *testing.T) {
		err := uow.Do(ctx, func(ctx context.Context, deps userservice.WorkDeps) error {
			if _, err := deps.Users.UpdateUser(ctx, update); err != nil {
				return err
			}
			return deps.Audit.RecordUserEvent(ctx, userservice.AuditEvent{
				EventType: userservice.EventUserUpdated,
				Message:   "User updated.",
				Metadata:  userservice.AuditMetadata{TargetUserID: userID.String()},
			})
		})
		if err != nil {
			t.Fatalf("Do returned error: %v", err)
		}
		if got := statusOf(); got != "disabled" {
			t.Fatalf("status = %q after commit, want %q", got, "disabled")
		}
		if n := auditCount(); n != 1 {
			t.Fatalf("audit events = %d after commit, want 1 (committed in the same tx)", n)
		}
	})
}
