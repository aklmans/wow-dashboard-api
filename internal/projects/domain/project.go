// Package domain contains project management domain types shared by the service and store adapter.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrProjectNotFound is returned by the store when no project matches the
// lookup (by id and owner). It lives in the domain package so store
// adapters do not need to import the service layer.
var (
	ErrProjectNotFound          = errors.New("projects: project not found")
	ErrProjectNameAlreadyExists = errors.New("projects: project name already exists")
	ErrProjectMemberNotFound    = errors.New("projects: project member not found")
	ErrMemberAlreadyExists      = errors.New("projects: project member already exists")
	ErrMemberUserNotFound       = errors.New("projects: member user not found")
)

// ProjectStatus enumerates the allowed lifecycle states for a project row.
type ProjectStatus string

const (
	ProjectStatusActive   ProjectStatus = "active"
	ProjectStatusArchived ProjectStatus = "archived"
)

// Project is the canonical project entity returned by the store.
type Project struct {
	ID          uuid.UUID
	Name        string
	Description string
	Status      ProjectStatus
	OwnerUserID uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ListProjectsInput is the normalized input passed from service into store.
// UserID is the requesting user; the store returns projects they own or are
// a member of.
type ListProjectsInput struct {
	UserID   uuid.UUID
	Page     int
	PageSize int
	Offset   int
	Search   string
	Status   ProjectStatus
}

// ListProjectsResult is the paginated list of projects returned by the store.
type ListProjectsResult struct {
	Projects []Project
	Page     int
	PageSize int
	Total    int
}

// CreateProjectInput is the normalized input the store needs to insert a row.
// Generated identifiers and timestamps are owned by the service layer so the
// store stays oblivious to clock/UUID concerns.
type CreateProjectInput struct {
	ID          uuid.UUID
	Name        string
	Description string
	Status      ProjectStatus
	OwnerUserID uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// UpdateProjectInput is the normalized partial-update input from service to
// store. Pointer fields distinguish "not provided" (nil, store leaves the
// column unchanged) from "provided" (apply the new value, including an empty
// string for description). UpdatedAt is always advanced by the service. The
// caller is authorized by the service before this reaches the store, so the
// store scopes the update by id alone.
type UpdateProjectInput struct {
	ID          uuid.UUID
	Name        *string
	Description *string
	Status      *ProjectStatus
	UpdatedAt   time.Time
}

// ProjectRole is a non-owner access grant level on a project.
type ProjectRole string

const (
	ProjectRoleViewer ProjectRole = "viewer"
	ProjectRoleEditor ProjectRole = "editor"
)

// AccessRole is a user's effective access on a project: the owner, or one of
// the non-owner ProjectRole grants.
type AccessRole string

const (
	AccessRoleOwner  AccessRole = "owner"
	AccessRoleEditor AccessRole = "editor"
	AccessRoleViewer AccessRole = "viewer"
)

// CanEdit reports whether the role may modify project fields.
func (r AccessRole) CanEdit() bool { return r == AccessRoleOwner || r == AccessRoleEditor }

// ProjectAccess pairs a project with the requesting user's effective role.
type ProjectAccess struct {
	Project    Project
	AccessRole AccessRole
}

// ProjectMember is a non-owner access grant row.
type ProjectMember struct {
	ProjectID uuid.UUID
	UserID    uuid.UUID
	Role      ProjectRole
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ProjectMemberDetail is a member grant enriched with the user's identity for
// member-list responses.
type ProjectMemberDetail struct {
	UserID      uuid.UUID
	Email       string
	DisplayName string
	Role        ProjectRole
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// AddProjectMemberInput is the normalized input to grant a user project access.
type AddProjectMemberInput struct {
	ProjectID uuid.UUID
	UserID    uuid.UUID
	Role      ProjectRole
	CreatedAt time.Time
	UpdatedAt time.Time
}

// UpdateProjectMemberRoleInput is the normalized input to change a member's role.
type UpdateProjectMemberRoleInput struct {
	ProjectID uuid.UUID
	UserID    uuid.UUID
	Role      ProjectRole
	UpdatedAt time.Time
}
