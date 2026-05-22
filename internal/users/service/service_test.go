package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/users/domain"
	"github.com/aklmans/wow-dashboard-api/internal/users/service"
)

func TestServiceListUsersDefaultsAndNormalizesInput(t *testing.T) {
	store := &fakeUserStore{}
	svc := service.NewService(store)

	_, err := svc.ListUsers(context.Background(), service.ListUsersInput{
		Search: "  Demo  ",
		Role:   "ADMIN",
		Status: "ACTIVE",
	})
	if err != nil {
		t.Fatalf("ListUsers returned error: %v", err)
	}

	if store.input.Page != 1 {
		t.Fatalf("page = %d, want 1", store.input.Page)
	}
	if store.input.PageSize != 20 {
		t.Fatalf("pageSize = %d, want 20", store.input.PageSize)
	}
	if store.input.Search != "Demo" {
		t.Fatalf("search = %q, want trimmed Demo", store.input.Search)
	}
	if store.input.Role != domain.UserRoleAdmin {
		t.Fatalf("role = %q, want admin", store.input.Role)
	}
	if store.input.Status != domain.UserStatusActive {
		t.Fatalf("status = %q, want active", store.input.Status)
	}
}

func TestServiceListUsersAcceptsPaginationBoundary(t *testing.T) {
	store := &fakeUserStore{}
	svc := service.NewService(store)

	_, err := svc.ListUsers(context.Background(), service.ListUsersInput{
		Page:     2,
		PageSize: 100,
	})
	if err != nil {
		t.Fatalf("ListUsers returned error: %v", err)
	}

	if store.input.Page != 2 {
		t.Fatalf("page = %d, want 2", store.input.Page)
	}
	if store.input.PageSize != 100 {
		t.Fatalf("pageSize = %d, want 100", store.input.PageSize)
	}
	if store.input.Offset != 100 {
		t.Fatalf("offset = %d, want 100", store.input.Offset)
	}
}

func TestServiceListUsersDefaultsPageSizeWhenPageIsProvided(t *testing.T) {
	store := &fakeUserStore{}
	svc := service.NewService(store)

	_, err := svc.ListUsers(context.Background(), service.ListUsersInput{
		Page: 2,
	})
	if err != nil {
		t.Fatalf("ListUsers returned error: %v", err)
	}

	if store.input.Page != 2 {
		t.Fatalf("page = %d, want 2", store.input.Page)
	}
	if store.input.PageSize != 20 {
		t.Fatalf("pageSize = %d, want 20", store.input.PageSize)
	}
}

func TestServiceListUsersRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input service.ListUsersInput
	}{
		{
			name:  "negative page",
			input: service.ListUsersInput{Page: -1},
		},
		{
			name:  "negative page size",
			input: service.ListUsersInput{Page: 1, PageSize: -1},
		},
		{
			name:  "too large page size",
			input: service.ListUsersInput{Page: 1, PageSize: 101},
		},
		{
			name:  "invalid role",
			input: service.ListUsersInput{Role: "owner"},
		},
		{
			name:  "invalid status",
			input: service.ListUsersInput{Status: "pending"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeUserStore{}
			svc := service.NewService(store)

			_, err := svc.ListUsers(context.Background(), tt.input)
			if !errors.Is(err, service.ErrInvalidInput) {
				t.Fatalf("ListUsers error = %v, want ErrInvalidInput", err)
			}
			if store.called {
				t.Fatal("store was called for invalid input")
			}
		})
	}
}

func TestServiceGetUserPassesParsedID(t *testing.T) {
	want := uuid.New()
	store := &fakeUserStore{getResult: domain.User{ID: want, Email: "demo@example.com"}}
	svc := service.NewService(store)

	got, err := svc.GetUser(context.Background(), "  "+want.String()+"  ")
	if err != nil {
		t.Fatalf("GetUser returned error: %v", err)
	}
	if !store.getCalled {
		t.Fatal("store.GetUserByID was not called")
	}
	if store.getID != want {
		t.Fatalf("store.GetUserByID id = %s, want %s", store.getID, want)
	}
	if got.ID != want {
		t.Fatalf("returned user id = %s, want %s", got.ID, want)
	}
}

func TestServiceGetUserRejectsInvalidID(t *testing.T) {
	cases := []string{"", "   ", "not-a-uuid"}
	for _, in := range cases {
		store := &fakeUserStore{}
		svc := service.NewService(store)

		_, err := svc.GetUser(context.Background(), in)
		if !errors.Is(err, service.ErrInvalidInput) {
			t.Fatalf("GetUser(%q) err = %v, want ErrInvalidInput", in, err)
		}
		if store.getCalled {
			t.Fatalf("GetUser(%q) called store for invalid id", in)
		}
	}
}

func TestServiceGetUserPropagatesNotFound(t *testing.T) {
	store := &fakeUserStore{getErr: domain.ErrUserNotFound}
	svc := service.NewService(store)

	_, err := svc.GetUser(context.Background(), uuid.New().String())
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("GetUser err = %v, want service.ErrNotFound", err)
	}
}

type fakeUserStore struct {
	called    bool
	input     domain.ListUsersInput
	result    domain.ListUsersResult
	err       error
	getCalled bool
	getID     uuid.UUID
	getResult domain.User
	getErr    error
}

func (f *fakeUserStore) ListUsers(ctx context.Context, input domain.ListUsersInput) (domain.ListUsersResult, error) {
	f.called = true
	f.input = input
	if f.err != nil {
		return domain.ListUsersResult{}, f.err
	}
	return f.result, nil
}

func (f *fakeUserStore) GetUserByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	f.getCalled = true
	f.getID = id
	if f.getErr != nil {
		return domain.User{}, f.getErr
	}
	return f.getResult, nil
}
