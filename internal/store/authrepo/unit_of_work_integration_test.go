//go:build integration

package authrepo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aklmans/wow-dashboard-api/internal/auth/domain"
	"github.com/aklmans/wow-dashboard-api/internal/auth/password"
	authservice "github.com/aklmans/wow-dashboard-api/internal/auth/service"
	"github.com/aklmans/wow-dashboard-api/internal/store/authrepo"
	"github.com/aklmans/wow-dashboard-api/internal/store/storetest"
	"github.com/google/uuid"
)

func TestUnitOfWorkIntegration(t *testing.T) {
	ctx := context.Background()
	pool := storetest.NewPostgresPool(t, ctx, "test_authrepo_unit_of_work_db", "../../../migrations")
	uow := authrepo.NewUnitOfWork(pool)
	userRepo := authrepo.NewUserStoreFromDB(pool)
	refreshRepo := authrepo.NewRefreshTokenStoreFromDB(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)

	t.Run("commits user and refresh token", func(t *testing.T) {
		userID := uuid.New()
		tokenHash := "uow-commit-token-hash"

		err := uow.Do(ctx, func(ctx context.Context, deps authservice.WorkDeps) error {
			_, err := deps.Users.CreateUser(ctx, testCreateUserInput(t, userID, "uow-commit@example.com", now))
			if err != nil {
				return err
			}
			_, err = deps.RefreshTokens.CreateRefreshToken(ctx, domain.CreateRefreshTokenInput{
				ID:        uuid.New(),
				UserID:    userID,
				TokenHash: tokenHash,
				FamilyID:  uuid.New(),
				ExpiresAt: now.Add(24 * time.Hour),
				CreatedAt: now,
				UpdatedAt: now,
			})
			return err
		})
		if err != nil {
			t.Fatalf("UnitOfWork.Do returned error: %v", err)
		}

		if _, err := userRepo.GetUserByID(ctx, userID); err != nil {
			t.Fatalf("GetUserByID after commit returned error: %v", err)
		}
		if _, err := refreshRepo.GetRefreshTokenByHash(ctx, tokenHash); err != nil {
			t.Fatalf("GetRefreshTokenByHash after commit returned error: %v", err)
		}
	})

	t.Run("rolls back when refresh token create fails", func(t *testing.T) {
		userID := uuid.New()

		err := uow.Do(ctx, func(ctx context.Context, deps authservice.WorkDeps) error {
			_, err := deps.Users.CreateUser(ctx, testCreateUserInput(t, userID, "uow-refresh-fail@example.com", now))
			if err != nil {
				return err
			}
			_, err = deps.RefreshTokens.CreateRefreshToken(ctx, domain.CreateRefreshTokenInput{
				ID:        uuid.New(),
				UserID:    uuid.New(),
				TokenHash: "uow-refresh-fail-token-hash",
				FamilyID:  uuid.New(),
				ExpiresAt: now.Add(24 * time.Hour),
				CreatedAt: now,
				UpdatedAt: now,
			})
			return err
		})
		if err == nil {
			t.Fatal("UnitOfWork.Do returned nil error, want refresh token create failure")
		}

		_, err = userRepo.GetUserByID(ctx, userID)
		if !errors.Is(err, domain.ErrUserNotFound) {
			t.Fatalf("GetUserByID after rollback error = %v, want ErrUserNotFound", err)
		}
	})

	t.Run("rolls back when callback returns business error", func(t *testing.T) {
		userID := uuid.New()
		businessErr := errors.New("business rule failed")

		err := uow.Do(ctx, func(ctx context.Context, deps authservice.WorkDeps) error {
			_, err := deps.Users.CreateUser(ctx, testCreateUserInput(t, userID, "uow-business-fail@example.com", now))
			if err != nil {
				return err
			}
			return businessErr
		})
		if !errors.Is(err, businessErr) {
			t.Fatalf("UnitOfWork.Do error = %v, want business error", err)
		}

		_, err = userRepo.GetUserByID(ctx, userID)
		if !errors.Is(err, domain.ErrUserNotFound) {
			t.Fatalf("GetUserByID after business rollback error = %v, want ErrUserNotFound", err)
		}
	})
}

func testCreateUserInput(t testing.TB, userID uuid.UUID, email string, now time.Time) domain.CreateUserInput {
	t.Helper()
	hash, err := password.Hash("correct-password")
	if err != nil {
		t.Fatalf("password.Hash failed: %v", err)
	}
	return domain.CreateUserInput{
		ID:           userID,
		Email:        email,
		DisplayName:  "UOW User",
		PasswordHash: hash,
		Status:       domain.UserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}
