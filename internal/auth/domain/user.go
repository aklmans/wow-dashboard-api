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

type AuthUser struct {
	User
	PasswordHash string
}

type CreateUserInput struct {
	ID           uuid.UUID
	Email        string
	DisplayName  string
	PasswordHash string
	Status       UserStatus
	Role         UserRole
	CreatedAt    time.Time
	UpdatedAt    time.Time
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

var (
	ErrUserNotFound         = errors.New("auth user store: user not found")
	ErrEmailAlreadyExists   = errors.New("auth user store: email already exists")
	ErrInvalidUserStatus    = errors.New("auth user store: invalid user status")
	ErrInvalidUserRole      = errors.New("auth user store: invalid user role")
	ErrRefreshTokenNotFound = errors.New("auth refresh token store: token not found")
)
