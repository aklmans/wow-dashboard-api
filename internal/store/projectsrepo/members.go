package projectsrepo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/aklmans/wow-dashboard-api/internal/projects/domain"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
)

// AddProjectMember grants a user access to a project. A user who is already a
// member surfaces as domain.ErrMemberAlreadyExists.
func (s *ProjectStore) AddProjectMember(ctx context.Context, input domain.AddProjectMemberInput) (domain.ProjectMember, error) {
	if s.queries == nil {
		return domain.ProjectMember{}, fmt.Errorf("projectsrepo: queries is nil")
	}

	row, err := s.queries.CreateProjectMember(ctx, query.CreateProjectMemberParams{
		ProjectID: pgUUIDFromDomain(input.ProjectID),
		UserID:    pgUUIDFromDomain(input.UserID),
		Role:      string(input.Role),
		CreatedAt: pgTime(input.CreatedAt),
		UpdatedAt: pgTime(input.UpdatedAt),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ProjectMember{}, domain.ErrMemberAlreadyExists
		}
		return domain.ProjectMember{}, fmt.Errorf("projectsrepo: add project member: %w", err)
	}
	return memberFromRow(row)
}

// ListProjectMembers returns every non-owner member of a project together with
// each member's email and display name.
func (s *ProjectStore) ListProjectMembers(ctx context.Context, projectID uuid.UUID) ([]domain.ProjectMemberDetail, error) {
	if s.queries == nil {
		return nil, fmt.Errorf("projectsrepo: queries is nil")
	}

	rows, err := s.queries.ListProjectMembers(ctx, pgUUIDFromDomain(projectID))
	if err != nil {
		return nil, fmt.Errorf("projectsrepo: list project members: %w", err)
	}

	members := make([]domain.ProjectMemberDetail, 0, len(rows))
	for _, row := range rows {
		member, err := memberDetailFromListRow(row)
		if err != nil {
			return nil, fmt.Errorf("projectsrepo: convert project member: %w", err)
		}
		members = append(members, member)
	}
	return members, nil
}

// GetProjectMember fetches a single membership row. A missing row surfaces as
// domain.ErrProjectMemberNotFound.
func (s *ProjectStore) GetProjectMember(ctx context.Context, projectID uuid.UUID, userID uuid.UUID) (domain.ProjectMember, error) {
	if s.queries == nil {
		return domain.ProjectMember{}, fmt.Errorf("projectsrepo: queries is nil")
	}

	row, err := s.queries.GetProjectMember(ctx, query.GetProjectMemberParams{
		ProjectID: pgUUIDFromDomain(projectID),
		UserID:    pgUUIDFromDomain(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ProjectMember{}, domain.ErrProjectMemberNotFound
		}
		return domain.ProjectMember{}, fmt.Errorf("projectsrepo: get project member: %w", err)
	}
	return memberFromRow(row)
}

// UpdateProjectMemberRole changes an existing member's role. A missing
// membership surfaces as domain.ErrProjectMemberNotFound.
func (s *ProjectStore) UpdateProjectMemberRole(ctx context.Context, input domain.UpdateProjectMemberRoleInput) (domain.ProjectMember, error) {
	if s.queries == nil {
		return domain.ProjectMember{}, fmt.Errorf("projectsrepo: queries is nil")
	}

	row, err := s.queries.UpdateProjectMemberRole(ctx, query.UpdateProjectMemberRoleParams{
		ProjectID: pgUUIDFromDomain(input.ProjectID),
		UserID:    pgUUIDFromDomain(input.UserID),
		Role:      string(input.Role),
		UpdatedAt: pgTime(input.UpdatedAt),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ProjectMember{}, domain.ErrProjectMemberNotFound
		}
		return domain.ProjectMember{}, fmt.Errorf("projectsrepo: update project member role: %w", err)
	}
	return memberFromRow(row)
}

// RemoveProjectMember revokes a user's access. A missing membership surfaces as
// domain.ErrProjectMemberNotFound.
func (s *ProjectStore) RemoveProjectMember(ctx context.Context, projectID uuid.UUID, userID uuid.UUID) error {
	if s.queries == nil {
		return fmt.Errorf("projectsrepo: queries is nil")
	}

	affected, err := s.queries.DeleteProjectMember(ctx, query.DeleteProjectMemberParams{
		ProjectID: pgUUIDFromDomain(projectID),
		UserID:    pgUUIDFromDomain(userID),
	})
	if err != nil {
		return fmt.Errorf("projectsrepo: remove project member: %w", err)
	}
	if affected == 0 {
		return domain.ErrProjectMemberNotFound
	}
	return nil
}

// FindUserByEmail resolves a normalized email to a user id so the service can
// share a project by email. An unknown email surfaces as
// domain.ErrMemberUserNotFound.
func (s *ProjectStore) FindUserByEmail(ctx context.Context, email string) (uuid.UUID, error) {
	if s.queries == nil {
		return uuid.Nil, fmt.Errorf("projectsrepo: queries is nil")
	}

	row, err := s.queries.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, domain.ErrMemberUserNotFound
		}
		return uuid.Nil, fmt.Errorf("projectsrepo: find user by email: %w", err)
	}
	if !row.ID.Valid {
		return uuid.Nil, fmt.Errorf("projectsrepo: invalid user id")
	}
	return uuid.UUID(row.ID.Bytes), nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == postgresUniqueViolation
}

func memberFromRow(row query.ProjectMember) (domain.ProjectMember, error) {
	if !row.ProjectID.Valid || !row.UserID.Valid {
		return domain.ProjectMember{}, fmt.Errorf("projectsrepo: invalid project member ids")
	}
	if !row.CreatedAt.Valid || !row.UpdatedAt.Valid {
		return domain.ProjectMember{}, fmt.Errorf("projectsrepo: invalid project member timestamps")
	}
	return domain.ProjectMember{
		ProjectID: uuid.UUID(row.ProjectID.Bytes),
		UserID:    uuid.UUID(row.UserID.Bytes),
		Role:      domain.ProjectRole(row.Role),
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}, nil
}

func memberDetailFromListRow(row query.ListProjectMembersRow) (domain.ProjectMemberDetail, error) {
	if !row.UserID.Valid {
		return domain.ProjectMemberDetail{}, fmt.Errorf("projectsrepo: invalid project member user id")
	}
	if !row.CreatedAt.Valid || !row.UpdatedAt.Valid {
		return domain.ProjectMemberDetail{}, fmt.Errorf("projectsrepo: invalid project member timestamps")
	}
	return domain.ProjectMemberDetail{
		UserID:      uuid.UUID(row.UserID.Bytes),
		Email:       row.Email,
		DisplayName: row.DisplayName,
		Role:        domain.ProjectRole(row.Role),
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}, nil
}
