//go:build integration

package projectsrepo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/aklmans/wow-dashboard-api/internal/auth/password"
	"github.com/aklmans/wow-dashboard-api/internal/projects/domain"
	"github.com/aklmans/wow-dashboard-api/internal/store/projectsrepo"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/aklmans/wow-dashboard-api/internal/store/storetest"
)

func TestProjectStoreIntegration(t *testing.T) {
	ctx := context.Background()
	pool := storetest.NewPostgresPool(t, ctx, "test_projectsrepo_db", "../../../migrations")
	queries := query.New(pool)
	repo := projectsrepo.NewProjectStore(queries)

	ownerA := mustCreateUser(t, ctx, queries, "ada@example.com", "Ada")
	ownerB := mustCreateUser(t, ctx, queries, "grace@example.com", "Grace")

	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)

	t.Run("create + get round trip", func(t *testing.T) {
		input := domain.CreateProjectInput{
			ID:          uuid.New(),
			Name:        "Alpha",
			Description: "first project",
			Status:      domain.ProjectStatusActive,
			OwnerUserID: ownerA,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		created, err := repo.CreateProject(ctx, input)
		if err != nil {
			t.Fatalf("CreateProject failed: %v", err)
		}
		if created.ID != input.ID {
			t.Fatalf("created.ID = %s, want %s", created.ID, input.ID)
		}
		if created.Status != domain.ProjectStatusActive {
			t.Fatalf("status = %q, want active", created.Status)
		}

		got, err := repo.GetProjectByID(ctx, ownerA, input.ID)
		if err != nil {
			t.Fatalf("GetProjectByID failed: %v", err)
		}
		if got.ID != input.ID || got.OwnerUserID != ownerA || got.Name != "Alpha" {
			t.Fatalf("got = %#v", got)
		}
	})

	t.Run("list is owner scoped", func(t *testing.T) {
		// Insert one for ownerB
		_, err := repo.CreateProject(ctx, domain.CreateProjectInput{
			ID:          uuid.New(),
			Name:        "Bravo",
			Description: "owner B",
			Status:      domain.ProjectStatusActive,
			OwnerUserID: ownerB,
			CreatedAt:   now.Add(time.Minute),
			UpdatedAt:   now.Add(time.Minute),
		})
		if err != nil {
			t.Fatalf("CreateProject ownerB failed: %v", err)
		}

		listA, err := repo.ListProjects(ctx, domain.ListProjectsInput{
			OwnerUserID: ownerA, Page: 1, PageSize: 20,
		})
		if err != nil {
			t.Fatalf("ListProjects A: %v", err)
		}
		for _, p := range listA.Projects {
			if p.OwnerUserID != ownerA {
				t.Fatalf("ownerA list contained %s", p.OwnerUserID)
			}
		}

		listB, err := repo.ListProjects(ctx, domain.ListProjectsInput{
			OwnerUserID: ownerB, Page: 1, PageSize: 20,
		})
		if err != nil {
			t.Fatalf("ListProjects B: %v", err)
		}
		if listB.Total != 1 || len(listB.Projects) != 1 || listB.Projects[0].OwnerUserID != ownerB {
			t.Fatalf("ownerB list = %#v", listB)
		}
	})

	t.Run("search and status filters", func(t *testing.T) {
		// Add an archived project for ownerA matching "Gamma"
		_, err := repo.CreateProject(ctx, domain.CreateProjectInput{
			ID:          uuid.New(),
			Name:        "Gamma archived",
			Description: "rare phrase xyzzy",
			Status:      domain.ProjectStatusArchived,
			OwnerUserID: ownerA,
			CreatedAt:   now.Add(2 * time.Minute),
			UpdatedAt:   now.Add(2 * time.Minute),
		})
		if err != nil {
			t.Fatalf("CreateProject archived failed: %v", err)
		}

		bySearch, err := repo.ListProjects(ctx, domain.ListProjectsInput{
			OwnerUserID: ownerA, Page: 1, PageSize: 20, Search: "xyzzy",
		})
		if err != nil {
			t.Fatalf("ListProjects search: %v", err)
		}
		if bySearch.Total != 1 || len(bySearch.Projects) != 1 || bySearch.Projects[0].Name != "Gamma archived" {
			t.Fatalf("search result = %#v", bySearch)
		}

		archived, err := repo.ListProjects(ctx, domain.ListProjectsInput{
			OwnerUserID: ownerA, Page: 1, PageSize: 20, Status: domain.ProjectStatusArchived,
		})
		if err != nil {
			t.Fatalf("ListProjects status: %v", err)
		}
		if archived.Total != 1 || len(archived.Projects) != 1 || archived.Projects[0].Status != domain.ProjectStatusArchived {
			t.Fatalf("status filter result = %#v", archived)
		}
	})

	t.Run("missing id returns ErrProjectNotFound", func(t *testing.T) {
		_, err := repo.GetProjectByID(ctx, ownerA, uuid.New())
		if !errors.Is(err, domain.ErrProjectNotFound) {
			t.Fatalf("err = %v, want ErrProjectNotFound", err)
		}
	})

	t.Run("other owner cannot read project", func(t *testing.T) {
		input := domain.CreateProjectInput{
			ID:          uuid.New(),
			Name:        "Owned-by-A",
			Description: "",
			Status:      domain.ProjectStatusActive,
			OwnerUserID: ownerA,
			CreatedAt:   now.Add(3 * time.Minute),
			UpdatedAt:   now.Add(3 * time.Minute),
		}
		if _, err := repo.CreateProject(ctx, input); err != nil {
			t.Fatalf("CreateProject failed: %v", err)
		}

		_, err := repo.GetProjectByID(ctx, ownerB, input.ID)
		if !errors.Is(err, domain.ErrProjectNotFound) {
			t.Fatalf("err = %v, want ErrProjectNotFound when other owner reads", err)
		}
	})
}

func TestProjectStoreNameUniquenessIntegration(t *testing.T) {
	ctx := context.Background()
	pool := storetest.NewPostgresPool(t, ctx, "test_projectsrepo_name_unique_db", "../../../migrations")
	queries := query.New(pool)
	repo := projectsrepo.NewProjectStore(queries)

	ownerA := mustCreateUser(t, ctx, queries, "ada@example.com", "Ada")
	ownerB := mustCreateUser(t, ctx, queries, "grace@example.com", "Grace")
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)

	seed := func(t *testing.T, owner uuid.UUID, name string, status domain.ProjectStatus) domain.Project {
		t.Helper()
		project, err := repo.CreateProject(ctx, domain.CreateProjectInput{
			ID:          uuid.New(),
			Name:        name,
			Description: "desc",
			Status:      status,
			OwnerUserID: owner,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
		if err != nil {
			t.Fatalf("seed CreateProject %q: %v", name, err)
		}
		return project
	}

	t.Run("same owner duplicate create returns name conflict", func(t *testing.T) {
		seed(t, ownerA, "Duplicate Create", domain.ProjectStatusActive)

		_, err := repo.CreateProject(ctx, domain.CreateProjectInput{
			ID:          uuid.New(),
			Name:        "Duplicate Create",
			Description: "second",
			Status:      domain.ProjectStatusActive,
			OwnerUserID: ownerA,
			CreatedAt:   now.Add(time.Minute),
			UpdatedAt:   now.Add(time.Minute),
		})
		if !errors.Is(err, domain.ErrProjectNameAlreadyExists) {
			t.Fatalf("err = %v, want ErrProjectNameAlreadyExists", err)
		}
	})

	t.Run("different owners can use same name", func(t *testing.T) {
		seed(t, ownerA, "Shared Name", domain.ProjectStatusActive)

		created, err := repo.CreateProject(ctx, domain.CreateProjectInput{
			ID:          uuid.New(),
			Name:        "Shared Name",
			Description: "owner B",
			Status:      domain.ProjectStatusActive,
			OwnerUserID: ownerB,
			CreatedAt:   now.Add(2 * time.Minute),
			UpdatedAt:   now.Add(2 * time.Minute),
		})
		if err != nil {
			t.Fatalf("CreateProject for different owner: %v", err)
		}
		if created.OwnerUserID != ownerB || created.Name != "Shared Name" {
			t.Fatalf("created = %#v", created)
		}
	})

	t.Run("update to duplicate name under same owner returns name conflict", func(t *testing.T) {
		seed(t, ownerA, "Existing Target", domain.ProjectStatusActive)
		project := seed(t, ownerA, "Rename Source", domain.ProjectStatusActive)
		newName := "Existing Target"

		_, err := repo.UpdateProject(ctx, domain.UpdateProjectInput{
			ID:          project.ID,
			OwnerUserID: ownerA,
			Name:        &newName,
			UpdatedAt:   now.Add(3 * time.Minute),
		})
		if !errors.Is(err, domain.ErrProjectNameAlreadyExists) {
			t.Fatalf("err = %v, want ErrProjectNameAlreadyExists", err)
		}
	})

	t.Run("update to same current name succeeds", func(t *testing.T) {
		project := seed(t, ownerA, "Same Current Name", domain.ProjectStatusActive)
		sameName := "Same Current Name"

		updated, err := repo.UpdateProject(ctx, domain.UpdateProjectInput{
			ID:          project.ID,
			OwnerUserID: ownerA,
			Name:        &sameName,
			UpdatedAt:   now.Add(4 * time.Minute),
		})
		if err != nil {
			t.Fatalf("UpdateProject same name: %v", err)
		}
		if updated.ID != project.ID || updated.Name != sameName {
			t.Fatalf("updated = %#v", updated)
		}
	})

	t.Run("archived row still blocks duplicate name", func(t *testing.T) {
		seed(t, ownerA, "Archived Reserved", domain.ProjectStatusArchived)

		_, err := repo.CreateProject(ctx, domain.CreateProjectInput{
			ID:          uuid.New(),
			Name:        "Archived Reserved",
			Description: "new active",
			Status:      domain.ProjectStatusActive,
			OwnerUserID: ownerA,
			CreatedAt:   now.Add(5 * time.Minute),
			UpdatedAt:   now.Add(5 * time.Minute),
		})
		if !errors.Is(err, domain.ErrProjectNameAlreadyExists) {
			t.Fatalf("err = %v, want ErrProjectNameAlreadyExists", err)
		}
	})
}

func mustCreateUser(t *testing.T, ctx context.Context, queries *query.Queries, email, displayName string) uuid.UUID {
	t.Helper()
	hash, err := password.Hash("test-password")
	if err != nil {
		t.Fatalf("password.Hash: %v", err)
	}
	id := uuid.New()
	_, err = queries.CreateUser(ctx, query.CreateUserParams{
		ID:           pgtype.UUID{Bytes: id, Valid: true},
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: hash,
		Status:       "active",
		Role:         "user",
		CreatedAt:    pgtype.Timestamptz{Time: time.Now(), Valid: true},
		UpdatedAt:    pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateUser %s failed: %v", email, err)
	}
	return id
}

func TestProjectStoreUpdateProjectIntegration(t *testing.T) {
	ctx := context.Background()
	pool := storetest.NewPostgresPool(t, ctx, "test_projectsrepo_update_db", "../../../migrations")
	queries := query.New(pool)
	repo := projectsrepo.NewProjectStore(queries)

	ownerA := mustCreateUser(t, ctx, queries, "ada@example.com", "Ada")
	ownerB := mustCreateUser(t, ctx, queries, "grace@example.com", "Grace")

	createdAt := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	seed := func(t *testing.T, owner uuid.UUID, name, description string, status domain.ProjectStatus) domain.Project {
		t.Helper()
		project, err := repo.CreateProject(ctx, domain.CreateProjectInput{
			ID:          uuid.New(),
			Name:        name,
			Description: description,
			Status:      status,
			OwnerUserID: owner,
			CreatedAt:   createdAt,
			UpdatedAt:   createdAt,
		})
		if err != nil {
			t.Fatalf("seed CreateProject: %v", err)
		}
		return project
	}

	t.Run("updates name only and leaves other fields unchanged", func(t *testing.T) {
		project := seed(t, ownerA, "Original", "keep description", domain.ProjectStatusActive)
		newName := "Renamed"
		newUpdatedAt := createdAt.Add(time.Hour)

		updated, err := repo.UpdateProject(ctx, domain.UpdateProjectInput{
			ID:          project.ID,
			OwnerUserID: ownerA,
			Name:        &newName,
			UpdatedAt:   newUpdatedAt,
		})
		if err != nil {
			t.Fatalf("UpdateProject: %v", err)
		}
		if updated.Name != "Renamed" {
			t.Fatalf("name = %q, want Renamed", updated.Name)
		}
		if updated.Description != "keep description" {
			t.Fatalf("description = %q, want unchanged", updated.Description)
		}
		if updated.Status != domain.ProjectStatusActive {
			t.Fatalf("status = %q, want unchanged active", updated.Status)
		}
		if !updated.UpdatedAt.Equal(newUpdatedAt) {
			t.Fatalf("updated_at = %v, want %v", updated.UpdatedAt, newUpdatedAt)
		}
	})

	t.Run("updates status only", func(t *testing.T) {
		project := seed(t, ownerA, "Status target", "desc", domain.ProjectStatusActive)
		newStatus := domain.ProjectStatusArchived

		updated, err := repo.UpdateProject(ctx, domain.UpdateProjectInput{
			ID:          project.ID,
			OwnerUserID: ownerA,
			Status:      &newStatus,
			UpdatedAt:   createdAt.Add(time.Minute),
		})
		if err != nil {
			t.Fatalf("UpdateProject status: %v", err)
		}
		if updated.Status != domain.ProjectStatusArchived {
			t.Fatalf("status = %q, want archived", updated.Status)
		}
		if updated.Name != "Status target" || updated.Description != "desc" {
			t.Fatalf("non-status fields changed: %#v", updated)
		}
	})

	t.Run("empty description pointer clears the column", func(t *testing.T) {
		project := seed(t, ownerA, "Has desc", "to be cleared", domain.ProjectStatusActive)
		empty := ""

		updated, err := repo.UpdateProject(ctx, domain.UpdateProjectInput{
			ID:          project.ID,
			OwnerUserID: ownerA,
			Description: &empty,
			UpdatedAt:   createdAt.Add(time.Minute),
		})
		if err != nil {
			t.Fatalf("UpdateProject clear desc: %v", err)
		}
		if updated.Description != "" {
			t.Fatalf("description = %q, want empty", updated.Description)
		}
		if updated.Name != "Has desc" {
			t.Fatalf("name changed unexpectedly: %q", updated.Name)
		}
	})

	t.Run("wrong owner returns ErrProjectNotFound", func(t *testing.T) {
		project := seed(t, ownerA, "Owned by A", "", domain.ProjectStatusActive)
		newName := "hacked"

		_, err := repo.UpdateProject(ctx, domain.UpdateProjectInput{
			ID:          project.ID,
			OwnerUserID: ownerB,
			Name:        &newName,
			UpdatedAt:   createdAt.Add(time.Minute),
		})
		if !errors.Is(err, domain.ErrProjectNotFound) {
			t.Fatalf("err = %v, want ErrProjectNotFound", err)
		}

		got, err := repo.GetProjectByID(ctx, ownerA, project.ID)
		if err != nil {
			t.Fatalf("GetProjectByID after bad update: %v", err)
		}
		if got.Name != "Owned by A" {
			t.Fatalf("name was modified by wrong owner: %q", got.Name)
		}
	})

	t.Run("missing id returns ErrProjectNotFound", func(t *testing.T) {
		newName := "anything"
		_, err := repo.UpdateProject(ctx, domain.UpdateProjectInput{
			ID:          uuid.New(),
			OwnerUserID: ownerA,
			Name:        &newName,
			UpdatedAt:   createdAt,
		})
		if !errors.Is(err, domain.ErrProjectNotFound) {
			t.Fatalf("err = %v, want ErrProjectNotFound", err)
		}
	})
}

func TestProjectStoreArchiveProjectIntegration(t *testing.T) {
	ctx := context.Background()
	pool := storetest.NewPostgresPool(t, ctx, "test_projectsrepo_archive_db", "../../../migrations")
	queries := query.New(pool)
	repo := projectsrepo.NewProjectStore(queries)

	ownerA := mustCreateUser(t, ctx, queries, "ada@example.com", "Ada")
	ownerB := mustCreateUser(t, ctx, queries, "grace@example.com", "Grace")

	createdAt := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	seed := func(t *testing.T, owner uuid.UUID, name string, status domain.ProjectStatus) domain.Project {
		t.Helper()
		project, err := repo.CreateProject(ctx, domain.CreateProjectInput{
			ID:          uuid.New(),
			Name:        name,
			Description: "desc",
			Status:      status,
			OwnerUserID: owner,
			CreatedAt:   createdAt,
			UpdatedAt:   createdAt,
		})
		if err != nil {
			t.Fatalf("seed CreateProject: %v", err)
		}
		return project
	}

	t.Run("archives active project", func(t *testing.T) {
		project := seed(t, ownerA, "Archive Active", domain.ProjectStatusActive)
		newUpdatedAt := createdAt.Add(time.Hour)

		archived, err := repo.ArchiveProject(ctx, ownerA, project.ID, newUpdatedAt)
		if err != nil {
			t.Fatalf("ArchiveProject: %v", err)
		}
		if archived.Status != domain.ProjectStatusArchived {
			t.Fatalf("status = %q, want archived", archived.Status)
		}
		if !archived.UpdatedAt.Equal(newUpdatedAt) {
			t.Fatalf("updated_at = %v, want %v", archived.UpdatedAt, newUpdatedAt)
		}
		if archived.Name != "Archive Active" || archived.Description != "desc" {
			t.Fatalf("other fields changed: %#v", archived)
		}
	})

	t.Run("archive already archived is idempotent and refreshes updated_at", func(t *testing.T) {
		project := seed(t, ownerA, "Archive Idempotent", domain.ProjectStatusArchived)
		first := createdAt.Add(time.Hour)
		second := createdAt.Add(2 * time.Hour)

		if _, err := repo.ArchiveProject(ctx, ownerA, project.ID, first); err != nil {
			t.Fatalf("first archive: %v", err)
		}
		got, err := repo.ArchiveProject(ctx, ownerA, project.ID, second)
		if err != nil {
			t.Fatalf("second archive: %v", err)
		}
		if got.Status != domain.ProjectStatusArchived {
			t.Fatalf("status = %q, want archived", got.Status)
		}
		if !got.UpdatedAt.Equal(second) {
			t.Fatalf("updated_at = %v, want %v (refreshed)", got.UpdatedAt, second)
		}
	})

	t.Run("wrong owner returns ErrProjectNotFound and original row unchanged", func(t *testing.T) {
		project := seed(t, ownerA, "Archive Wrong Owner", domain.ProjectStatusActive)

		_, err := repo.ArchiveProject(ctx, ownerB, project.ID, createdAt.Add(time.Hour))
		if !errors.Is(err, domain.ErrProjectNotFound) {
			t.Fatalf("err = %v, want ErrProjectNotFound", err)
		}

		got, err := repo.GetProjectByID(ctx, ownerA, project.ID)
		if err != nil {
			t.Fatalf("GetProjectByID after wrong-owner archive: %v", err)
		}
		if got.Status != domain.ProjectStatusActive {
			t.Fatalf("status was modified by wrong owner: %q", got.Status)
		}
	})

	t.Run("missing id returns ErrProjectNotFound", func(t *testing.T) {
		_, err := repo.ArchiveProject(ctx, ownerA, uuid.New(), createdAt)
		if !errors.Is(err, domain.ErrProjectNotFound) {
			t.Fatalf("err = %v, want ErrProjectNotFound", err)
		}
	})
}
