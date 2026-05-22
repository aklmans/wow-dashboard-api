package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	authservice "github.com/aklmans/wow-dashboard-api/internal/auth/service"
	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
	"github.com/aklmans/wow-dashboard-api/internal/projects/domain"
	projectservice "github.com/aklmans/wow-dashboard-api/internal/projects/service"
)

func TestProjectMembersHandler(t *testing.T) {
	authedUser := &authservice.PublicUser{ID: uuid.New().String(), Status: "active"}
	projectID := uuid.New()
	memberID := uuid.New()

	send := func(t *testing.T, projectsSvc *fakeProjectsService, method, path string, body map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		router := newProjectsTestRouter(&fakeUsersAuthService{currentUser: authedUser}, projectsSvc)
		var reader *bytes.Reader
		if body != nil {
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			reader = bytes.NewReader(raw)
		} else {
			reader = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, path, reader)
		req.Header.Set("Authorization", "Bearer access-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	membersPath := "/api/projects/" + projectID.String() + "/members"
	memberPath := membersPath + "/" + memberID.String()

	t.Run("list members returns the project members", func(t *testing.T) {
		projectsSvc := &fakeProjectsService{listMembersResult: []domain.ProjectMemberDetail{
			{UserID: memberID, Email: "m@example.com", DisplayName: "Mate", Role: domain.ProjectRoleEditor},
		}}
		rec := send(t, projectsSvc, http.MethodGet, membersPath, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if projectsSvc.listMembersProjectID != projectID.String() || projectsSvc.listMembersUserID != authedUser.ID {
			t.Fatalf("service args = %q/%q", projectsSvc.listMembersProjectID, projectsSvc.listMembersUserID)
		}
		var body struct {
			Members []struct {
				UserID string `json:"userId"`
				Email  string `json:"email"`
				Role   string `json:"role"`
			} `json:"members"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if len(body.Members) != 1 || body.Members[0].Role != "editor" {
			t.Fatalf("members = %#v", body.Members)
		}
	})

	t.Run("list members without authorization returns 401", func(t *testing.T) {
		router := newProjectsTestRouter(&fakeUsersAuthService{}, &fakeProjectsService{})
		req := httptest.NewRequest(http.MethodGet, membersPath, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assertAPIError(t, rec, http.StatusUnauthorized, apierror.CodeUnauthorized,
			"Authorization token missing or invalid.")
	})

	t.Run("add member returns 201 and forwards input", func(t *testing.T) {
		projectsSvc := &fakeProjectsService{addMemberResult: domain.ProjectMember{
			ProjectID: projectID, UserID: memberID, Role: domain.ProjectRoleEditor,
		}}
		rec := send(t, projectsSvc, http.MethodPost, membersPath,
			map[string]any{"email": "mate@example.com", "role": "editor"})
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
		}
		if projectsSvc.addMemberInput.Email != "mate@example.com" || projectsSvc.addMemberInput.Role != "editor" {
			t.Fatalf("add input = %#v", projectsSvc.addMemberInput)
		}
		if projectsSvc.addMemberInput.RequestingUserID != authedUser.ID {
			t.Fatalf("requesting user = %q, want %q", projectsSvc.addMemberInput.RequestingUserID, authedUser.ID)
		}
	})

	t.Run("add member maps forbidden and conflict", func(t *testing.T) {
		forbidden := send(t, &fakeProjectsService{addMemberErr: projectservice.ErrForbidden},
			http.MethodPost, membersPath, map[string]any{"email": "mate@example.com", "role": "editor"})
		assertAPIError(t, forbidden, http.StatusForbidden, apierror.CodeForbidden,
			"You do not have permission to perform this action on the project.")

		conflict := send(t, &fakeProjectsService{addMemberErr: projectservice.ErrMemberConflict},
			http.MethodPost, membersPath, map[string]any{"email": "mate@example.com", "role": "editor"})
		assertAPIError(t, conflict, http.StatusConflict, apierror.CodeConflict,
			"That user already has access to the project.")
	})

	t.Run("add member with invalid role is rejected at the schema edge", func(t *testing.T) {
		rec := send(t, &fakeProjectsService{}, http.MethodPost, membersPath,
			map[string]any{"email": "mate@example.com", "role": "owner"})
		assertAPIError(t, rec, http.StatusUnprocessableEntity, apierror.CodeValidationFailed, "Invalid request.")
	})

	t.Run("update member role succeeds", func(t *testing.T) {
		projectsSvc := &fakeProjectsService{updateMemberResult: domain.ProjectMember{
			ProjectID: projectID, UserID: memberID, Role: domain.ProjectRoleViewer,
		}}
		rec := send(t, projectsSvc, http.MethodPatch, memberPath, map[string]any{"role": "viewer"})
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if projectsSvc.updateMemberInput.TargetUserID != memberID.String() || projectsSvc.updateMemberInput.Role != "viewer" {
			t.Fatalf("update input = %#v", projectsSvc.updateMemberInput)
		}
	})

	t.Run("update member maps not found", func(t *testing.T) {
		rec := send(t, &fakeProjectsService{updateMemberErr: projectservice.ErrNotFound},
			http.MethodPatch, memberPath, map[string]any{"role": "viewer"})
		assertAPIError(t, rec, http.StatusNotFound, apierror.CodeNotFound, "Project not found.")
	})

	t.Run("remove member succeeds", func(t *testing.T) {
		projectsSvc := &fakeProjectsService{}
		rec := send(t, projectsSvc, http.MethodDelete, memberPath, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if !projectsSvc.removeMemberCalled || projectsSvc.removeMemberTargetID != memberID.String() {
			t.Fatalf("remove not forwarded: called=%v target=%q", projectsSvc.removeMemberCalled, projectsSvc.removeMemberTargetID)
		}
	})

	t.Run("remove member maps forbidden", func(t *testing.T) {
		rec := send(t, &fakeProjectsService{removeMemberErr: projectservice.ErrForbidden},
			http.MethodDelete, memberPath, nil)
		assertAPIError(t, rec, http.StatusForbidden, apierror.CodeForbidden,
			"You do not have permission to perform this action on the project.")
	})
}
