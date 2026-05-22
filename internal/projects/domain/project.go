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
type ListProjectsInput struct {
	OwnerUserID uuid.UUID
	Page        int
	PageSize    int
	Offset      int
	Search      string
	Status      ProjectStatus
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
// string for description). UpdatedAt is always advanced by the service.
type UpdateProjectInput struct {
	ID          uuid.UUID
	OwnerUserID uuid.UUID
	Name        *string
	Description *string
	Status      *ProjectStatus
	UpdatedAt   time.Time
}
