package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/roles/domain"
	"github.com/aklmans/wow-dashboard-api/internal/roles/service"
)

func strptr(s string) *string { return &s }

// --- ListRoles / GetRole ----------------------------------------------------

func TestServiceListRolesPassesThrough(t *testing.T) {
	store := &fakeRoleStore{listResult: []domain.Role{{Name: "admin"}, {Name: "auditor"}}}
	svc := service.NewService(store)

	roles, err := svc.ListRoles(context.Background())
	if err != nil {
		t.Fatalf("ListRoles returned error: %v", err)
	}
	if len(roles) != 2 {
		t.Fatalf("len(roles) = %d, want 2", len(roles))
	}
}

func TestServiceGetRoleRejectsInvalidID(t *testing.T) {
	svc := service.NewService(&fakeRoleStore{})
	if _, err := svc.GetRole(context.Background(), "not-a-uuid"); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("GetRole err = %v, want ErrInvalidInput", err)
	}
}

func TestServiceGetRoleMapsNotFound(t *testing.T) {
	svc := service.NewService(&fakeRoleStore{getErr: domain.ErrRoleNotFound})
	if _, err := svc.GetRole(context.Background(), uuid.New().String()); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("GetRole err = %v, want ErrNotFound", err)
	}
}

// --- CreateRole -------------------------------------------------------------

func TestServiceCreateRoleNormalizesAndAudits(t *testing.T) {
	store := &fakeRoleStore{createResult: domain.Role{ID: uuid.New(), Name: "auditor", Permissions: []string{"system_events:read"}}}
	audit := &fakeRoleAuditRecorder{}
	svc := service.NewService(store, service.WithAuditRecorder(audit))

	// "system_events:read" is repeated to confirm de-duplication.
	if _, err := svc.CreateRole(context.Background(), service.CreateRoleInput{
		ActorUserID: uuid.New().String(),
		Name:        "  auditor  ",
		Permissions: []string{"system_events:read", "system_events:read"},
	}); err != nil {
		t.Fatalf("CreateRole returned error: %v", err)
	}
	if store.createInput.Name != "auditor" {
		t.Fatalf("stored name = %q, want trimmed auditor", store.createInput.Name)
	}
	if len(store.createInput.Permissions) != 1 {
		t.Fatalf("stored permissions = %v, want de-duplicated single entry", store.createInput.Permissions)
	}
	if len(audit.events) != 1 || audit.events[0].EventType != service.EventRoleCreated {
		t.Fatalf("audit = %#v, want one roles.role.created", audit.events)
	}
}

func TestServiceCreateRoleRejectsBadInput(t *testing.T) {
	tests := []struct {
		name  string
		input service.CreateRoleInput
	}{
		{"empty name", service.CreateRoleInput{Name: "   ", Permissions: []string{"users:read"}}},
		{"unknown permission", service.CreateRoleInput{Name: "x", Permissions: []string{"orders:read"}}},
		{"wildcard permission", service.CreateRoleInput{Name: "x", Permissions: []string{"*"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeRoleStore{}
			svc := service.NewService(store)
			if _, err := svc.CreateRole(context.Background(), tt.input); !errors.Is(err, service.ErrInvalidInput) {
				t.Fatalf("CreateRole err = %v, want ErrInvalidInput", err)
			}
			if store.createCalled {
				t.Fatal("store.CreateRole was called for invalid input")
			}
		})
	}
}

func TestServiceCreateRoleMapsNameConflict(t *testing.T) {
	store := &fakeRoleStore{createErr: domain.ErrNameConflict}
	svc := service.NewService(store)
	if _, err := svc.CreateRole(context.Background(), service.CreateRoleInput{
		Name: "admin", Permissions: []string{"users:read"},
	}); !errors.Is(err, service.ErrNameConflict) {
		t.Fatalf("CreateRole err = %v, want ErrNameConflict", err)
	}
}

// --- UpdateRole -------------------------------------------------------------

func TestServiceUpdateRoleReplacesPermissions(t *testing.T) {
	roleID := uuid.New()
	store := &fakeRoleStore{
		getResult:    domain.Role{ID: roleID, Name: "auditor", IsSystem: false},
		updateResult: domain.Role{ID: roleID, Name: "auditor"},
	}
	audit := &fakeRoleAuditRecorder{}
	svc := service.NewService(store, service.WithAuditRecorder(audit))

	if _, err := svc.UpdateRole(context.Background(), service.UpdateRoleInput{
		RoleID:      roleID.String(),
		Permissions: &[]string{"users:read", "roles:read"},
	}); err != nil {
		t.Fatalf("UpdateRole returned error: %v", err)
	}
	if store.updateInput.Permissions == nil || len(*store.updateInput.Permissions) != 2 {
		t.Fatalf("update permissions = %v, want two entries", store.updateInput.Permissions)
	}
	if len(audit.events) != 1 || audit.events[0].EventType != service.EventRoleUpdated {
		t.Fatalf("audit = %#v, want one roles.role.updated", audit.events)
	}
}

func TestServiceUpdateRoleRejectsSystemRole(t *testing.T) {
	roleID := uuid.New()
	store := &fakeRoleStore{getResult: domain.Role{ID: roleID, Name: "admin", IsSystem: true}}
	svc := service.NewService(store)

	if _, err := svc.UpdateRole(context.Background(), service.UpdateRoleInput{
		RoleID: roleID.String(), Name: strptr("superadmin"),
	}); !errors.Is(err, service.ErrSystemRole) {
		t.Fatalf("UpdateRole err = %v, want ErrSystemRole", err)
	}
	if store.updateCalled {
		t.Fatal("store.UpdateRole was called for a system role")
	}
}

func TestServiceUpdateRoleRejectsEmptyPatch(t *testing.T) {
	roleID := uuid.New()
	store := &fakeRoleStore{getResult: domain.Role{ID: roleID}}
	svc := service.NewService(store)

	if _, err := svc.UpdateRole(context.Background(), service.UpdateRoleInput{
		RoleID: roleID.String(),
	}); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("UpdateRole err = %v, want ErrInvalidInput", err)
	}
}

func TestServiceUpdateRoleMapsNotFound(t *testing.T) {
	store := &fakeRoleStore{getErr: domain.ErrRoleNotFound}
	svc := service.NewService(store)
	if _, err := svc.UpdateRole(context.Background(), service.UpdateRoleInput{
		RoleID: uuid.New().String(), Name: strptr("x"),
	}); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("UpdateRole err = %v, want ErrNotFound", err)
	}
}

// --- DeleteRole -------------------------------------------------------------

func TestServiceDeleteRoleSucceeds(t *testing.T) {
	roleID := uuid.New()
	store := &fakeRoleStore{getResult: domain.Role{ID: roleID, IsSystem: false, UserCount: 0}}
	audit := &fakeRoleAuditRecorder{}
	svc := service.NewService(store, service.WithAuditRecorder(audit))

	if err := svc.DeleteRole(context.Background(), uuid.New().String(), roleID.String()); err != nil {
		t.Fatalf("DeleteRole returned error: %v", err)
	}
	if !store.deleteCalled || store.deleteID != roleID {
		t.Fatalf("store.DeleteRole not called with %s", roleID)
	}
	if len(audit.events) != 1 || audit.events[0].EventType != service.EventRoleDeleted {
		t.Fatalf("audit = %#v, want one roles.role.deleted", audit.events)
	}
}

func TestServiceDeleteRoleRejectsSystemAndInUse(t *testing.T) {
	t.Run("system role", func(t *testing.T) {
		roleID := uuid.New()
		store := &fakeRoleStore{getResult: domain.Role{ID: roleID, IsSystem: true}}
		svc := service.NewService(store)
		if err := svc.DeleteRole(context.Background(), uuid.New().String(), roleID.String()); !errors.Is(err, service.ErrSystemRole) {
			t.Fatalf("DeleteRole err = %v, want ErrSystemRole", err)
		}
		if store.deleteCalled {
			t.Fatal("store.DeleteRole was called for a system role")
		}
	})

	t.Run("role in use", func(t *testing.T) {
		roleID := uuid.New()
		store := &fakeRoleStore{getResult: domain.Role{ID: roleID, IsSystem: false, UserCount: 2}}
		svc := service.NewService(store)
		if err := svc.DeleteRole(context.Background(), uuid.New().String(), roleID.String()); !errors.Is(err, service.ErrRoleInUse) {
			t.Fatalf("DeleteRole err = %v, want ErrRoleInUse", err)
		}
		if store.deleteCalled {
			t.Fatal("store.DeleteRole was called for a role still in use")
		}
	})
}

// --- fakes ------------------------------------------------------------------

type fakeRoleAuditRecorder struct {
	events []service.AuditEvent
}

func (f *fakeRoleAuditRecorder) RecordRoleEvent(ctx context.Context, event service.AuditEvent) error {
	f.events = append(f.events, event)
	return nil
}

type fakeRoleStore struct {
	listResult []domain.Role
	listErr    error

	getResult domain.Role
	getErr    error

	createCalled bool
	createInput  domain.CreateRoleInput
	createResult domain.Role
	createErr    error

	updateCalled bool
	updateInput  domain.UpdateRoleInput
	updateResult domain.Role
	updateErr    error

	deleteCalled bool
	deleteID     uuid.UUID
	deleteErr    error
}

func (f *fakeRoleStore) ListRoles(ctx context.Context) ([]domain.Role, error) {
	return f.listResult, f.listErr
}

func (f *fakeRoleStore) GetRoleByID(ctx context.Context, id uuid.UUID) (domain.Role, error) {
	if f.getErr != nil {
		return domain.Role{}, f.getErr
	}
	return f.getResult, nil
}

func (f *fakeRoleStore) CreateRole(ctx context.Context, input domain.CreateRoleInput) (domain.Role, error) {
	f.createCalled = true
	f.createInput = input
	if f.createErr != nil {
		return domain.Role{}, f.createErr
	}
	return f.createResult, nil
}

func (f *fakeRoleStore) UpdateRole(ctx context.Context, input domain.UpdateRoleInput) (domain.Role, error) {
	f.updateCalled = true
	f.updateInput = input
	if f.updateErr != nil {
		return domain.Role{}, f.updateErr
	}
	return f.updateResult, nil
}

func (f *fakeRoleStore) DeleteRole(ctx context.Context, id uuid.UUID) error {
	f.deleteCalled = true
	f.deleteID = id
	return f.deleteErr
}
