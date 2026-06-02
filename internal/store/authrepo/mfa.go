package authrepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aklmans/wow-dashboard-api/internal/auth/domain"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
)

// MfaStore is the store adapter for TOTP secrets and one-time recovery codes.
type MfaStore struct {
	pool    *pgxpool.Pool
	queries *query.Queries
}

func NewMfaStore(pool *pgxpool.Pool) *MfaStore {
	return &MfaStore{pool: pool, queries: query.New(pool)}
}

// GetMfaSecret returns the user's TOTP secret, or domain.ErrMfaSecretNotFound.
func (s *MfaStore) GetMfaSecret(ctx context.Context, userID uuid.UUID) (domain.MfaSecret, error) {
	row, err := s.queries.GetUserMfaSecret(ctx, pgUUID(userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.MfaSecret{}, domain.ErrMfaSecretNotFound
		}
		return domain.MfaSecret{}, fmt.Errorf("authrepo: get mfa secret: %w", err)
	}
	return mfaSecretFromRow(row)
}

// DeleteMfaSecret removes the user's TOTP secret (used on disable / re-setup).
func (s *MfaStore) DeleteMfaSecret(ctx context.Context, userID uuid.UUID) error {
	if err := s.queries.DeleteUserMfaSecret(ctx, pgUUID(userID)); err != nil {
		return fmt.Errorf("authrepo: delete mfa secret: %w", err)
	}
	return nil
}

// SetUserMfaEnabled flips the user's mfa_enabled flag. confirmedAt is nil to
// clear the timestamp (on disable).
func (s *MfaStore) SetUserMfaEnabled(ctx context.Context, userID uuid.UUID, enabled bool, confirmedAt *time.Time, now time.Time) error {
	confirmed := pgtype.Timestamptz{}
	if confirmedAt != nil {
		confirmed = pgTimestamp(*confirmedAt)
	}
	if err := s.queries.SetUserMfaEnabled(ctx, query.SetUserMfaEnabledParams{
		MfaEnabled:     enabled,
		MfaConfirmedAt: confirmed,
		UpdatedAt:      pgTimestamp(now),
		ID:             pgUUID(userID),
	}); err != nil {
		return fmt.Errorf("authrepo: set mfa enabled: %w", err)
	}
	return nil
}

// DeleteRecoveryCodes removes all of the user's recovery codes (before a fresh
// set is generated, or on disable).
func (s *MfaStore) DeleteRecoveryCodes(ctx context.Context, userID uuid.UUID) error {
	if err := s.queries.DeleteMfaRecoveryCodesForUser(ctx, pgUUID(userID)); err != nil {
		return fmt.Errorf("authrepo: delete recovery codes: %w", err)
	}
	return nil
}

// CreateRecoveryCode stores one hashed recovery code.
func (s *MfaStore) CreateRecoveryCode(ctx context.Context, id, userID uuid.UUID, codeHash string, createdAt time.Time) error {
	if err := s.queries.CreateMfaRecoveryCode(ctx, query.CreateMfaRecoveryCodeParams{
		ID:        pgUUID(id),
		UserID:    pgUUID(userID),
		CodeHash:  codeHash,
		CreatedAt: pgTimestamp(createdAt),
	}); err != nil {
		return fmt.Errorf("authrepo: create recovery code: %w", err)
	}
	return nil
}

// StoreSetupSecret stores a fresh (unconfirmed) TOTP secret, serialized against
// concurrent setup/confirm by locking the user row, and refusing if MFA is
// already enabled (domain.ErrMfaAlreadyEnabled).
func (s *MfaStore) StoreSetupSecret(ctx context.Context, input domain.UpsertMfaSecretInput) error {
	return s.inTx(ctx, func(q *query.Queries) error {
		enabled, err := lockUser(ctx, q, input.UserID)
		if err != nil {
			return err
		}
		if enabled {
			return domain.ErrMfaAlreadyEnabled
		}
		if _, err := q.UpsertUserMfaSecret(ctx, query.UpsertUserMfaSecretParams{
			ID:              pgUUID(input.ID),
			UserID:          pgUUID(input.UserID),
			SecretEncrypted: input.SecretEncrypted,
			Algorithm:       input.Algorithm,
			Digits:          int32(input.Digits),
			Period:          int32(input.Period),
			CreatedAt:       pgTimestamp(input.CreatedAt),
			UpdatedAt:       pgTimestamp(input.UpdatedAt),
		}); err != nil {
			return fmt.Errorf("authrepo: upsert mfa secret: %w", err)
		}
		return nil
	})
}

// CompleteEnrollment atomically replaces the user's recovery codes and enables
// MFA, serialized by the user-row lock. It refuses (domain.ErrMfaAlreadyEnabled)
// if a concurrent confirm already enabled MFA, so codes and the enabled flag
// always commit together.
func (s *MfaStore) CompleteEnrollment(ctx context.Context, userID uuid.UUID, codeHashes []string, confirmedAt time.Time, now time.Time) error {
	return s.inTx(ctx, func(q *query.Queries) error {
		enabled, err := lockUser(ctx, q, userID)
		if err != nil {
			return err
		}
		if enabled {
			return domain.ErrMfaAlreadyEnabled
		}
		if err := q.DeleteMfaRecoveryCodesForUser(ctx, pgUUID(userID)); err != nil {
			return fmt.Errorf("authrepo: clear recovery codes: %w", err)
		}
		for _, h := range codeHashes {
			if err := q.CreateMfaRecoveryCode(ctx, query.CreateMfaRecoveryCodeParams{
				ID:        pgUUID(uuid.New()),
				UserID:    pgUUID(userID),
				CodeHash:  h,
				CreatedAt: pgTimestamp(now),
			}); err != nil {
				return fmt.Errorf("authrepo: create recovery code: %w", err)
			}
		}
		if err := q.SetUserMfaEnabled(ctx, query.SetUserMfaEnabledParams{
			MfaEnabled:     true,
			MfaConfirmedAt: pgTimestamp(confirmedAt),
			UpdatedAt:      pgTimestamp(now),
			ID:             pgUUID(userID),
		}); err != nil {
			return fmt.Errorf("authrepo: enable mfa: %w", err)
		}
		return nil
	})
}

func lockUser(ctx context.Context, q *query.Queries, userID uuid.UUID) (bool, error) {
	enabled, err := q.LockUserMfaEnabled(ctx, pgUUID(userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, domain.ErrUserNotFound
		}
		return false, fmt.Errorf("authrepo: lock user: %w", err)
	}
	return enabled, nil
}

func (s *MfaStore) inTx(ctx context.Context, fn func(*query.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("authrepo: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(query.New(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("authrepo: commit tx: %w", err)
	}
	return nil
}

func mfaSecretFromRow(row query.UserMfaSecret) (domain.MfaSecret, error) {
	id, err := domainUUID(row.ID)
	if err != nil {
		return domain.MfaSecret{}, err
	}
	userID, err := domainUUID(row.UserID)
	if err != nil {
		return domain.MfaSecret{}, err
	}
	createdAt, err := domainTimestamp(row.CreatedAt)
	if err != nil {
		return domain.MfaSecret{}, err
	}
	updatedAt, err := domainTimestamp(row.UpdatedAt)
	if err != nil {
		return domain.MfaSecret{}, err
	}
	return domain.MfaSecret{
		ID:              id,
		UserID:          userID,
		SecretEncrypted: row.SecretEncrypted,
		Algorithm:       row.Algorithm,
		Digits:          int(row.Digits),
		Period:          int(row.Period),
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}, nil
}
