// Package domain contains user management domain types shared by the service and store adapter.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrUserNotFound is returned by the store when no user matches the lookup.
	ErrUserNotFound = errors.New("users: user not found")
	// ErrRoleNotFound is returned when a role id supplied for assignment does
	// not exist.
	ErrRoleNotFound = errors.New("users: role not found")
)

type UserStatus string

const (
	UserStatusActive   UserStatus = "active"
	UserStatusDisabled UserStatus = "disabled"
)

// User is a user enriched with the names of every role assigned to them and
// their profile fields. AvatarURL, Phone, JobTitle, and Company are empty when
// unset; LastLoginAt is nil until the user's first successful sign-in.
type User struct {
	ID            uuid.UUID
	Email         string
	DisplayName   string
	Status        UserStatus
	Roles         []string
	AvatarURL     string
	Phone         string
	JobTitle      string
	Company       string
	EmailVerified bool
	LastLoginAt   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type ListUsersInput struct {
	Page     int
	PageSize int
	Offset   int
	Search   string
	Role     string
	Status   UserStatus
}

type ListUsersResult struct {
	Users    []User
	Page     int
	PageSize int
	Total    int
}

// UpdateUserInput is the normalized input for an admin user update, applied
// atomically by the store. A nil Status leaves the status unchanged; a nil
// RoleIDs leaves role assignments unchanged, while a non-nil RoleIDs replaces
// the user's entire role set.
type UpdateUserInput struct {
	ID        uuid.UUID
	Status    *UserStatus
	RoleIDs   []uuid.UUID
	AvatarURL *string
	Phone     *string
	JobTitle  *string
	Company   *string
	UpdatedAt time.Time
}
