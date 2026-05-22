// Package domain contains user management domain types shared by the service and store adapter.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrUserNotFound is returned by the store when no user matches the lookup
// (e.g. by id). It lives in the domain package so store adapters do not
// need to import the service layer.
var ErrUserNotFound = errors.New("users: user not found")

type UserStatus string

const (
	UserStatusActive   UserStatus = "active"
	UserStatusDisabled UserStatus = "disabled"
)

type UserRole string

const (
	UserRoleAdmin UserRole = "admin"
	UserRoleUser  UserRole = "user"
)

type User struct {
	ID          uuid.UUID
	Email       string
	DisplayName string
	Status      UserStatus
	Role        UserRole
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ListUsersInput struct {
	Page     int
	PageSize int
	Offset   int
	Search   string
	Role     UserRole
	Status   UserStatus
}

type ListUsersResult struct {
	Users    []User
	Page     int
	PageSize int
	Total    int
}

// UpdateUserInput is the normalized input for an admin user update. Role and
// Status are pointers so a nil field leaves the corresponding column
// unchanged; at least one is non-nil by the time it reaches the store.
type UpdateUserInput struct {
	ID        uuid.UUID
	Role      *UserRole
	Status    *UserStatus
	UpdatedAt time.Time
}
