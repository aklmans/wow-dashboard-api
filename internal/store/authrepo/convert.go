package authrepo

import (
	"fmt"
	"time"

	"github.com/aklmans/wow-dashboard-api/internal/auth/domain"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func domainUUID(id pgtype.UUID) (uuid.UUID, error) {
	if !id.Valid {
		return uuid.Nil, fmt.Errorf("authrepo: invalid user id")
	}
	return uuid.UUID(id.Bytes), nil
}

func pgTimestamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func domainTimestamp(t pgtype.Timestamptz) (time.Time, error) {
	if !t.Valid {
		return time.Time{}, fmt.Errorf("authrepo: invalid timestamp")
	}
	return t.Time, nil
}

func nullableDomainTimestamp(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	value := t.Time
	return &value
}

func nullableDomainUUID(id pgtype.UUID) *uuid.UUID {
	if !id.Valid {
		return nil
	}
	value := uuid.UUID(id.Bytes)
	return &value
}

func userFromCreateRow(row query.CreateUserRow) (domain.User, error) {
	id, err := domainUUID(row.ID)
	if err != nil {
		return domain.User{}, err
	}
	createdAt, err := domainTimestamp(row.CreatedAt)
	if err != nil {
		return domain.User{}, err
	}
	updatedAt, err := domainTimestamp(row.UpdatedAt)
	if err != nil {
		return domain.User{}, err
	}
	return domain.User{
		ID:          id,
		Email:       row.Email,
		DisplayName: row.DisplayName,
		Status:      domain.UserStatus(row.Status),
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}, nil
}

func userFromGetByIDRow(row query.GetUserByIDRow) (domain.User, error) {
	id, err := domainUUID(row.ID)
	if err != nil {
		return domain.User{}, err
	}
	createdAt, err := domainTimestamp(row.CreatedAt)
	if err != nil {
		return domain.User{}, err
	}
	updatedAt, err := domainTimestamp(row.UpdatedAt)
	if err != nil {
		return domain.User{}, err
	}
	return domain.User{
		ID:            id,
		Email:         row.Email,
		DisplayName:   row.DisplayName,
		Status:        domain.UserStatus(row.Status),
		EmailVerified: row.EmailVerifiedAt.Valid,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}, nil
}

func authUserFromRow(row query.User) (domain.AuthUser, error) {
	id, err := domainUUID(row.ID)
	if err != nil {
		return domain.AuthUser{}, err
	}
	createdAt, err := domainTimestamp(row.CreatedAt)
	if err != nil {
		return domain.AuthUser{}, err
	}
	updatedAt, err := domainTimestamp(row.UpdatedAt)
	if err != nil {
		return domain.AuthUser{}, err
	}
	return domain.AuthUser{
		User: domain.User{
			ID:            id,
			Email:         row.Email,
			DisplayName:   row.DisplayName,
			Status:        domain.UserStatus(row.Status),
			EmailVerified: row.EmailVerifiedAt.Valid,
			CreatedAt:     createdAt,
			UpdatedAt:     updatedAt,
		},
		PasswordHash:     row.PasswordHash,
		FailedLoginCount: int(row.FailedLoginCount),
		LockedUntil:      nullableDomainTimestamp(row.LockedUntil),
	}, nil
}

func refreshTokenFromRow(row query.RefreshToken) (domain.RefreshToken, error) {
	id, err := domainUUID(row.ID)
	if err != nil {
		return domain.RefreshToken{}, err
	}
	userID, err := domainUUID(row.UserID)
	if err != nil {
		return domain.RefreshToken{}, err
	}
	familyID, err := domainUUID(row.FamilyID)
	if err != nil {
		return domain.RefreshToken{}, err
	}
	expiresAt, err := domainTimestamp(row.ExpiresAt)
	if err != nil {
		return domain.RefreshToken{}, err
	}
	createdAt, err := domainTimestamp(row.CreatedAt)
	if err != nil {
		return domain.RefreshToken{}, err
	}
	updatedAt, err := domainTimestamp(row.UpdatedAt)
	if err != nil {
		return domain.RefreshToken{}, err
	}

	return domain.RefreshToken{
		ID:                id,
		UserID:            userID,
		TokenHash:         row.TokenHash,
		FamilyID:          familyID,
		ExpiresAt:         expiresAt,
		RevokedAt:         nullableDomainTimestamp(row.RevokedAt),
		ReplacedByTokenID: nullableDomainUUID(row.ReplacedByTokenID),
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
	}, nil
}
