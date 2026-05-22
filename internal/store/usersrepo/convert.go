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

// domainTimestampPtr maps a nullable timestamp to a *time.Time, used for
// optional columns such as last_login_at.
func domainTimestampPtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

// pgTextValue maps a nullable text column to a plain string, treating NULL as
// the empty string.
func pgTextValue(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}

// pgTextPtr maps an optional update field to a nullable text arg: a nil
// pointer leaves the column unchanged, a non-nil pointer (including "")
// overwrites it.
func pgTextPtr(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

// pgStatusPtr maps an optional status update to a nullable text arg.
func pgStatusPtr(status *domain.UserStatus) pgtype.Text {
	if status == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: string(*status), Valid: true}
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
		ID:            id,
		Email:         row.Email,
		DisplayName:   row.DisplayName,
		Status:        domain.UserStatus(row.Status),
		Roles:         roles,
		AvatarURL:     pgTextValue(row.AvatarUrl),
		Phone:         pgTextValue(row.Phone),
		JobTitle:      pgTextValue(row.JobTitle),
		Company:       pgTextValue(row.Company),
		EmailVerified: row.EmailVerifiedAt.Valid,
		LastLoginAt:   domainTimestampPtr(row.LastLoginAt),
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
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
		ID:            id,
		Email:         row.Email,
		DisplayName:   row.DisplayName,
		Status:        domain.UserStatus(row.Status),
		Roles:         []string{},
		AvatarURL:     pgTextValue(row.AvatarUrl),
		Phone:         pgTextValue(row.Phone),
		JobTitle:      pgTextValue(row.JobTitle),
		Company:       pgTextValue(row.Company),
		EmailVerified: row.EmailVerifiedAt.Valid,
		LastLoginAt:   domainTimestampPtr(row.LastLoginAt),
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}, nil
}
