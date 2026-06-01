package authrepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/aklmans/wow-dashboard-api/internal/auth/domain"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
)

// MfaStore is the store adapter for TOTP secrets and one-time recovery codes.
type MfaStore struct {
	queries *query.Queries
}

func NewMfaStore(q *query.Queries) *MfaStore {
	return &MfaStore{queries: q}
}

// UpsertMfaSecret stores (or replaces) the user's encrypted TOTP secret.
func (s *MfaStore) UpsertMfaSecret(ctx context.Context, input domain.UpsertMfaSecretInput) (domain.MfaSecret, error) {
	row, err := s.queries.UpsertUserMfaSecret(ctx, query.UpsertUserMfaSecretParams{
		ID:              pgUUID(input.ID),
		UserID:          pgUUID(input.UserID),
		SecretEncrypted: input.SecretEncrypted,
		Algorithm:       input.Algorithm,
		Digits:          int32(input.Digits),
		Period:          int32(input.Period),
		CreatedAt:       pgTimestamp(input.CreatedAt),
		UpdatedAt:       pgTimestamp(input.UpdatedAt),
	})
	if err != nil {
		return domain.MfaSecret{}, fmt.Errorf("authrepo: upsert mfa secret: %w", err)
	}
	return mfaSecretFromRow(row)
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
