//go:build integration

package usersrepo_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/aklmans/wow-dashboard-api/internal/auth/password"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/aklmans/wow-dashboard-api/internal/store/storetest"
	"github.com/aklmans/wow-dashboard-api/internal/store/usersrepo"
	"github.com/aklmans/wow-dashboard-api/internal/users/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestUserStoreListUsersIntegration(t *testing.T) {
	ctx := context.Background()
	pool := storetest.NewPostgresPool(t, ctx, "test_usersrepo_list_db", "../../../migrations")
	queries := query.New(pool)
	repo := usersrepo.NewUserStore(queries)

	base := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	insertUser(t, ctx, queries, "ada@example.com", "Ada Lovelace", "admin", "active", base.Add(3*time.Minute))
	insertUser(t, ctx, queries, "grace@example.com", "Grace Hopper", "user", "active", base.Add(2*time.Minute))
	insertUser(t, ctx, queries, "linus@example.com", "Linus Disabled", "user", "disabled", base.Add(time.Minute))

	t.Run("list returns users with roles and total count", func(t *testing.T) {
		result, err := repo.ListUsers(ctx, domain.ListUsersInput{Page: 1, PageSize: 20})
		if err != nil {
			t.Fatalf("ListUsers failed: %v", err)
		}
		if result.Total != 3 || len(result.Users) != 3 {
			t.Fatalf("total/len = %d/%d, want 3/3", result.Total, len(result.Users))
		}
		if result.Users[0].Email != "ada@example.com" {
			t.Fatalf("first email = %q, want newest user ada@example.com", result.Users[0].Email)
		}
		if !slices.Contains(result.Users[0].Roles, "admin") {
			t.Fatalf("ada roles = %v, want to contain admin", result.Users[0].Roles)
		}
	})

	t.Run("search matches email and display name", func(t *testing.T) {
		byEmail, err := repo.ListUsers(ctx, domain.ListUsersInput{Page: 1, PageSize: 20, Search: "GRACE@"})
		if err != nil {
			t.Fatalf("ListUsers search email failed: %v", err)
		}
		if byEmail.Total != 1 || len(byEmail.Users) != 1 || byEmail.Users[0].Email != "grace@example.com" {
			t.Fatalf("email search result = %#v, want grace only", byEmail)
		}

		byName, err := repo.ListUsers(ctx, domain.ListUsersInput{Page: 1, PageSize: 20, Search: "disabled"})
		if err != nil {
			t.Fatalf("ListUsers search name failed: %v", err)
		}
		if byName.Total != 1 || len(byName.Users) != 1 || byName.Users[0].Email != "linus@example.com" {
			t.Fatalf("name search result = %#v, want linus only", byName)
		}
	})

	t.Run("filters role and status", func(t *testing.T) {
		admins, err := repo.ListUsers(ctx, domain.ListUsersInput{Page: 1, PageSize: 20, Role: "admin"})
		if err != nil {
			t.Fatalf("ListUsers role failed: %v", err)
		}
		if admins.Total != 1 || len(admins.Users) != 1 || !slices.Contains(admins.Users[0].Roles, "admin") {
			t.Fatalf("role filter result = %#v, want one admin", admins)
		}

		disabled, err := repo.ListUsers(ctx, domain.ListUsersInput{Page: 1, PageSize: 20, Status: domain.UserStatusDisabled})
		if err != nil {
			t.Fatalf("ListUsers status failed: %v", err)
		}
		if disabled.Total != 1 || len(disabled.Users) != 1 || disabled.Users[0].Status != domain.UserStatusDisabled {
			t.Fatalf("status filter result = %#v, want one disabled user", disabled)
		}
	})

	t.Run("pagination returns page and total", func(t *testing.T) {
		page2, err := repo.ListUsers(ctx, domain.ListUsersInput{Page: 2, PageSize: 1, Offset: 1})
		if err != nil {
			t.Fatalf("ListUsers page 2 failed: %v", err)
		}
		if page2.Total != 3 || page2.Page != 2 || page2.PageSize != 1 {
			t.Fatalf("page metadata = total %d page %d/%d, want 3 2/1", page2.Total, page2.Page, page2.PageSize)
		}
		if len(page2.Users) != 1 || page2.Users[0].Email != "grace@example.com" {
			t.Fatalf("page 2 users = %#v, want grace", page2.Users)
		}
	})
}

func TestUserStoreGetUserByIDIntegration(t *testing.T) {
	ctx := context.Background()
	pool := storetest.NewPostgresPool(t, ctx, "test_usersrepo_detail_db", "../../../migrations")
	queries := query.New(pool)
	repo := usersrepo.NewUserStore(queries)

	created := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	insertUser(t, ctx, queries, "ada@example.com", "Ada Lovelace", "admin", "active", created)

	t.Run("returns existing user with roles", func(t *testing.T) {
		list, err := repo.ListUsers(ctx, domain.ListUsersInput{Page: 1, PageSize: 20})
		if err != nil {
			t.Fatalf("ListUsers failed: %v", err)
		}
		id := list.Users[0].ID

		got, err := repo.GetUserByID(ctx, id)
		if err != nil {
			t.Fatalf("GetUserByID returned error: %v", err)
		}
		if got.ID != id || got.Email != "ada@example.com" || got.DisplayName != "Ada Lovelace" {
			t.Fatalf("user = %#v, want ada@example.com / Ada Lovelace", got)
		}
		if got.Status != domain.UserStatusActive || !slices.Contains(got.Roles, "admin") {
			t.Fatalf("status/roles = %s/%v, want active and admin", got.Status, got.Roles)
		}
		if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
			t.Fatalf("timestamps zero: %#v", got)
		}
	})

	t.Run("missing id returns ErrUserNotFound", func(t *testing.T) {
		_, err := repo.GetUserByID(ctx, uuid.New())
		if !errors.Is(err, domain.ErrUserNotFound) {
			t.Fatalf("err = %v, want domain.ErrUserNotFound", err)
		}
	})
}

// insertUser creates a user and assigns the named (already-seeded) role.
func insertUser(t *testing.T, ctx context.Context, queries *query.Queries, email string, displayName string, role string, status string, createdAt time.Time) {
	t.Helper()

	hash, err := password.Hash("test-password")
	if err != nil {
		t.Fatalf("password.Hash failed: %v", err)
	}

	userID := pgUUID(t, uuid.New())
	if _, err := queries.CreateUser(ctx, query.CreateUserParams{
		ID:           userID,
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: hash,
		Status:       status,
		CreatedAt:    pgTime(createdAt),
		UpdatedAt:    pgTime(createdAt),
	}); err != nil {
		t.Fatalf("CreateUser %s failed: %v", email, err)
	}

	roleRow, err := queries.GetRoleByName(ctx, role)
	if err != nil {
		t.Fatalf("GetRoleByName %s failed: %v", role, err)
	}
	if err := queries.AddUserRole(ctx, query.AddUserRoleParams{UserID: userID, RoleID: roleRow.ID}); err != nil {
		t.Fatalf("AddUserRole %s failed: %v", email, err)
	}
}

func pgUUID(t *testing.T, id uuid.UUID) pgtype.UUID {
	t.Helper()

	var value pgtype.UUID
	if err := value.Scan(id.String()); err != nil {
		t.Fatalf("scan uuid failed: %v", err)
	}
	return value
}

func pgTime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}
