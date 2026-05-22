package usersrepo

import (
	"fmt"
	"strings"
	"time"

	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/aklmans/wow-dashboard-api/internal/users/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func pgText(value string) pgtype.Text {
	value = strings.TrimSpace(value)
	return pgtype.Text{String: value, Valid: value != ""}
}

func domainUUID(id pgtype.UUID) (uuid.UUID, error) {
	if !id.Valid {
		return uuid.Nil, fmt.Errorf("usersrepo: invalid user id")
	}
	return uuid.UUID(id.Bytes), nil
}

func domainTimestamp(t pgtype.Timestamptz) (time.Time, error) {
	if !t.Valid {
		return time.Time{}, fmt.Errorf("usersrepo: invalid timestamp")
	}
	return t.Time, nil
}

func userFromListRow(row query.ListUsersPageRow) (domain.User, error) {
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
		Role:        domain.UserRole(row.Role),
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}, nil
}

func userFromDetailRow(row query.GetUserByIDRow) (domain.User, error) {
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
		Role:        domain.UserRole(row.Role),
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}, nil
}

func pgUUIDFromDomain(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}
