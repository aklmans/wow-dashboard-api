// Package domain contains authentication domain types shared by services and store adapters.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

type UserStatus string

const (
	UserStatusActive   UserStatus = "active"
	UserStatusDisabled UserStatus = "disabled"
)

type User struct {
	ID            uuid.UUID
	Email         string
	DisplayName   string
	Status        UserStatus
	EmailVerified bool
	AvatarURL     string
	Phone         string
	JobTitle      string
	Company       string
	LastLoginAt   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// UpdateProfileInput is the self-service profile-edit input. Each pointer is
// nil to leave the corresponding field unchanged; admins use a different
// path that also accepts status and role changes.
type UpdateProfileInput struct {
	DisplayName *string
	AvatarURL   *string
	Phone       *string
	JobTitle    *string
	Company     *string
}

type AuthUser struct {
	User
	PasswordHash string
	// FailedLoginCount is the running count of consecutive failed sign-ins.
	FailedLoginCount int
	// LockedUntil, when set and in the future, means sign-in is locked.
	LockedUntil *time.Time
}

type CreateUserInput struct {
	ID           uuid.UUID
	Email        string
	DisplayName  string
	PasswordHash string
	Status       UserStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Role is a named role from the roles table.
type Role struct {
	ID   uuid.UUID
	Name string
}

type RefreshToken struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	TokenHash         string
	FamilyID          uuid.UUID
	ExpiresAt         time.Time
	RevokedAt         *time.Time
	ReplacedByTokenID *uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type CreateRefreshTokenInput struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TokenHash string
	FamilyID  uuid.UUID
	ExpiresAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Auth token purposes for the email-driven flows.
const (
	AuthTokenPurposePasswordReset     = "password_reset"
	AuthTokenPurposeEmailVerification = "email_verification"
)

// AuthToken is a one-time token backing an email-driven auth flow. Only the
// hash of the token is stored; the raw value is delivered by email.
type AuthToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Purpose   string
	TokenHash string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

type CreateAuthTokenInput struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Purpose   string
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
}

var (
	ErrUserNotFound         = errors.New("auth user store: user not found")
	ErrEmailAlreadyExists   = errors.New("auth user store: email already exists")
	ErrInvalidUserStatus    = errors.New("auth user store: invalid user status")
	ErrRoleNotFound         = errors.New("auth user store: role not found")
	ErrRefreshTokenNotFound = errors.New("auth refresh token store: token not found")
	ErrAuthTokenNotFound    = errors.New("auth token store: token not found")
)
