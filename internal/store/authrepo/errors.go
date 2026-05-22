package authrepo

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/aklmans/wow-dashboard-api/internal/auth/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	postgresUniqueViolation = "23505"
	postgresCheckViolation  = "23514"

	usersEmailUniqueConstraint = "users_email_unique"
	usersStatusConstraint      = "users_status_valid"
)

func mapStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		return domain.ErrUserNotFound
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case postgresUniqueViolation:
			if pgErr.ConstraintName == usersEmailUniqueConstraint {
				return domain.ErrEmailAlreadyExists
			}
		case postgresCheckViolation:
			if pgErr.ConstraintName == usersStatusConstraint {
				return domain.ErrInvalidUserStatus
			}
		}
	}

	return fmt.Errorf("authrepo: store operation failed: %w", err)
}

func mapRefreshTokenError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		return domain.ErrRefreshTokenNotFound
	}

	return fmt.Errorf("authrepo: refresh token store operation failed: %w", err)
}
