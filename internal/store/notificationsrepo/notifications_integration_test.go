//go:build integration

package notificationsrepo_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/aklmans/wow-dashboard-api/internal/auth/password"
	"github.com/aklmans/wow-dashboard-api/internal/notifications/domain"
	"github.com/aklmans/wow-dashboard-api/internal/store/notificationsrepo"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/aklmans/wow-dashboard-api/internal/store/storetest"
)

func TestNotificationStoreRoundTripIntegration(t *testing.T) {
	ctx := context.Background()
	pool := storetest.NewPostgresPool(t, ctx, "test_notificationsrepo_db", "../../../migrations")
	queries := query.New(pool)
	store := notificationsrepo.NewNotificationStore(queries)

	alice := mustCreateUser(t, ctx, queries, "alice@example.com", "Alice")
	bob := mustCreateUser(t, ctx, queries, "bob@example.com", "Bob")

	first, err := store.Create(ctx, domain.CreateInput{
		UserID:   alice,
		Type:     "users.roles.updated",
		Title:    "Your roles were updated",
		Body:     "An administrator changed your roles.",
		Metadata: map[string]any{"role": "admin"},
	})
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	if first.ReadAt != nil {
		t.Fatal("a freshly created notification must be unread (ReadAt == nil)")
	}
	if _, err := store.Create(ctx, domain.CreateInput{UserID: alice, Type: "t.second", Title: "Second"}); err != nil {
		t.Fatalf("Create second: %v", err)
	}
	if _, err := store.Create(ctx, domain.CreateInput{UserID: bob, Type: "t.bob", Title: "Bob's"}); err != nil {
		t.Fatalf("Create bob: %v", err)
	}

	// List for Alice: newest first, Bob's notification excluded.
	res, err := store.ListNotifications(ctx, domain.ListInput{UserID: alice, Limit: 10})
	if err != nil {
		t.Fatalf("ListNotifications: %v", err)
	}
	if len(res.Notifications) != 2 {
		t.Fatalf("len = %d, want 2 (Bob's excluded by user scoping)", len(res.Notifications))
	}
	if res.Notifications[0].Title != "Second" {
		t.Fatalf("newest-first order wrong: first title = %q, want %q", res.Notifications[0].Title, "Second")
	}
	if res.Notifications[1].Metadata["role"] != "admin" {
		t.Fatalf("metadata not round-tripped: %#v", res.Notifications[1].Metadata)
	}

	if n, err := store.CountUnread(ctx, alice); err != nil || n != 2 {
		t.Fatalf("CountUnread = %d (err %v), want 2", n, err)
	}

	// Mark one read: affects exactly one row and is idempotent.
	affected, err := store.MarkRead(ctx, alice, first.ID)
	if err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	if affected != 1 {
		t.Fatalf("MarkRead affected = %d, want 1", affected)
	}
	if again, _ := store.MarkRead(ctx, alice, first.ID); again != 0 {
		t.Fatalf("re-marking an already-read row affected = %d, want 0", again)
	}
	if n, _ := store.CountUnread(ctx, alice); n != 1 {
		t.Fatalf("CountUnread after one mark = %d, want 1", n)
	}

	// Cross-user safety: Bob cannot mark Alice's notification read.
	if crossed, _ := store.MarkRead(ctx, bob, first.ID); crossed != 0 {
		t.Fatalf("cross-user MarkRead affected = %d, want 0", crossed)
	}

	// Unread-only list returns just the remaining unread row.
	unreadOnly, err := store.ListNotifications(ctx, domain.ListInput{UserID: alice, Limit: 10, UnreadOnly: true})
	if err != nil {
		t.Fatalf("ListNotifications unreadOnly: %v", err)
	}
	if len(unreadOnly.Notifications) != 1 || unreadOnly.Notifications[0].Title != "Second" {
		t.Fatalf("unreadOnly = %d rows, want 1 (the unread 'Second')", len(unreadOnly.Notifications))
	}

	// Mark all read clears the count.
	if affected, _ := store.MarkAllRead(ctx, alice); affected != 1 {
		t.Fatalf("MarkAllRead affected = %d, want 1 (one row still unread)", affected)
	}
	if n, _ := store.CountUnread(ctx, alice); n != 0 {
		t.Fatalf("CountUnread after MarkAllRead = %d, want 0", n)
	}
}

func mustCreateUser(t *testing.T, ctx context.Context, queries *query.Queries, email, displayName string) uuid.UUID {
	t.Helper()
	hash, err := password.Hash("test-password")
	if err != nil {
		t.Fatalf("password.Hash: %v", err)
	}
	id := uuid.New()
	if _, err := queries.CreateUser(ctx, query.CreateUserParams{
		ID:           pgtype.UUID{Bytes: id, Valid: true},
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: hash,
		Status:       "active",
		CreatedAt:    pgtype.Timestamptz{Time: time.Now(), Valid: true},
		UpdatedAt:    pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}); err != nil {
		t.Fatalf("CreateUser %s failed: %v", email, err)
	}
	return id
}
