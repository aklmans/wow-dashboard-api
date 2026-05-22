package rolesrepo

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/aklmans/wow-dashboard-api/internal/roles/domain"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
)

func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func pgTimestamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// pgTextPtr maps an optional string to a nullable pgtype.Text — a nil pointer
// becomes SQL NULL so a COALESCE update leaves the column unchanged.
func pgTextPtr(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

func domainUUID(id pgtype.UUID) (uuid.UUID, error) {
	if !id.Valid {
		return uuid.Nil, fmt.Errorf("rolesrepo: invalid role id")
	}
	return uuid.UUID(id.Bytes), nil
}

func domainTimestamp(t pgtype.Timestamptz) (time.Time, error) {
	if !t.Valid {
		return time.Time{}, fmt.Errorf("rolesrepo: invalid timestamp")
	}
	return t.Time, nil
}

func buildRole(id pgtype.UUID, name string, description string, isSystem bool, createdAt pgtype.Timestamptz, updatedAt pgtype.Timestamptz, permissions []string, userCount int64) (domain.Role, error) {
	roleID, err := domainUUID(id)
	if err != nil {
		return domain.Role{}, err
	}
	created, err := domainTimestamp(createdAt)
	if err != nil {
		return domain.Role{}, err
	}
	updated, err := domainTimestamp(updatedAt)
	if err != nil {
		return domain.Role{}, err
	}
	if permissions == nil {
		permissions = []string{}
	}
	return domain.Role{
		ID:          roleID,
		Name:        name,
		Description: description,
		IsSystem:    isSystem,
		Permissions: permissions,
		UserCount:   int(userCount),
		CreatedAt:   created,
		UpdatedAt:   updated,
	}, nil
}

func roleFromListRow(row query.ListRolesRow) (domain.Role, error) {
	return buildRole(row.ID, row.Name, row.Description, row.IsSystem, row.CreatedAt, row.UpdatedAt, row.Permissions, row.UserCount)
}

func roleFromGetRow(row query.GetRoleByIDRow) (domain.Role, error) {
	return buildRole(row.ID, row.Name, row.Description, row.IsSystem, row.CreatedAt, row.UpdatedAt, row.Permissions, row.UserCount)
}
