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

	t.Run("create then read back with owner access", func(t *testing.T) {
		input := domain.CreateProjectInput{
			ID:          uuid.New(),
			Name:        "Alpha",
			Description: "first project",
			Status:      domain.ProjectStatusActive,
			OwnerUserID: ownerA,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if _, err := repo.CreateProject(ctx, input); err != nil {
			t.Fatalf("CreateProject failed: %v", err)
		}

		access, err := repo.GetProjectWithAccess(ctx, input.ID, ownerA)
		if err != nil {
			t.Fatalf("GetProjectWithAccess failed: %v", err)
		}
		if access.Project.ID != input.ID || access.Project.OwnerUserID != ownerA {
			t.Fatalf("project = %#v", access.Project)
		}
		if access.AccessRole != domain.AccessRoleOwner {
			t.Fatalf("access role = %q, want owner", access.AccessRole)
		}
	})

	t.Run("list returns owned and shared projects", func(t *testing.T) {
		bravo := domain.CreateProjectInput{
			ID: uuid.New(), Name: "Bravo", Description: "owner B",
			Status: domain.ProjectStatusActive, OwnerUserID: ownerB,
			CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute),
		}
		if _, err := repo.CreateProject(ctx, bravo); err != nil {
			t.Fatalf("CreateProject ownerB failed: %v", err)
		}
		// Share Bravo with ownerA as a viewer.
		if _, err := repo.AddProjectMember(ctx, domain.AddProjectMemberInput{
			ProjectID: bravo.ID, UserID: ownerA, Role: domain.ProjectRoleViewer,
			CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute),
		}); err != nil {
			t.Fatalf("AddProjectMember failed: %v", err)
		}

		listA, err := repo.ListProjects(ctx, domain.ListProjectsInput{UserID: ownerA, Page: 1, PageSize: 20})
		if err != nil {
			t.Fatalf("ListProjects A: %v", err)
		}
		var sawShared bool
		for _, p := range listA.Projects {
			if p.ID == bravo.ID {
				sawShared = true
			}
		}
		if !sawShared {
			t.Fatal("ownerA list did not include the project shared with them")
		}

		listB, err := repo.ListProjects(ctx, domain.ListProjectsInput{UserID: ownerB, Page: 1, PageSize: 20})
		if err != nil {
			t.Fatalf("ListProjects B: %v", err)
		}
		if listB.Total != 1 || listB.Projects[0].OwnerUserID != ownerB {
			t.Fatalf("ownerB list = %#v", listB)
		}
	})

	t.Run("a member has member access; a stranger has none", func(t *testing.T) {
		input := domain.CreateProjectInput{
			ID: uuid.New(), Name: "Shared Project", Status: domain.ProjectStatusActive,
			OwnerUserID: ownerA, CreatedAt: now.Add(2 * time.Minute), UpdatedAt: now.Add(2 * time.Minute),
		}
		if _, err := repo.CreateProject(ctx, input); err != nil {
			t.Fatalf("CreateProject failed: %v", err)
		}
		if _, err := repo.AddProjectMember(ctx, domain.AddProjectMemberInput{
			ProjectID: input.ID, UserID: ownerB, Role: domain.ProjectRoleEditor,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("AddProjectMember failed: %v", err)
		}

		access, err := repo.GetProjectWithAccess(ctx, input.ID, ownerB)
		if err != nil {
			t.Fatalf("GetProjectWithAccess for member: %v", err)
		}
		if access.AccessRole != domain.AccessRoleEditor {
			t.Fatalf("member access role = %q, want editor", access.AccessRole)
		}

		stranger := mustCreateUser(t, ctx, queries, "stranger@example.com", "Stranger")
		if _, err := repo.GetProjectWithAccess(ctx, input.ID, stranger); !errors.Is(err, domain.ErrProjectNotFound) {
			t.Fatalf("stranger access err = %v, want ErrProjectNotFound", err)
		}
	})

	t.Run("missing id returns ErrProjectNotFound", func(t *testing.T) {
		if _, err := repo.GetProjectWithAccess(ctx, uuid.New(), ownerA); !errors.Is(err, domain.ErrProjectNotFound) {
			t.Fatalf("err = %v, want ErrProjectNotFound", err)
		}
	})
}

func TestProjectMembersIntegration(t *testing.T) {
	ctx := context.Background()
	pool := storetest.NewPostgresPool(t, ctx, "test_projectsrepo_members_db", "../../../migrations")
	queries := query.New(pool)
	repo := projectsrepo.NewProjectStore(queries)

	owner := mustCreateUser(t, ctx, queries, "owner@example.com", "Owner")
	member := mustCreateUser(t, ctx, queries, "member@example.com", "Member")
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)

	project, err := repo.CreateProject(ctx, domain.CreateProjectInput{
		ID: uuid.New(), Name: "Members Project", Status: domain.ProjectStatusActive,
		OwnerUserID: owner, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}

	t.Run("add, list, update, remove round trip", func(t *testing.T) {
		if _, err := repo.AddProjectMember(ctx, domain.AddProjectMemberInput{
			ProjectID: project.ID, UserID: member, Role: domain.ProjectRoleViewer,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("AddProjectMember: %v", err)
		}

		members, err := repo.ListProjectMembers(ctx, project.ID)
		if err != nil {
			t.Fatalf("ListProjectMembers: %v", err)
		}
		if len(members) != 1 || members[0].UserID != member || members[0].Email != "member@example.com" {
			t.Fatalf("members = %#v", members)
		}
		if members[0].Role != domain.ProjectRoleViewer {
			t.Fatalf("role = %q, want viewer", members[0].Role)
		}

		updated, err := repo.UpdateProjectMemberRole(ctx, domain.UpdateProjectMemberRoleInput{
			ProjectID: project.ID, UserID: member, Role: domain.ProjectRoleEditor,
			UpdatedAt: now.Add(time.Hour),
		})
		if err != nil {
			t.Fatalf("UpdateProjectMemberRole: %v", err)
		}
		if updated.Role != domain.ProjectRoleEditor {
			t.Fatalf("updated role = %q, want editor", updated.Role)
		}

		if err := repo.RemoveProjectMember(ctx, project.ID, member); err != nil {
			t.Fatalf("RemoveProjectMember: %v", err)
		}
		if _, err := repo.GetProjectMember(ctx, project.ID, member); !errors.Is(err, domain.ErrProjectMemberNotFound) {
			t.Fatalf("GetProjectMember after remove err = %v, want ErrProjectMemberNotFound", err)
		}
	})

	t.Run("adding the same member twice conflicts", func(t *testing.T) {
		repeat := mustCreateUser(t, ctx, queries, "repeat@example.com", "Repeat")
		add := domain.AddProjectMemberInput{
			ProjectID: project.ID, UserID: repeat, Role: domain.ProjectRoleViewer,
			CreatedAt: now, UpdatedAt: now,
		}
		if _, err := repo.AddProjectMember(ctx, add); err != nil {
			t.Fatalf("first AddProjectMember: %v", err)
		}
		if _, err := repo.AddProjectMember(ctx, add); !errors.Is(err, domain.ErrMemberAlreadyExists) {
			t.Fatalf("duplicate add err = %v, want ErrMemberAlreadyExists", err)
		}
	})

	t.Run("updating or removing a non-member returns not found", func(t *testing.T) {
		ghost := mustCreateUser(t, ctx, queries, "ghost@example.com", "Ghost")
		if _, err := repo.UpdateProjectMemberRole(ctx, domain.UpdateProjectMemberRoleInput{
			ProjectID: project.ID, UserID: ghost, Role: domain.ProjectRoleViewer, UpdatedAt: now,
		}); !errors.Is(err, domain.ErrProjectMemberNotFound) {
			t.Fatalf("update non-member err = %v, want ErrProjectMemberNotFound", err)
		}
		if err := repo.RemoveProjectMember(ctx, project.ID, ghost); !errors.Is(err, domain.ErrProjectMemberNotFound) {
			t.Fatalf("remove non-member err = %v, want ErrProjectMemberNotFound", err)
		}
	})

	t.Run("FindUserByEmail resolves and rejects", func(t *testing.T) {
		id, err := repo.FindUserByEmail(ctx, "member@example.com")
		if err != nil || id != member {
			t.Fatalf("FindUserByEmail = %s/%v, want %s/nil", id, err, member)
		}
		if _, err := repo.FindUserByEmail(ctx, "nobody@example.com"); !errors.Is(err, domain.ErrMemberUserNotFound) {
			t.Fatalf("FindUserByEmail unknown err = %v, want ErrMemberUserNotFound", err)
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
			ID: uuid.New(), Name: name, Description: "desc", Status: status,
			OwnerUserID: owner, CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			t.Fatalf("seed CreateProject %q: %v", name, err)
		}
		return project
	}

	t.Run("same owner duplicate create returns name conflict", func(t *testing.T) {
		seed(t, ownerA, "Duplicate Create", domain.ProjectStatusActive)
		_, err := repo.CreateProject(ctx, domain.CreateProjectInput{
			ID: uuid.New(), Name: "Duplicate Create", Description: "second",
			Status: domain.ProjectStatusActive, OwnerUserID: ownerA,
			CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute),
		})
		if !errors.Is(err, domain.ErrProjectNameAlreadyExists) {
			t.Fatalf("err = %v, want ErrProjectNameAlreadyExists", err)
		}
	})

	t.Run("different owners can use the same name", func(t *testing.T) {
		seed(t, ownerA, "Shared Name", domain.ProjectStatusActive)
		created, err := repo.CreateProject(ctx, domain.CreateProjectInput{
			ID: uuid.New(), Name: "Shared Name", Description: "owner B",
			Status: domain.ProjectStatusActive, OwnerUserID: ownerB,
			CreatedAt: now.Add(2 * time.Minute), UpdatedAt: now.Add(2 * time.Minute),
		})
		if err != nil {
			t.Fatalf("CreateProject for different owner: %v", err)
		}
		if created.OwnerUserID != ownerB {
			t.Fatalf("created owner = %s, want %s", created.OwnerUserID, ownerB)
		}
	})

	t.Run("update to a duplicate name returns name conflict", func(t *testing.T) {
		seed(t, ownerA, "Existing Target", domain.ProjectStatusActive)
		project := seed(t, ownerA, "Rename Source", domain.ProjectStatusActive)
		newName := "Existing Target"
		_, err := repo.UpdateProject(ctx, domain.UpdateProjectInput{
			ID: project.ID, Name: &newName, UpdatedAt: now.Add(3 * time.Minute),
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
	if _, err := queries.CreateUser(ctx, query.CreateUserParams{
		ID:           pgtype.UUID{Bytes: id, Valid: true},
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: hash,
		Status:       "active",
		Role:         "user",
		CreatedAt:    pgtype.Timestamptz{Time: time.Now(), Valid: true},
		UpdatedAt:    pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}); err != nil {
		t.Fatalf("CreateUser %s failed: %v", email, err)
	}
	return id
}

func TestProjectStoreUpdateProjectIntegration(t *testing.T) {
	ctx := context.Background()
	pool := storetest.NewPostgresPool(t, ctx, "test_projectsrepo_update_db", "../../../migrations")
	queries := query.New(pool)
	repo := projectsrepo.NewProjectStore(queries)

	owner := mustCreateUser(t, ctx, queries, "ada@example.com", "Ada")
	createdAt := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	seed := func(t *testing.T, name, description string, status domain.ProjectStatus) domain.Project {
		t.Helper()
		project, err := repo.CreateProject(ctx, domain.CreateProjectInput{
			ID: uuid.New(), Name: name, Description: description, Status: status,
			OwnerUserID: owner, CreatedAt: createdAt, UpdatedAt: createdAt,
		})
		if err != nil {
			t.Fatalf("seed CreateProject: %v", err)
		}
		return project
	}

	t.Run("updates name only and leaves other fields unchanged", func(t *testing.T) {
		project := seed(t, "Original", "keep description", domain.ProjectStatusActive)
		newName := "Renamed"
		newUpdatedAt := createdAt.Add(time.Hour)

		updated, err := repo.UpdateProject(ctx, domain.UpdateProjectInput{
			ID: project.ID, Name: &newName, UpdatedAt: newUpdatedAt,
		})
		if err != nil {
			t.Fatalf("UpdateProject: %v", err)
		}
		if updated.Name != "Renamed" || updated.Description != "keep description" {
			t.Fatalf("updated = %#v", updated)
		}
		if updated.Status != domain.ProjectStatusActive || !updated.UpdatedAt.Equal(newUpdatedAt) {
			t.Fatalf("updated = %#v", updated)
		}
	})

	t.Run("empty description pointer clears the column", func(t *testing.T) {
		project := seed(t, "Has desc", "to be cleared", domain.ProjectStatusActive)
		empty := ""
		updated, err := repo.UpdateProject(ctx, domain.UpdateProjectInput{
			ID: project.ID, Description: &empty, UpdatedAt: createdAt.Add(time.Minute),
		})
		if err != nil {
			t.Fatalf("UpdateProject clear desc: %v", err)
		}
		if updated.Description != "" || updated.Name != "Has desc" {
			t.Fatalf("updated = %#v", updated)
		}
	})

	t.Run("missing id returns ErrProjectNotFound", func(t *testing.T) {
		newName := "anything"
		_, err := repo.UpdateProject(ctx, domain.UpdateProjectInput{
			ID: uuid.New(), Name: &newName, UpdatedAt: createdAt,
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
			ID: uuid.New(), Name: name, Description: "desc", Status: status,
			OwnerUserID: owner, CreatedAt: createdAt, UpdatedAt: createdAt,
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
		if archived.Status != domain.ProjectStatusArchived || !archived.UpdatedAt.Equal(newUpdatedAt) {
			t.Fatalf("archived = %#v", archived)
		}
	})

	t.Run("archive remains owner scoped in the store", func(t *testing.T) {
		project := seed(t, ownerA, "Archive Wrong Owner", domain.ProjectStatusActive)
		if _, err := repo.ArchiveProject(ctx, ownerB, project.ID, createdAt.Add(time.Hour)); !errors.Is(err, domain.ErrProjectNotFound) {
			t.Fatalf("err = %v, want ErrProjectNotFound", err)
		}
		access, err := repo.GetProjectWithAccess(ctx, project.ID, ownerA)
		if err != nil {
			t.Fatalf("GetProjectWithAccess after wrong-owner archive: %v", err)
		}
		if access.Project.Status != domain.ProjectStatusActive {
			t.Fatalf("status was modified by a non-owner: %q", access.Project.Status)
		}
	})

	t.Run("missing id returns ErrProjectNotFound", func(t *testing.T) {
		if _, err := repo.ArchiveProject(ctx, ownerA, uuid.New(), createdAt); !errors.Is(err, domain.ErrProjectNotFound) {
			t.Fatalf("err = %v, want ErrProjectNotFound", err)
		}
	})
}
