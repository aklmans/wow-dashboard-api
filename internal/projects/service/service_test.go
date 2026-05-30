package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/projects/domain"
	"github.com/aklmans/wow-dashboard-api/internal/projects/service"
)

func ownerAccess(project domain.Project) domain.ProjectAccess {
	return domain.ProjectAccess{Project: project, AccessRole: domain.AccessRoleOwner}
}

func longString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}

// --- ListProjects -----------------------------------------------------------

func TestServiceListProjectsNormalizesInput(t *testing.T) {
	store := &fakeProjectStore{}
	svc := service.NewService(store)

	user := uuid.New()
	if _, err := svc.ListProjects(context.Background(), service.ListProjectsInput{
		UserID: "  " + user.String() + "  ",
		Search: "  Demo  ",
		Status: "ACTIVE",
	}); err != nil {
		t.Fatalf("ListProjects returned error: %v", err)
	}

	if store.listInput.UserID != user {
		t.Fatalf("user = %s, want %s", store.listInput.UserID, user)
	}
	if store.listInput.Page != 1 || store.listInput.PageSize != 20 {
		t.Fatalf("pagination = %d/%d, want 1/20", store.listInput.Page, store.listInput.PageSize)
	}
	if store.listInput.Search != "Demo" {
		t.Fatalf("search = %q, want Demo", store.listInput.Search)
	}
	if store.listInput.Status != domain.ProjectStatusActive {
		t.Fatalf("status = %q, want active", store.listInput.Status)
	}
}

func TestServiceListProjectsRejectsInvalidInput(t *testing.T) {
	user := uuid.New().String()
	tests := []struct {
		name  string
		input service.ListProjectsInput
	}{
		{name: "invalid user", input: service.ListProjectsInput{UserID: "not-a-uuid"}},
		{name: "missing user", input: service.ListProjectsInput{UserID: ""}},
		{name: "negative page", input: service.ListProjectsInput{UserID: user, Page: -1}},
		{name: "too large page size", input: service.ListProjectsInput{UserID: user, PageSize: 101}},
		{name: "invalid status", input: service.ListProjectsInput{UserID: user, Status: "pending"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeProjectStore{}
			svc := service.NewService(store)
			if _, err := svc.ListProjects(context.Background(), tt.input); !errors.Is(err, service.ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
			if store.listCalled {
				t.Fatal("store.ListProjects was called for invalid input")
			}
		})
	}
}

// --- GetProject -------------------------------------------------------------

func TestServiceGetProjectReturnsAccessibleProject(t *testing.T) {
	user := uuid.New()
	id := uuid.New()
	store := &fakeProjectStore{getAccess: domain.ProjectAccess{
		Project:    domain.Project{ID: id, OwnerUserID: user, Name: "demo"},
		AccessRole: domain.AccessRoleViewer,
	}}
	svc := service.NewService(store)

	got, err := svc.GetProject(context.Background(), user.String(), "  "+id.String()+"  ")
	if err != nil {
		t.Fatalf("GetProject error: %v", err)
	}
	if store.getProjectID != id || store.getUserID != user {
		t.Fatalf("store args = project %s user %s, want %s/%s", store.getProjectID, store.getUserID, id, user)
	}
	if got.ID != id {
		t.Fatalf("returned id = %s, want %s", got.ID, id)
	}
}

func TestServiceGetProjectRejectsInvalidIDs(t *testing.T) {
	user := uuid.New().String()
	cases := []struct{ name, user, project string }{
		{"invalid user", "not-a-uuid", uuid.New().String()},
		{"invalid project id", user, "not-a-uuid"},
		{"empty project id", user, "   "},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeProjectStore{}
			svc := service.NewService(store)
			if _, err := svc.GetProject(context.Background(), tt.user, tt.project); !errors.Is(err, service.ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
			if store.getCalled {
				t.Fatalf("store was called for invalid input %#v", tt)
			}
		})
	}
}

func TestServiceGetProjectMapsNotFound(t *testing.T) {
	store := &fakeProjectStore{getErr: domain.ErrProjectNotFound}
	svc := service.NewService(store)

	if _, err := svc.GetProject(context.Background(), uuid.New().String(), uuid.New().String()); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("err = %v, want service.ErrNotFound", err)
	}
}

// --- CreateProject ----------------------------------------------------------

func TestServiceCreateProjectNormalizesInput(t *testing.T) {
	owner := uuid.New()
	fixedNow := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	store := &fakeProjectStore{}
	svc := service.NewService(store, service.WithClock(func() time.Time { return fixedNow }))

	if _, err := svc.CreateProject(context.Background(), service.CreateProjectInput{
		OwnerUserID: owner.String(),
		Name:        "  Demo Project  ",
		Description: "  hello  ",
	}); err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}

	if store.createInput.Name != "Demo Project" {
		t.Fatalf("name = %q, want trimmed", store.createInput.Name)
	}
	if store.createInput.Description != "hello" {
		t.Fatalf("description = %q, want trimmed", store.createInput.Description)
	}
	if store.createInput.Status != domain.ProjectStatusActive {
		t.Fatalf("status = %q, want active default", store.createInput.Status)
	}
	if store.createInput.OwnerUserID != owner {
		t.Fatalf("owner = %s, want %s", store.createInput.OwnerUserID, owner)
	}
	if store.createInput.ID == uuid.Nil {
		t.Fatal("project id is zero uuid")
	}
	if !store.createInput.CreatedAt.Equal(fixedNow) || !store.createInput.UpdatedAt.Equal(fixedNow) {
		t.Fatalf("timestamps = %v/%v, want %v", store.createInput.CreatedAt, store.createInput.UpdatedAt, fixedNow)
	}
}

func TestServiceCreateProjectRejectsInvalidInput(t *testing.T) {
	owner := uuid.New().String()
	tests := []struct {
		name  string
		input service.CreateProjectInput
	}{
		{"invalid owner", service.CreateProjectInput{OwnerUserID: "bad", Name: "x"}},
		{"empty name", service.CreateProjectInput{OwnerUserID: owner, Name: "   "}},
		{"name too long", service.CreateProjectInput{OwnerUserID: owner, Name: longString(121)}},
		{"description too long", service.CreateProjectInput{OwnerUserID: owner, Name: "ok", Description: longString(2001)}},
		{"invalid status", service.CreateProjectInput{OwnerUserID: owner, Name: "ok", Status: "deleted"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeProjectStore{}
			svc := service.NewService(store)
			if _, err := svc.CreateProject(context.Background(), tt.input); !errors.Is(err, service.ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
			if store.createCalled {
				t.Fatalf("store was called for invalid input %s", tt.name)
			}
		})
	}
}

func TestServiceCreateProjectMapsNameConflictWithoutAudit(t *testing.T) {
	recorder := &fakeAuditRecorder{}
	store := &fakeProjectStore{createErr: domain.ErrProjectNameAlreadyExists}
	svc := service.NewService(store, service.WithAuditRecorder(recorder))

	if _, err := svc.CreateProject(context.Background(), service.CreateProjectInput{
		OwnerUserID: uuid.New().String(),
		Name:        "Demo",
	}); !errors.Is(err, service.ErrNameConflict) {
		t.Fatalf("err = %v, want ErrNameConflict", err)
	}
	if len(recorder.calls) != 0 {
		t.Fatalf("audit calls = %d, want 0 for name conflict", len(recorder.calls))
	}
}

func TestServiceCreateProjectRecordsAudit(t *testing.T) {
	owner := uuid.New()
	created := domain.Project{ID: uuid.New(), OwnerUserID: owner, Status: domain.ProjectStatusActive}
	recorder := &fakeAuditRecorder{}
	svc := service.NewService(&fakeProjectStore{createResult: created}, service.WithAuditRecorder(recorder))

	if _, err := svc.CreateProject(context.Background(), service.CreateProjectInput{
		OwnerUserID: owner.String(),
		Name:        "Demo",
	}); err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}
	if len(recorder.calls) != 1 || recorder.calls[0].EventType != service.EventProjectCreated {
		t.Fatalf("audit calls = %#v, want one project.created", recorder.calls)
	}
}

// --- UpdateProject ----------------------------------------------------------

func TestServiceUpdateProjectEditorForwardsNormalizedInput(t *testing.T) {
	user := uuid.New()
	id := uuid.New()
	fixedNow := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	store := &fakeProjectStore{
		getAccess:    domain.ProjectAccess{Project: domain.Project{ID: id}, AccessRole: domain.AccessRoleEditor},
		updateResult: domain.Project{ID: id, Name: "New"},
	}
	svc := service.NewService(store, service.WithClock(func() time.Time { return fixedNow }))

	name := "  New Name  "
	description := "  hello  "
	status := "ARCHIVED"
	if _, err := svc.UpdateProject(context.Background(), service.UpdateProjectInput{
		UserID:      user.String(),
		ID:          id.String(),
		Name:        &name,
		Description: &description,
		Status:      &status,
	}); err != nil {
		t.Fatalf("UpdateProject error: %v", err)
	}
	if !store.updateCalled {
		t.Fatal("store.UpdateProject was not called")
	}
	if store.updateInput.ID != id {
		t.Fatalf("forwarded id = %s, want %s", store.updateInput.ID, id)
	}
	if store.updateInput.Name == nil || *store.updateInput.Name != "New Name" {
		t.Fatalf("name forwarded = %v, want trimmed", store.updateInput.Name)
	}
	if store.updateInput.Description == nil || *store.updateInput.Description != "hello" {
		t.Fatalf("description forwarded = %v, want trimmed", store.updateInput.Description)
	}
	if store.updateInput.Status == nil || *store.updateInput.Status != domain.ProjectStatusArchived {
		t.Fatalf("status forwarded = %v, want archived", store.updateInput.Status)
	}
	if !store.updateInput.UpdatedAt.Equal(fixedNow) {
		t.Fatalf("updatedAt = %v, want %v", store.updateInput.UpdatedAt, fixedNow)
	}
}

func TestServiceUpdateProjectViewerIsForbidden(t *testing.T) {
	id := uuid.New()
	store := &fakeProjectStore{getAccess: domain.ProjectAccess{
		Project: domain.Project{ID: id}, AccessRole: domain.AccessRoleViewer,
	}}
	svc := service.NewService(store)

	name := "New"
	_, err := svc.UpdateProject(context.Background(), service.UpdateProjectInput{
		UserID: uuid.New().String(),
		ID:     id.String(),
		Name:   &name,
	})
	if !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if store.updateCalled {
		t.Fatal("store.UpdateProject was called for a viewer")
	}
}

func TestServiceUpdateProjectAllowsEmptyDescription(t *testing.T) {
	id := uuid.New()
	store := &fakeProjectStore{
		getAccess:    ownerAccess(domain.Project{ID: id}),
		updateResult: domain.Project{ID: id},
	}
	svc := service.NewService(store)

	empty := ""
	if _, err := svc.UpdateProject(context.Background(), service.UpdateProjectInput{
		UserID:      uuid.New().String(),
		ID:          id.String(),
		Description: &empty,
	}); err != nil {
		t.Fatalf("UpdateProject error: %v", err)
	}
	if store.updateInput.Description == nil || *store.updateInput.Description != "" {
		t.Fatalf("description forwarded = %v, want empty string pointer", store.updateInput.Description)
	}
	if store.updateInput.Name != nil || store.updateInput.Status != nil {
		t.Fatal("unprovided fields should remain nil")
	}
}

func TestServiceUpdateProjectRejectsInvalidInput(t *testing.T) {
	user := uuid.New().String()
	id := uuid.New().String()
	emptyStr, tooLongName, tooLongDesc := "", longString(121), longString(2001)
	emptyName, badStatus, okName := "   ", "deleted", "ok"
	tests := []struct {
		name  string
		input service.UpdateProjectInput
	}{
		{"invalid user", service.UpdateProjectInput{UserID: "bad", ID: id, Name: &okName}},
		{"invalid id", service.UpdateProjectInput{UserID: user, ID: "bad", Name: &okName}},
		{"empty body", service.UpdateProjectInput{UserID: user, ID: id}},
		{"empty name pointer", service.UpdateProjectInput{UserID: user, ID: id, Name: &emptyStr}},
		{"whitespace name", service.UpdateProjectInput{UserID: user, ID: id, Name: &emptyName}},
		{"name too long", service.UpdateProjectInput{UserID: user, ID: id, Name: &tooLongName}},
		{"description too long", service.UpdateProjectInput{UserID: user, ID: id, Description: &tooLongDesc}},
		{"invalid status", service.UpdateProjectInput{UserID: user, ID: id, Status: &badStatus}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeProjectStore{getAccess: ownerAccess(domain.Project{})}
			svc := service.NewService(store)
			if _, err := svc.UpdateProject(context.Background(), tt.input); !errors.Is(err, service.ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
			if store.updateCalled {
				t.Fatalf("store called for invalid input %s", tt.name)
			}
		})
	}
}

func TestServiceUpdateProjectMapsNotFound(t *testing.T) {
	store := &fakeProjectStore{getErr: domain.ErrProjectNotFound}
	svc := service.NewService(store)

	name := "x"
	if _, err := svc.UpdateProject(context.Background(), service.UpdateProjectInput{
		UserID: uuid.New().String(),
		ID:     uuid.New().String(),
		Name:   &name,
	}); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestServiceUpdateProjectMapsNameConflictWithoutAudit(t *testing.T) {
	id := uuid.New()
	recorder := &fakeAuditRecorder{}
	store := &fakeProjectStore{
		getAccess: ownerAccess(domain.Project{ID: id}),
		updateErr: domain.ErrProjectNameAlreadyExists,
	}
	svc := service.NewService(store, service.WithAuditRecorder(recorder))

	name := "Demo"
	if _, err := svc.UpdateProject(context.Background(), service.UpdateProjectInput{
		UserID: uuid.New().String(),
		ID:     id.String(),
		Name:   &name,
	}); !errors.Is(err, service.ErrNameConflict) {
		t.Fatalf("err = %v, want ErrNameConflict", err)
	}
	if len(recorder.calls) != 0 {
		t.Fatalf("audit calls = %d, want 0 for name conflict", len(recorder.calls))
	}
}

func TestServiceUpdateProjectRecordsAuditWithChangedFields(t *testing.T) {
	id := uuid.New()
	recorder := &fakeAuditRecorder{}
	store := &fakeProjectStore{
		getAccess:    ownerAccess(domain.Project{ID: id}),
		updateResult: domain.Project{ID: id, Status: domain.ProjectStatusArchived},
	}
	svc := service.NewService(store, service.WithAuditRecorder(recorder))

	name, desc, status := "New Name", "  hello  ", "archived"
	if _, err := svc.UpdateProject(context.Background(), service.UpdateProjectInput{
		UserID: uuid.New().String(), ID: id.String(),
		Name: &name, Description: &desc, Status: &status,
	}); err != nil {
		t.Fatalf("UpdateProject error: %v", err)
	}
	if len(recorder.calls) != 1 || recorder.calls[0].EventType != service.EventProjectUpdated {
		t.Fatalf("audit = %#v, want one project.updated", recorder.calls)
	}
	want := []string{"name", "description", "status"}
	if got := recorder.calls[0].Metadata.ChangedFields; len(got) != len(want) {
		t.Fatalf("changed_fields = %v, want %v", got, want)
	}
}

// --- ArchiveProject ---------------------------------------------------------

func TestServiceArchiveProjectOwnerSucceeds(t *testing.T) {
	owner := uuid.New()
	id := uuid.New()
	fixedNow := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	store := &fakeProjectStore{
		getAccess:     ownerAccess(domain.Project{ID: id, OwnerUserID: owner}),
		archiveResult: domain.Project{ID: id, Status: domain.ProjectStatusArchived},
	}
	recorder := &fakeAuditRecorder{}
	svc := service.NewService(store, service.WithClock(func() time.Time { return fixedNow }), service.WithAuditRecorder(recorder))

	got, err := svc.ArchiveProject(context.Background(), "  "+owner.String()+"  ", id.String())
	if err != nil {
		t.Fatalf("ArchiveProject error: %v", err)
	}
	if !store.archiveCalled || store.archiveID != id || store.archiveOwnerID != owner {
		t.Fatalf("archive args = owner %s id %s, want %s/%s", store.archiveOwnerID, store.archiveID, owner, id)
	}
	if !store.archiveUpdatedAt.Equal(fixedNow) {
		t.Fatalf("updatedAt = %v, want %v", store.archiveUpdatedAt, fixedNow)
	}
	if got.Status != domain.ProjectStatusArchived {
		t.Fatalf("status = %q, want archived", got.Status)
	}
	if len(recorder.calls) != 1 || recorder.calls[0].EventType != service.EventProjectArchived {
		t.Fatalf("audit = %#v, want one project.archived", recorder.calls)
	}
}

func TestServiceArchiveProjectNonOwnerIsForbidden(t *testing.T) {
	id := uuid.New()
	for _, role := range []domain.AccessRole{domain.AccessRoleEditor, domain.AccessRoleViewer} {
		store := &fakeProjectStore{getAccess: domain.ProjectAccess{
			Project: domain.Project{ID: id}, AccessRole: role,
		}}
		svc := service.NewService(store)
		_, err := svc.ArchiveProject(context.Background(), uuid.New().String(), id.String())
		if !errors.Is(err, service.ErrForbidden) {
			t.Fatalf("role %q: err = %v, want ErrForbidden", role, err)
		}
		if store.archiveCalled {
			t.Fatalf("role %q: store.ArchiveProject was called", role)
		}
	}
}

func TestServiceArchiveProjectMapsNotFound(t *testing.T) {
	store := &fakeProjectStore{getErr: domain.ErrProjectNotFound}
	svc := service.NewService(store)
	if _, err := svc.ArchiveProject(context.Background(), uuid.New().String(), uuid.New().String()); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// --- Audit safety -----------------------------------------------------------

func TestServiceAuditMetadataDoesNotLeakBusinessText(t *testing.T) {
	owner := uuid.New()
	id := uuid.New()
	recorder := &fakeAuditRecorder{}

	createSvc := service.NewService(&fakeProjectStore{
		createResult: domain.Project{ID: id, OwnerUserID: owner, Status: domain.ProjectStatusActive},
	}, service.WithAuditRecorder(recorder))
	if _, err := createSvc.CreateProject(context.Background(), service.CreateProjectInput{
		OwnerUserID: owner.String(),
		Name:        "Top Secret Name",
		Description: "leaky-token-ABC123 password=hunter2",
	}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	for _, event := range recorder.calls {
		raw, err := json.Marshal(event.Metadata)
		if err != nil {
			t.Fatalf("marshal metadata: %v", err)
		}
		body := strings.ToLower(string(raw))
		for _, forbidden := range []string{"top secret name", "hunter2", "leaky-token", "password", "\"name\":\"", "\"description\":\""} {
			if strings.Contains(body, strings.ToLower(forbidden)) {
				t.Fatalf("metadata leaks %q: %s", forbidden, raw)
			}
		}
	}
}

// --- Transactional audit (unit of work) ------------------------------------

func TestServiceCreateProjectTransactionalRecordsAuditInSameUnit(t *testing.T) {
	owner := uuid.New()
	created := domain.Project{ID: uuid.New(), OwnerUserID: owner, Status: domain.ProjectStatusActive}
	store := &fakeProjectStore{createResult: created}
	audit := &fakeAuditRecorder{}
	uow := &fakeProjectUnitOfWork{mutator: store, recorder: audit}
	svc := service.NewService(&fakeProjectStore{}, service.WithUnitOfWork(uow))

	if _, err := svc.CreateProject(context.Background(), service.CreateProjectInput{
		OwnerUserID: owner.String(),
		Name:        "Demo",
	}); err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}
	if !uow.committed {
		t.Fatal("unit of work did not commit")
	}
	if !store.createCalled {
		t.Fatal("mutation did not run inside the unit of work")
	}
	if len(audit.calls) != 1 || audit.calls[0].EventType != service.EventProjectCreated {
		t.Fatalf("audit = %#v, want one project.created in the same unit", audit.calls)
	}
}

func TestServiceCreateProjectTransactionalAuditFailureRollsBack(t *testing.T) {
	owner := uuid.New()
	store := &fakeProjectStore{createResult: domain.Project{ID: uuid.New(), OwnerUserID: owner, Status: domain.ProjectStatusActive}}
	audit := &fakeAuditRecorder{err: errors.New("audit insert failed")}
	uow := &fakeProjectUnitOfWork{mutator: store, recorder: audit}
	svc := service.NewService(&fakeProjectStore{}, service.WithUnitOfWork(uow))

	if _, err := svc.CreateProject(context.Background(), service.CreateProjectInput{
		OwnerUserID: owner.String(),
		Name:        "Demo",
	}); err == nil {
		t.Fatal("CreateProject should fail when the audit write fails in the unit of work")
	}
	if !store.createCalled {
		t.Fatal("mutation should have been attempted inside the unit of work")
	}
	if uow.committed {
		t.Fatal("unit of work must not commit when the audit write fails")
	}
}

// --- fakes ------------------------------------------------------------------

// fakeProjectUnitOfWork runs the work function with the configured mutator and
// recorder, committing only when the function returns nil.
type fakeProjectUnitOfWork struct {
	mutator   service.ProjectMutator
	recorder  service.AuditRecorder
	emitter   service.NotificationEmitter
	committed bool
}

func (f *fakeProjectUnitOfWork) Do(ctx context.Context, fn func(context.Context, service.WorkDeps) error) error {
	if err := fn(ctx, service.WorkDeps{Projects: f.mutator, Audit: f.recorder, Notifications: f.emitter}); err != nil {
		return err
	}
	f.committed = true
	return nil
}

type fakeAuditRecorder struct {
	calls []service.AuditEvent
	err   error
}

func (f *fakeAuditRecorder) RecordProjectEvent(ctx context.Context, event service.AuditEvent) error {
	f.calls = append(f.calls, event)
	return f.err
}

type fakeProjectStore struct {
	listCalled bool
	listInput  domain.ListProjectsInput
	listResult domain.ListProjectsResult
	listErr    error

	getCalled    bool
	getProjectID uuid.UUID
	getUserID    uuid.UUID
	getAccess    domain.ProjectAccess
	getErr       error

	createCalled bool
	createInput  domain.CreateProjectInput
	createResult domain.Project
	createErr    error

	updateCalled bool
	updateInput  domain.UpdateProjectInput
	updateResult domain.Project
	updateErr    error

	archiveCalled    bool
	archiveOwnerID   uuid.UUID
	archiveID        uuid.UUID
	archiveUpdatedAt time.Time
	archiveResult    domain.Project
	archiveErr       error

	addMemberCalled bool
	addMemberInput  domain.AddProjectMemberInput
	addMemberResult domain.ProjectMember
	addMemberErr    error

	listMembersResult []domain.ProjectMemberDetail
	listMembersErr    error

	updateMemberInput  domain.UpdateProjectMemberRoleInput
	updateMemberResult domain.ProjectMember
	updateMemberErr    error

	removeMemberCalled  bool
	removeMemberProject uuid.UUID
	removeMemberUser    uuid.UUID
	removeMemberErr     error

	findEmailInput  string
	findEmailResult uuid.UUID
	findEmailErr    error
}

func (f *fakeProjectStore) ListProjects(ctx context.Context, input domain.ListProjectsInput) (domain.ListProjectsResult, error) {
	f.listCalled = true
	f.listInput = input
	return f.listResult, f.listErr
}

func (f *fakeProjectStore) GetProjectWithAccess(ctx context.Context, projectID uuid.UUID, userID uuid.UUID) (domain.ProjectAccess, error) {
	f.getCalled = true
	f.getProjectID = projectID
	f.getUserID = userID
	if f.getErr != nil {
		return domain.ProjectAccess{}, f.getErr
	}
	return f.getAccess, nil
}

func (f *fakeProjectStore) CreateProject(ctx context.Context, input domain.CreateProjectInput) (domain.Project, error) {
	f.createCalled = true
	f.createInput = input
	if f.createErr != nil {
		return domain.Project{}, f.createErr
	}
	return f.createResult, nil
}

func (f *fakeProjectStore) UpdateProject(ctx context.Context, input domain.UpdateProjectInput) (domain.Project, error) {
	f.updateCalled = true
	f.updateInput = input
	if f.updateErr != nil {
		return domain.Project{}, f.updateErr
	}
	return f.updateResult, nil
}

func (f *fakeProjectStore) ArchiveProject(ctx context.Context, ownerUserID uuid.UUID, id uuid.UUID, updatedAt time.Time) (domain.Project, error) {
	f.archiveCalled = true
	f.archiveOwnerID = ownerUserID
	f.archiveID = id
	f.archiveUpdatedAt = updatedAt
	if f.archiveErr != nil {
		return domain.Project{}, f.archiveErr
	}
	return f.archiveResult, nil
}

func (f *fakeProjectStore) AddProjectMember(ctx context.Context, input domain.AddProjectMemberInput) (domain.ProjectMember, error) {
	f.addMemberCalled = true
	f.addMemberInput = input
	if f.addMemberErr != nil {
		return domain.ProjectMember{}, f.addMemberErr
	}
	return f.addMemberResult, nil
}

func (f *fakeProjectStore) ListProjectMembers(ctx context.Context, projectID uuid.UUID) ([]domain.ProjectMemberDetail, error) {
	return f.listMembersResult, f.listMembersErr
}

func (f *fakeProjectStore) GetProjectMember(ctx context.Context, projectID uuid.UUID, userID uuid.UUID) (domain.ProjectMember, error) {
	return domain.ProjectMember{}, domain.ErrProjectMemberNotFound
}

func (f *fakeProjectStore) UpdateProjectMemberRole(ctx context.Context, input domain.UpdateProjectMemberRoleInput) (domain.ProjectMember, error) {
	f.updateMemberInput = input
	if f.updateMemberErr != nil {
		return domain.ProjectMember{}, f.updateMemberErr
	}
	return f.updateMemberResult, nil
}

func (f *fakeProjectStore) RemoveProjectMember(ctx context.Context, projectID uuid.UUID, userID uuid.UUID) error {
	f.removeMemberCalled = true
	f.removeMemberProject = projectID
	f.removeMemberUser = userID
	return f.removeMemberErr
}

func (f *fakeProjectStore) FindUserByEmail(ctx context.Context, email string) (uuid.UUID, error) {
	f.findEmailInput = email
	if f.findEmailErr != nil {
		return uuid.Nil, f.findEmailErr
	}
	return f.findEmailResult, nil
}
