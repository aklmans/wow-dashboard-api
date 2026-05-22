// Package seed provides explicit local-development database seed helpers.
package seed

import (
	"context"
	"fmt"
	"time"

	"github.com/aklmans/wow-dashboard-api/internal/auth/password"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	DemoEmail       = "demo@minimals.cc"
	DemoPassword    = "@2Minimal"
	DemoDisplayName = "Demo User"
	demoStatus      = "active"
	demoRole        = "admin"
)

// DemoUser is the public result of seeding the local starter demo account.
type DemoUser struct {
	ID          string
	Email       string
	DisplayName string
	Status      string
	Role        string
}

// DemoUserStore is the sqlc query subset needed by the demo seed.
type DemoUserStore interface {
	UpsertDemoUser(ctx context.Context, arg query.UpsertDemoUserParams) (query.UpsertDemoUserRow, error)
}

// SeedDemoUser creates or updates the local starter demo account.
func SeedDemoUser(ctx context.Context, store DemoUserStore) (DemoUser, error) {
	passwordHash, err := password.Hash(DemoPassword)
	if err != nil {
		return DemoUser{}, fmt.Errorf("seed demo user password: %w", err)
	}

	userID := uuid.New()
	pgUserID := pgtype.UUID{Bytes: userID, Valid: true}

	now := time.Now().UTC().Truncate(time.Microsecond)
	var pgNow pgtype.Timestamptz
	if err := pgNow.Scan(now); err != nil {
		return DemoUser{}, fmt.Errorf("seed demo user timestamp: %w", err)
	}

	seeded, err := store.UpsertDemoUser(ctx, query.UpsertDemoUserParams{
		ID:           pgUserID,
		Email:        DemoEmail,
		DisplayName:  DemoDisplayName,
		PasswordHash: passwordHash,
		Status:       demoStatus,
		Role:         demoRole,
		CreatedAt:    pgNow,
		UpdatedAt:    pgNow,
	})
	if err != nil {
		return DemoUser{}, fmt.Errorf("seed demo user: %w", err)
	}
	if !seeded.ID.Valid {
		return DemoUser{}, fmt.Errorf("seed demo user: returned invalid user id")
	}

	return DemoUser{
		ID:          uuid.UUID(seeded.ID.Bytes).String(),
		Email:       seeded.Email,
		DisplayName: seeded.DisplayName,
		Status:      seeded.Status,
		Role:        seeded.Role,
	}, nil
}
