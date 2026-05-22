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

// escapeLikePattern escapes the ILIKE wildcard metacharacters so a user search
// term is matched literally rather than as a pattern. PostgreSQL's default
// ILIKE escape character is backslash, so backslash is escaped first.
func escapeLikePattern(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

func pgTimestamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// pgRolePtr maps an optional role update to a pgtype.Text: nil yields an
// invalid (SQL NULL) value so the UpdateUser COALESCE keeps the existing
// column, and a non-nil pointer becomes the new value.
func pgRolePtr(role *domain.UserRole) pgtype.Text {
	if role == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: string(*role), Valid: true}
}

// pgStatusPtr is the UserStatus equivalent of pgRolePtr.
func pgStatusPtr(status *domain.UserStatus) pgtype.Text {
	if status == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: string(*status), Valid: true}
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

func userFromUpdateRow(row query.UpdateUserRow) (domain.User, error) {
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
