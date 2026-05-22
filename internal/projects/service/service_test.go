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

func TestServiceListProjectsNormalizesInput(t *testing.T) {
	store := &fakeProjectStore{}
	svc := service.NewService(store)

	owner := uuid.New()
	_, err := svc.ListProjects(context.Background(), service.ListProjectsInput{
		OwnerUserID: "  " + owner.String() + "  ",
		Search:      "  Demo  ",
		Status:      "ACTIVE",
	})
	if err != nil {
		t.Fatalf("ListProjects returned error: %v", err)
	}

	if store.listInput.OwnerUserID != owner {
		t.Fatalf("owner = %s, want %s", store.listInput.OwnerUserID, owner)
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
	owner := uuid.New().String()
	tests := []struct {
		name  string
		input service.ListProjectsInput
	}{
		{name: "invalid owner", input: service.ListProjectsInput{OwnerUserID: "not-a-uuid"}},
		{name: "missing owner", input: service.ListProjectsInput{OwnerUserID: ""}},
		{name: "negative page", input: service.ListProjectsInput{OwnerUserID: owner, Page: -1}},
		{name: "too large page size", input: service.ListProjectsInput{OwnerUserID: owner, PageSize: 101}},
		{name: "invalid status", input: service.ListProjectsInput{OwnerUserID: owner, Status: "pending"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeProjectStore{}
			svc := service.NewService(store)

			_, err := svc.ListProjects(context.Background(), tt.input)
			if !errors.Is(err, service.ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
			if store.listCalled {
				t.Fatal("store.ListProjects was called for invalid input")
			}
		})
	}
}

func TestServiceGetProjectPassesParsedID(t *testing.T) {
	owner := uuid.New()
	want := uuid.New()
	store := &fakeProjectStore{getResult: domain.Project{ID: want, OwnerUserID: owner, Name: "demo"}}
	svc := service.NewService(store)

	got, err := svc.GetProject(context.Background(), owner.String(), "  "+want.String()+"  ")
	if err != nil {
		t.Fatalf("GetProject error: %v", err)
	}
	if !store.getCalled {
		t.Fatal("store.GetProjectByID was not called")
	}
	if store.getOwnerID != owner || store.getID != want {
		t.Fatalf("store args = owner %s id %s, want %s/%s", store.getOwnerID, store.getID, owner, want)
	}
	if got.ID != want {
		t.Fatalf("returned id = %s, want %s", got.ID, want)
	}
}

func TestServiceGetProjectRejectsInvalidIDs(t *testing.T) {
	owner := uuid.New().String()
	cases := []struct {
		name    string
		owner   string
		project string
	}{
		{name: "invalid owner", owner: "not-a-uuid", project: uuid.New().String()},
		{name: "invalid project id", owner: owner, project: "not-a-uuid"},
		{name: "empty project id", owner: owner, project: "   "},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeProjectStore{}
			svc := service.NewService(store)

			_, err := svc.GetProject(context.Background(), tt.owner, tt.project)
			if !errors.Is(err, service.ErrInvalidInput) {
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

	_, err := svc.GetProject(context.Background(), uuid.New().String(), uuid.New().String())
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("err = %v, want service.ErrNotFound", err)
	}
}

func TestServiceCreateProjectNormalizesInput(t *testing.T) {
	owner := uuid.New()
	fixedNow := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	store := &fakeProjectStore{}
	svc := service.NewService(store, service.WithClock(func() time.Time { return fixedNow }))

	got, err := svc.CreateProject(context.Background(), service.CreateProjectInput{
		OwnerUserID: owner.String(),
		Name:        "  Demo Project  ",
		Description: "  hello  ",
		Status:      "",
	})
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}

	if !store.createCalled {
		t.Fatal("store.CreateProject was not called")
	}
	if store.createInput.Name != "Demo Project" {
		t.Fatalf("name = %q, want trimmed Demo Project", store.createInput.Name)
	}
	if store.createInput.Description != "hello" {
		t.Fatalf("description = %q, want trimmed hello", store.createInput.Description)
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
	if got.Name != "" {
		// store returns zero Project from fake; just sanity-check no panic
	}
}

func TestServiceCreateProjectRejectsInvalidInput(t *testing.T) {
	owner := uuid.New().String()
	tests := []struct {
		name  string
		input service.CreateProjectInput
	}{
		{name: "invalid owner", input: service.CreateProjectInput{OwnerUserID: "bad", Name: "x"}},
		{name: "empty name", input: service.CreateProjectInput{OwnerUserID: owner, Name: "   "}},
		{name: "name too long", input: service.CreateProjectInput{OwnerUserID: owner, Name: longString(121)}},
		{name: "description too long", input: service.CreateProjectInput{OwnerUserID: owner, Name: "ok", Description: longString(2001)}},
		{name: "invalid status", input: service.CreateProjectInput{OwnerUserID: owner, Name: "ok", Status: "deleted"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeProjectStore{}
			svc := service.NewService(store)

			_, err := svc.CreateProject(context.Background(), tt.input)
			if !errors.Is(err, service.ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
			if store.createCalled {
				t.Fatalf("store was called for invalid input %s", tt.name)
			}
		})
	}
}

func TestServiceCreateProjectMapsNameConflictWithoutAudit(t *testing.T) {
	owner := uuid.New()
	recorder := &fakeAuditRecorder{}
	store := &fakeProjectStore{createErr: domain.ErrProjectNameAlreadyExists}
	svc := service.NewService(store, service.WithAuditRecorder(recorder))

	_, err := svc.CreateProject(context.Background(), service.CreateProjectInput{
		OwnerUserID: owner.String(),
		Name:        "Demo",
	})
	if !errors.Is(err, service.ErrNameConflict) {
		t.Fatalf("err = %v, want ErrNameConflict", err)
	}
	if len(recorder.calls) != 0 {
		t.Fatalf("audit calls = %d, want 0 for name conflict", len(recorder.calls))
	}
}

func longString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}

func TestServiceUpdateProjectNormalizesAndForwards(t *testing.T) {
	owner := uuid.New()
	project := uuid.New()
	fixedNow := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	store := &fakeProjectStore{updateResult: domain.Project{
		ID: project, OwnerUserID: owner, Name: "New",
	}}
	svc := service.NewService(store, service.WithClock(func() time.Time { return fixedNow }))

	name := "  New Name  "
	description := "  hello  "
	status := "ARCHIVED"
	got, err := svc.UpdateProject(context.Background(), service.UpdateProjectInput{
		OwnerUserID: owner.String(),
		ID:          project.String(),
		Name:        &name,
		Description: &description,
		Status:      &status,
	})
	if err != nil {
		t.Fatalf("UpdateProject error: %v", err)
	}
	if !store.updateCalled {
		t.Fatal("store.UpdateProject was not called")
	}
	if store.updateInput.OwnerUserID != owner || store.updateInput.ID != project {
		t.Fatalf("ids forwarded = owner=%s id=%s, want %s/%s", store.updateInput.OwnerUserID, store.updateInput.ID, owner, project)
	}
	if store.updateInput.Name == nil || *store.updateInput.Name != "New Name" {
		t.Fatalf("name forwarded = %v, want trimmed New Name", store.updateInput.Name)
	}
	if store.updateInput.Description == nil || *store.updateInput.Description != "hello" {
		t.Fatalf("description forwarded = %v, want trimmed hello", store.updateInput.Description)
	}
	if store.updateInput.Status == nil || *store.updateInput.Status != domain.ProjectStatusArchived {
		t.Fatalf("status forwarded = %v, want archived", store.updateInput.Status)
	}
	if !store.updateInput.UpdatedAt.Equal(fixedNow) {
		t.Fatalf("updatedAt = %v, want %v", store.updateInput.UpdatedAt, fixedNow)
	}
	if got.ID != project {
		t.Fatalf("returned id = %s, want %s", got.ID, project)
	}
}

func TestServiceUpdateProjectAllowsEmptyDescription(t *testing.T) {
	store := &fakeProjectStore{updateResult: domain.Project{}}
	svc := service.NewService(store)

	empty := ""
	_, err := svc.UpdateProject(context.Background(), service.UpdateProjectInput{
		OwnerUserID: uuid.New().String(),
		ID:          uuid.New().String(),
		Description: &empty,
	})
	if err != nil {
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
	owner := uuid.New().String()
	id := uuid.New().String()
	emptyStr := ""
	tooLongName := longString(121)
	tooLongDesc := longString(2001)
	emptyName := "   "
	badStatus := "deleted"
	okName := "ok"

	tests := []struct {
		name  string
		input service.UpdateProjectInput
	}{
		{name: "invalid owner", input: service.UpdateProjectInput{OwnerUserID: "bad", ID: id, Name: &okName}},
		{name: "invalid id", input: service.UpdateProjectInput{OwnerUserID: owner, ID: "bad", Name: &okName}},
		{name: "empty body", input: service.UpdateProjectInput{OwnerUserID: owner, ID: id}},
		{name: "empty name pointer", input: service.UpdateProjectInput{OwnerUserID: owner, ID: id, Name: &emptyStr}},
		{name: "whitespace name", input: service.UpdateProjectInput{OwnerUserID: owner, ID: id, Name: &emptyName}},
		{name: "name too long", input: service.UpdateProjectInput{OwnerUserID: owner, ID: id, Name: &tooLongName}},
		{name: "description too long", input: service.UpdateProjectInput{OwnerUserID: owner, ID: id, Description: &tooLongDesc}},
		{name: "invalid status", input: service.UpdateProjectInput{OwnerUserID: owner, ID: id, Status: &badStatus}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeProjectStore{}
			svc := service.NewService(store)

			_, err := svc.UpdateProject(context.Background(), tt.input)
			if !errors.Is(err, service.ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
			if store.updateCalled {
				t.Fatalf("store called for invalid input %s", tt.name)
			}
		})
	}
}

func TestServiceUpdateProjectMapsNotFound(t *testing.T) {
	store := &fakeProjectStore{updateErr: domain.ErrProjectNotFound}
	svc := service.NewService(store)

	name := "x"
	_, err := svc.UpdateProject(context.Background(), service.UpdateProjectInput{
		OwnerUserID: uuid.New().String(),
		ID:          uuid.New().String(),
		Name:        &name,
	})
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestServiceUpdateProjectMapsNameConflictWithoutAudit(t *testing.T) {
	owner := uuid.New()
	recorder := &fakeAuditRecorder{}
	store := &fakeProjectStore{updateErr: domain.ErrProjectNameAlreadyExists}
	svc := service.NewService(store, service.WithAuditRecorder(recorder))

	name := "Demo"
	_, err := svc.UpdateProject(context.Background(), service.UpdateProjectInput{
		OwnerUserID: owner.String(),
		ID:          uuid.New().String(),
		Name:        &name,
	})
	if !errors.Is(err, service.ErrNameConflict) {
		t.Fatalf("err = %v, want ErrNameConflict", err)
	}
	if len(recorder.calls) != 0 {
		t.Fatalf("audit calls = %d, want 0 for name conflict", len(recorder.calls))
	}
}

func TestServiceArchiveProjectSuccess(t *testing.T) {
	owner := uuid.New()
	project := uuid.New()
	fixedNow := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	store := &fakeProjectStore{archiveResult: domain.Project{
		ID: project, OwnerUserID: owner, Status: domain.ProjectStatusArchived,
	}}
	svc := service.NewService(store, service.WithClock(func() time.Time { return fixedNow }))

	got, err := svc.ArchiveProject(context.Background(), "  "+owner.String()+"  ", "  "+project.String()+"  ")
	if err != nil {
		t.Fatalf("ArchiveProject error: %v", err)
	}
	if !store.archiveCalled {
		t.Fatal("store.ArchiveProject was not called")
	}
	if store.archiveOwnerID != owner || store.archiveID != project {
		t.Fatalf("ids forwarded = owner=%s id=%s, want %s/%s", store.archiveOwnerID, store.archiveID, owner, project)
	}
	if !store.archiveUpdatedAt.Equal(fixedNow) {
		t.Fatalf("updatedAt = %v, want %v", store.archiveUpdatedAt, fixedNow)
	}
	if got.Status != domain.ProjectStatusArchived {
		t.Fatalf("returned status = %q, want archived", got.Status)
	}
}

func TestServiceArchiveProjectRejectsInvalidIDs(t *testing.T) {
	owner := uuid.New().String()
	cases := []struct {
		name    string
		owner   string
		project string
	}{
		{name: "invalid owner", owner: "bad", project: uuid.New().String()},
		{name: "empty owner", owner: "", project: uuid.New().String()},
		{name: "invalid project id", owner: owner, project: "bad"},
		{name: "empty project id", owner: owner, project: "   "},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeProjectStore{}
			svc := service.NewService(store)

			_, err := svc.ArchiveProject(context.Background(), tt.owner, tt.project)
			if !errors.Is(err, service.ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
			if store.archiveCalled {
				t.Fatalf("store called for invalid input %s", tt.name)
			}
		})
	}
}

func TestServiceArchiveProjectMapsNotFound(t *testing.T) {
	store := &fakeProjectStore{archiveErr: domain.ErrProjectNotFound}
	svc := service.NewService(store)

	_, err := svc.ArchiveProject(context.Background(), uuid.New().String(), uuid.New().String())
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

type fakeProjectStore struct {
	listCalled bool
	listInput  domain.ListProjectsInput
	listResult domain.ListProjectsResult
	listErr    error

	getCalled  bool
	getOwnerID uuid.UUID
	getID      uuid.UUID
	getResult  domain.Project
	getErr     error

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
}

func (f *fakeProjectStore) ListProjects(ctx context.Context, input domain.ListProjectsInput) (domain.ListProjectsResult, error) {
	f.listCalled = true
	f.listInput = input
	if f.listErr != nil {
		return domain.ListProjectsResult{}, f.listErr
	}
	return f.listResult, nil
}

func (f *fakeProjectStore) GetProjectByID(ctx context.Context, ownerUserID uuid.UUID, id uuid.UUID) (domain.Project, error) {
	f.getCalled = true
	f.getOwnerID = ownerUserID
	f.getID = id
	if f.getErr != nil {
		return domain.Project{}, f.getErr
	}
	return f.getResult, nil
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

type fakeAuditRecorder struct {
	calls []service.AuditEvent
	err   error
}

func (f *fakeAuditRecorder) RecordProjectEvent(ctx context.Context, event service.AuditEvent) error {
	f.calls = append(f.calls, event)
	return f.err
}

func TestServiceCreateProjectRecordsAudit(t *testing.T) {
	owner := uuid.New()
	created := domain.Project{
		ID:          uuid.New(),
		OwnerUserID: owner,
		Status:      domain.ProjectStatusActive,
	}
	recorder := &fakeAuditRecorder{}
	store := &fakeProjectStore{createResult: created}
	svc := service.NewService(store, service.WithAuditRecorder(recorder))

	got, err := svc.CreateProject(context.Background(), service.CreateProjectInput{
		OwnerUserID: owner.String(),
		Name:        "Demo",
	})
	if err != nil {
		t.Fatalf("CreateProject error: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("returned id mismatch")
	}
	if len(recorder.calls) != 1 {
		t.Fatalf("audit calls = %d, want 1", len(recorder.calls))
	}
	event := recorder.calls[0]
	if event.EventType != service.EventProjectCreated {
		t.Fatalf("event_type = %q, want %q", event.EventType, service.EventProjectCreated)
	}
	if event.Metadata.ProjectID != created.ID.String() ||
		event.Metadata.OwnerUserID != owner.String() ||
		event.Metadata.Status != string(domain.ProjectStatusActive) {
		t.Fatalf("metadata = %#v", event.Metadata)
	}
}

func TestServiceUpdateProjectRecordsAuditWithChangedFields(t *testing.T) {
	owner := uuid.New()
	id := uuid.New()
	updated := domain.Project{
		ID:          id,
		OwnerUserID: owner,
		Status:      domain.ProjectStatusArchived,
	}
	recorder := &fakeAuditRecorder{}
	store := &fakeProjectStore{updateResult: updated}
	svc := service.NewService(store, service.WithAuditRecorder(recorder))

	name := "New Name"
	desc := "  hello  "
	status := "archived"
	_, err := svc.UpdateProject(context.Background(), service.UpdateProjectInput{
		OwnerUserID: owner.String(),
		ID:          id.String(),
		Name:        &name,
		Description: &desc,
		Status:      &status,
	})
	if err != nil {
		t.Fatalf("UpdateProject error: %v", err)
	}
	if len(recorder.calls) != 1 {
		t.Fatalf("audit calls = %d, want 1", len(recorder.calls))
	}
	event := recorder.calls[0]
	if event.EventType != service.EventProjectUpdated {
		t.Fatalf("event_type = %q", event.EventType)
	}
	if event.Metadata.ProjectID != id.String() {
		t.Fatalf("project_id = %q", event.Metadata.ProjectID)
	}
	if event.Metadata.OwnerUserID != owner.String() {
		t.Fatalf("owner_user_id = %q", event.Metadata.OwnerUserID)
	}
	if event.Metadata.Status != "archived" {
		t.Fatalf("status = %q", event.Metadata.Status)
	}
	wantFields := []string{"name", "description", "status"}
	if len(event.Metadata.ChangedFields) != len(wantFields) {
		t.Fatalf("changed_fields = %v, want %v", event.Metadata.ChangedFields, wantFields)
	}
	for i, want := range wantFields {
		if event.Metadata.ChangedFields[i] != want {
			t.Fatalf("changed_fields[%d] = %q, want %q", i, event.Metadata.ChangedFields[i], want)
		}
	}
}

func TestServiceArchiveProjectRecordsAudit(t *testing.T) {
	owner := uuid.New()
	id := uuid.New()
	archived := domain.Project{
		ID:          id,
		OwnerUserID: owner,
		Status:      domain.ProjectStatusArchived,
	}
	recorder := &fakeAuditRecorder{}
	store := &fakeProjectStore{archiveResult: archived}
	svc := service.NewService(store, service.WithAuditRecorder(recorder))

	_, err := svc.ArchiveProject(context.Background(), owner.String(), id.String())
	if err != nil {
		t.Fatalf("ArchiveProject error: %v", err)
	}
	if len(recorder.calls) != 1 {
		t.Fatalf("audit calls = %d, want 1", len(recorder.calls))
	}
	event := recorder.calls[0]
	if event.EventType != service.EventProjectArchived {
		t.Fatalf("event_type = %q", event.EventType)
	}
	if event.Metadata.ProjectID != id.String() ||
		event.Metadata.OwnerUserID != owner.String() ||
		event.Metadata.Status != string(domain.ProjectStatusArchived) {
		t.Fatalf("metadata = %#v", event.Metadata)
	}
}

func TestServiceWriteOperationsTolerateAuditFailures(t *testing.T) {
	owner := uuid.New()
	id := uuid.New()
	recorder := &fakeAuditRecorder{err: errors.New("audit boom")}

	t.Run("create still returns project", func(t *testing.T) {
		store := &fakeProjectStore{createResult: domain.Project{ID: id, OwnerUserID: owner}}
		svc := service.NewService(store, service.WithAuditRecorder(recorder))
		got, err := svc.CreateProject(context.Background(), service.CreateProjectInput{
			OwnerUserID: owner.String(),
			Name:        "Demo",
		})
		if err != nil {
			t.Fatalf("CreateProject error: %v", err)
		}
		if got.ID != id {
			t.Fatalf("project id mismatch")
		}
	})

	t.Run("update still returns project", func(t *testing.T) {
		store := &fakeProjectStore{updateResult: domain.Project{ID: id, OwnerUserID: owner}}
		svc := service.NewService(store, service.WithAuditRecorder(recorder))
		name := "x"
		got, err := svc.UpdateProject(context.Background(), service.UpdateProjectInput{
			OwnerUserID: owner.String(),
			ID:          id.String(),
			Name:        &name,
		})
		if err != nil {
			t.Fatalf("UpdateProject error: %v", err)
		}
		if got.ID != id {
			t.Fatalf("project id mismatch")
		}
	})

	t.Run("archive still returns project", func(t *testing.T) {
		store := &fakeProjectStore{archiveResult: domain.Project{ID: id, OwnerUserID: owner, Status: domain.ProjectStatusArchived}}
		svc := service.NewService(store, service.WithAuditRecorder(recorder))
		got, err := svc.ArchiveProject(context.Background(), owner.String(), id.String())
		if err != nil {
			t.Fatalf("ArchiveProject error: %v", err)
		}
		if got.ID != id || got.Status != domain.ProjectStatusArchived {
			t.Fatalf("project mismatch: %#v", got)
		}
	})
}

func TestServiceAuditMetadataDoesNotLeakBusinessText(t *testing.T) {
	owner := uuid.New()
	id := uuid.New()
	recorder := &fakeAuditRecorder{}

	createStore := &fakeProjectStore{createResult: domain.Project{ID: id, OwnerUserID: owner, Status: domain.ProjectStatusActive}}
	createSvc := service.NewService(createStore, service.WithAuditRecorder(recorder))
	if _, err := createSvc.CreateProject(context.Background(), service.CreateProjectInput{
		OwnerUserID: owner.String(),
		Name:        "Top Secret Name",
		Description: "leaky-token-ABC123 password=hunter2",
	}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	updateStore := &fakeProjectStore{updateResult: domain.Project{ID: id, OwnerUserID: owner, Status: domain.ProjectStatusActive}}
	updateSvc := service.NewService(updateStore, service.WithAuditRecorder(recorder))
	name := "Another Secret"
	desc := "Bearer eyJsecret token"
	if _, err := updateSvc.UpdateProject(context.Background(), service.UpdateProjectInput{
		OwnerUserID: owner.String(),
		ID:          id.String(),
		Name:        &name,
		Description: &desc,
	}); err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}

	if len(recorder.calls) == 0 {
		t.Fatal("no audit calls captured")
	}

	for _, event := range recorder.calls {
		raw, err := json.Marshal(event.Metadata)
		if err != nil {
			t.Fatalf("marshal metadata: %v", err)
		}
		body := strings.ToLower(string(raw))
		for _, forbidden := range []string{
			"top secret name",
			"another secret",
			"hunter2",
			"leaky-token",
			"bearer ey",
			"password",
			"\"name\":\"",
			"\"description\":\"",
		} {
			if strings.Contains(body, strings.ToLower(forbidden)) {
				t.Fatalf("metadata leaks %q: %s", forbidden, raw)
			}
		}
	}
}
