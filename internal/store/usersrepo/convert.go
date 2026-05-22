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

// escapeLikePattern escapes ILIKE wildcard metacharacters so a search term is
// matched literally. PostgreSQL's default escape character is backslash.
func escapeLikePattern(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

func pgUUIDFromDomain(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func pgUUIDsFromDomain(ids []uuid.UUID) []pgtype.UUID {
	out := make([]pgtype.UUID, len(ids))
	for i, id := range ids {
		out[i] = pgUUIDFromDomain(id)
	}
	return out
}

func pgTimestamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
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
	roles := row.Roles
	if roles == nil {
		roles = []string{}
	}
	return domain.User{
		ID:          id,
		Email:       row.Email,
		DisplayName: row.DisplayName,
		Status:      domain.UserStatus(row.Status),
		Roles:       roles,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}, nil
}

// userFromDetailRow converts a GetUserByID row. The detail query does not join
// roles, so Roles is left empty for the caller to populate.
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
		Roles:       []string{},
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}, nil
}
