// Package projectsrepo is the postgres-backed adapter for the projects
// domain. It translates between sqlc generated rows / pgtype values and the
// domain types in internal/projects/domain. It must not depend on the
// service or HTTP layers.
package projectsrepo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/aklmans/wow-dashboard-api/internal/projects/domain"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
)

const (
	postgresUniqueViolation      = "23505"
	projectsOwnerNameUniqueIndex = "idx_projects_owner_name_unique"
)

// ProjectStore is the projects-domain repository backed by sqlc.
type ProjectStore struct {
	queries *query.Queries
}

// NewProjectStore wraps an existing *query.Queries.
func NewProjectStore(q *query.Queries) *ProjectStore {
	return &ProjectStore{queries: q}
}

// NewProjectStoreFromDB builds a ProjectStore from a pgx-compatible DBTX.
func NewProjectStoreFromDB(db query.DBTX) *ProjectStore {
	return NewProjectStore(query.New(db))
}

// CreateProject inserts a project row owned by input.OwnerUserID.
func (s *ProjectStore) CreateProject(ctx context.Context, input domain.CreateProjectInput) (domain.Project, error) {
	if s.queries == nil {
		return domain.Project{}, fmt.Errorf("projectsrepo: queries is nil")
	}

	row, err := s.queries.CreateProject(ctx, query.CreateProjectParams{
		ID:          pgUUIDFromDomain(input.ID),
		Name:        input.Name,
		Description: input.Description,
		Status:      string(input.Status),
		OwnerUserID: pgUUIDFromDomain(input.OwnerUserID),
		CreatedAt:   pgTime(input.CreatedAt),
		UpdatedAt:   pgTime(input.UpdatedAt),
	})
	if err != nil {
		if isProjectNameUniqueViolation(err) {
			return domain.Project{}, domain.ErrProjectNameAlreadyExists
		}
		return domain.Project{}, fmt.Errorf("projectsrepo: create project: %w", err)
	}
	return projectFromRow(row)
}

// ListProjects returns the paginated list of projects owned by
// input.OwnerUserID, applying optional search and status filters.
func (s *ProjectStore) ListProjects(ctx context.Context, input domain.ListProjectsInput) (domain.ListProjectsResult, error) {
	if s.queries == nil {
		return domain.ListProjectsResult{}, fmt.Errorf("projectsrepo: queries is nil")
	}

	owner := pgUUIDFromDomain(input.OwnerUserID)
	search := pgText(escapeLikePattern(input.Search))
	status := pgText(string(input.Status))

	total, err := s.queries.CountProjectsPage(ctx, query.CountProjectsPageParams{
		OwnerUserID: owner,
		Search:      search,
		Status:      status,
	})
	if err != nil {
		return domain.ListProjectsResult{}, fmt.Errorf("projectsrepo: count projects: %w", err)
	}

	rows, err := s.queries.ListProjectsPage(ctx, query.ListProjectsPageParams{
		OwnerUserID: owner,
		Search:      search,
		Status:      status,
		LimitVal:    int32(input.PageSize),
		OffsetVal:   int32(input.Offset),
	})
	if err != nil {
		return domain.ListProjectsResult{}, fmt.Errorf("projectsrepo: list projects: %w", err)
	}

	projects := make([]domain.Project, 0, len(rows))
	for _, row := range rows {
		project, err := projectFromRow(row)
		if err != nil {
			return domain.ListProjectsResult{}, fmt.Errorf("projectsrepo: convert project: %w", err)
		}
		projects = append(projects, project)
	}

	return domain.ListProjectsResult{
		Projects: projects,
		Page:     input.Page,
		PageSize: input.PageSize,
		Total:    int(total),
	}, nil
}

// GetProjectByID fetches a single project scoped to the owner. Missing rows
// (or rows owned by another user) are mapped to domain.ErrProjectNotFound.
func (s *ProjectStore) GetProjectByID(ctx context.Context, ownerUserID uuid.UUID, id uuid.UUID) (domain.Project, error) {
	if s.queries == nil {
		return domain.Project{}, fmt.Errorf("projectsrepo: queries is nil")
	}

	row, err := s.queries.GetProjectByID(ctx, query.GetProjectByIDParams{
		ID:          pgUUIDFromDomain(id),
		OwnerUserID: pgUUIDFromDomain(ownerUserID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Project{}, domain.ErrProjectNotFound
		}
		return domain.Project{}, fmt.Errorf("projectsrepo: get project: %w", err)
	}
	return projectFromRow(row)
}

// UpdateProject applies a partial update to a project owned by
// input.OwnerUserID. Nil pointer fields leave the column untouched; non-nil
// fields are applied verbatim (an empty Description string clears the field).
// Missing rows or rows owned by a different user surface as
// domain.ErrProjectNotFound.
func (s *ProjectStore) UpdateProject(ctx context.Context, input domain.UpdateProjectInput) (domain.Project, error) {
	if s.queries == nil {
		return domain.Project{}, fmt.Errorf("projectsrepo: queries is nil")
	}

	row, err := s.queries.UpdateProject(ctx, query.UpdateProjectParams{
		ID:          pgUUIDFromDomain(input.ID),
		OwnerUserID: pgUUIDFromDomain(input.OwnerUserID),
		Name:        pgTextPtr(input.Name),
		Description: pgTextPtr(input.Description),
		Status:      pgStatusPtr(input.Status),
		UpdatedAt:   pgTime(input.UpdatedAt),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Project{}, domain.ErrProjectNotFound
		}
		if isProjectNameUniqueViolation(err) {
			return domain.Project{}, domain.ErrProjectNameAlreadyExists
		}
		return domain.Project{}, fmt.Errorf("projectsrepo: update project: %w", err)
	}
	return projectFromRow(row)
}

// ArchiveProject marks the owner-scoped project row as archived and refreshes
// updated_at. It is idempotent: archiving an already-archived project succeeds
// and returns the row with a refreshed updated_at. Missing rows or rows owned
// by a different user surface as domain.ErrProjectNotFound.
func (s *ProjectStore) ArchiveProject(ctx context.Context, ownerUserID uuid.UUID, id uuid.UUID, updatedAt time.Time) (domain.Project, error) {
	if s.queries == nil {
		return domain.Project{}, fmt.Errorf("projectsrepo: queries is nil")
	}

	row, err := s.queries.ArchiveProject(ctx, query.ArchiveProjectParams{
		ID:          pgUUIDFromDomain(id),
		OwnerUserID: pgUUIDFromDomain(ownerUserID),
		UpdatedAt:   pgTime(updatedAt),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Project{}, domain.ErrProjectNotFound
		}
		return domain.Project{}, fmt.Errorf("projectsrepo: archive project: %w", err)
	}
	return projectFromRow(row)
}

func pgText(value string) pgtype.Text {
	value = strings.TrimSpace(value)
	return pgtype.Text{String: value, Valid: value != ""}
}

// escapeLikePattern escapes the ILIKE wildcard metacharacters so a user search
// term is matched literally rather than as a pattern (e.g. a term of "50%"
// matches the text "50%", not "50" followed by anything). PostgreSQL's default
// ILIKE escape character is backslash, so backslash is escaped first.
func escapeLikePattern(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

// pgTextPtr maps a partial-update string pointer to a pgtype.Text. Nil means
// "not provided" so the SQL COALESCE keeps the existing column value; a
// non-nil pointer (including an empty string) is treated as "provided" and
// becomes the new value.
func pgTextPtr(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

// pgStatusPtr is the ProjectStatus equivalent of pgTextPtr.
func pgStatusPtr(value *domain.ProjectStatus) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: string(*value), Valid: true}
}

func isProjectNameUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == postgresUniqueViolation &&
		pgErr.ConstraintName == projectsOwnerNameUniqueIndex
}

func pgUUIDFromDomain(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func pgTime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func projectFromRow(row query.Project) (domain.Project, error) {
	if !row.ID.Valid {
		return domain.Project{}, fmt.Errorf("projectsrepo: invalid project id")
	}
	if !row.OwnerUserID.Valid {
		return domain.Project{}, fmt.Errorf("projectsrepo: invalid owner_user_id")
	}
	if !row.CreatedAt.Valid || !row.UpdatedAt.Valid {
		return domain.Project{}, fmt.Errorf("projectsrepo: invalid timestamps")
	}
	return domain.Project{
		ID:          uuid.UUID(row.ID.Bytes),
		Name:        row.Name,
		Description: row.Description,
		Status:      domain.ProjectStatus(row.Status),
		OwnerUserID: uuid.UUID(row.OwnerUserID.Bytes),
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}, nil
}
