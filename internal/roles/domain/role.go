// Package domain contains role management domain types shared by the service
// and store adapter.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrRoleNotFound is returned by the store when no role matches the lookup.
	ErrRoleNotFound = errors.New("roles: role not found")
	// ErrNameConflict is returned when a role name is already taken.
	ErrNameConflict = errors.New("roles: role name already exists")
	// ErrRoleInUse is returned when a delete is rejected because the role is
	// still assigned to one or more users.
	ErrRoleInUse = errors.New("roles: role is assigned to users")
)

// Role is a named role together with its granted permission strings and the
// number of users currently assigned to it.
type Role struct {
	ID          uuid.UUID
	Name        string
	Description string
	IsSystem    bool
	Permissions []string
	UserCount   int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CreateRoleInput is the normalized input for creating a custom role.
type CreateRoleInput struct {
	ID          uuid.UUID
	Name        string
	Description string
	Permissions []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// UpdateRoleInput is the normalized input for updating a role. Name,
// Description, and Permissions are pointers so an omitted field is left
// unchanged; Permissions, when set, replaces the role's entire permission set.
type UpdateRoleInput struct {
	ID          uuid.UUID
	Name        *string
	Description *string
	Permissions *[]string
	UpdatedAt   time.Time
}
