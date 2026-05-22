//go:build integration

package rolesrepo_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/aklmans/wow-dashboard-api/internal/auth/password"
	"github.com/aklmans/wow-dashboard-api/internal/roles/domain"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/aklmans/wow-dashboard-api/internal/store/rolesrepo"
	"github.com/aklmans/wow-dashboard-api/internal/store/storetest"
)

func TestRoleStoreIntegration(t *testing.T) {
	ctx := context.Background()
	pool := storetest.NewPostgresPool(t, ctx, "test_rolesrepo_db", "../../../migrations")
	queries := query.New(pool)
	repo := rolesrepo.NewRoleStore(pool)

	now := time.Now().UTC().Truncate(time.Microsecond)

	// Migration 00009 seeds the admin and user system roles.
	seeded, err := repo.ListRoles(ctx)
	if err != nil {
		t.Fatalf("ListRoles failed: %v", err)
	}
	if len(seeded) != 2 {
		t.Fatalf("seeded roles = %d, want 2 (admin, user)", len(seeded))
	}
	for _, r := range seeded {
		if !r.IsSystem {
			t.Fatalf("seeded role %q is not flagged as a system role", r.Name)
		}
	}

	// CreateRole.
	created, err := repo.CreateRole(ctx, domain.CreateRoleInput{
		ID:          uuid.New(),
		Name:        "auditor",
		Description: "Read-only audit access",
		Permissions: []string{"system_events:read", "users:read"},
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}
	if created.Name != "auditor" || created.IsSystem || created.UserCount != 0 {
		t.Fatalf("created role = %#v, want non-system auditor with no users", created)
	}
	if len(created.Permissions) != 2 {
		t.Fatalf("created permissions = %v, want two entries", created.Permissions)
	}

	// Duplicate name is rejected.
	if _, err := repo.CreateRole(ctx, domain.CreateRoleInput{
		ID: uuid.New(), Name: "auditor", CreatedAt: now, UpdatedAt: now,
	}); !errors.Is(err, domain.ErrNameConflict) {
		t.Fatalf("duplicate CreateRole err = %v, want ErrNameConflict", err)
	}

	// GetRoleByID.
	got, err := repo.GetRoleByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetRoleByID failed: %v", err)
	}
	if got.ID != created.ID || !slices.Contains(got.Permissions, "users:read") {
		t.Fatalf("fetched role = %#v, want auditor with users:read", got)
	}
	if _, err := repo.GetRoleByID(ctx, uuid.New()); !errors.Is(err, domain.ErrRoleNotFound) {
		t.Fatalf("GetRoleByID missing err = %v, want ErrRoleNotFound", err)
	}

	// UpdateRole: rename and replace the permission set.
	newName := "senior-auditor"
	updated, err := repo.UpdateRole(ctx, domain.UpdateRoleInput{
		ID:          created.ID,
		Name:        &newName,
		Permissions: &[]string{"users:read"},
		UpdatedAt:   time.Now().UTC().Truncate(time.Microsecond),
	})
	if err != nil {
		t.Fatalf("UpdateRole failed: %v", err)
	}
	if updated.Name != "senior-auditor" || len(updated.Permissions) != 1 || updated.Permissions[0] != "users:read" {
		t.Fatalf("updated role = %#v, want senior-auditor with only users:read", updated)
	}

	// A role assigned to a user cannot be deleted.
	userID := mustCreateUser(t, ctx, queries, "auditor-user@example.com")
	if err := queries.AddUserRole(ctx, query.AddUserRoleParams{
		UserID: pgtype.UUID{Bytes: userID, Valid: true},
		RoleID: pgtype.UUID{Bytes: created.ID, Valid: true},
	}); err != nil {
		t.Fatalf("AddUserRole failed: %v", err)
	}
	if err := repo.DeleteRole(ctx, created.ID); !errors.Is(err, domain.ErrRoleInUse) {
		t.Fatalf("DeleteRole of an assigned role err = %v, want ErrRoleInUse", err)
	}

	// Once the assignment is removed the role deletes cleanly.
	if err := queries.ReplaceUserRoles(ctx, query.ReplaceUserRolesParams{
		UserID:  pgtype.UUID{Bytes: userID, Valid: true},
		RoleIds: []pgtype.UUID{},
	}); err != nil {
		t.Fatalf("ReplaceUserRoles failed: %v", err)
	}
	if err := repo.DeleteRole(ctx, created.ID); err != nil {
		t.Fatalf("DeleteRole failed: %v", err)
	}
	if _, err := repo.GetRoleByID(ctx, created.ID); !errors.Is(err, domain.ErrRoleNotFound) {
		t.Fatalf("GetRoleByID after delete err = %v, want ErrRoleNotFound", err)
	}
}

func mustCreateUser(t *testing.T, ctx context.Context, queries *query.Queries, email string) uuid.UUID {
	t.Helper()
	hash, err := password.Hash("test-password")
	if err != nil {
		t.Fatalf("password.Hash failed: %v", err)
	}
	id := uuid.New()
	now := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	if _, err := queries.CreateUser(ctx, query.CreateUserParams{
		ID:           pgtype.UUID{Bytes: id, Valid: true},
		Email:        email,
		DisplayName:  "Auditor User",
		PasswordHash: hash,
		Status:       "active",
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	return id
}
