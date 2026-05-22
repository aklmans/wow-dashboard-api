//go:build integration

package projectsrepo_test

import (
	"context"
	"encoding/json"
	"testing"

	projectservice "github.com/aklmans/wow-dashboard-api/internal/projects/service"
	"github.com/aklmans/wow-dashboard-api/internal/store/projectsrepo"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/aklmans/wow-dashboard-api/internal/store/storetest"
)

func TestProjectsSystemEventRecorderIntegration(t *testing.T) {
	ctx := context.Background()
	pool := storetest.NewPostgresPool(t, ctx, "test_projectsrepo_audit_db", "../../../migrations")
	queries := query.New(pool)
	recorder := projectsrepo.NewSystemEventRecorder(queries)

	err := recorder.RecordProjectEvent(ctx, projectservice.AuditEvent{
		EventType: projectservice.EventProjectUpdated,
		Message:   "Project updated.",
		Metadata: projectservice.AuditMetadata{
			ProjectID:     "00000000-0000-0000-0000-000000000abc",
			OwnerUserID:   "00000000-0000-0000-0000-000000000def",
			Status:        "active",
			ChangedFields: []string{"name", "status"},
			RequestID:     "req-projects-audit-1",
		},
	})
	if err != nil {
		t.Fatalf("RecordProjectEvent returned error: %v", err)
	}

	events, err := queries.ListSystemEvents(ctx, 10)
	if err != nil {
		t.Fatalf("ListSystemEvents failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("system event count = %d, want 1", len(events))
	}
	if events[0].EventType != projectservice.EventProjectUpdated {
		t.Fatalf("event_type = %q, want %q", events[0].EventType, projectservice.EventProjectUpdated)
	}

	var metadata map[string]any
	if err := json.Unmarshal(events[0].Metadata, &metadata); err != nil {
		t.Fatalf("metadata is not a JSON object: %v", err)
	}
	for _, key := range []string{"project_id", "owner_user_id", "status", "changed_fields", "request_id"} {
		if _, ok := metadata[key]; !ok {
			t.Fatalf("metadata missing snake_case key %q; got=%v", key, metadata)
		}
	}
	if metadata["project_id"] != "00000000-0000-0000-0000-000000000abc" {
		t.Fatalf("metadata project_id = %v", metadata["project_id"])
	}
	if metadata["owner_user_id"] != "00000000-0000-0000-0000-000000000def" {
		t.Fatalf("metadata owner_user_id = %v", metadata["owner_user_id"])
	}
	if metadata["status"] != "active" {
		t.Fatalf("metadata status = %v", metadata["status"])
	}
	if metadata["request_id"] != "req-projects-audit-1" {
		t.Fatalf("metadata request_id = %v", metadata["request_id"])
	}
	changed, ok := metadata["changed_fields"].([]any)
	if !ok {
		t.Fatalf("changed_fields is %T, want array", metadata["changed_fields"])
	}
	wantFields := []string{"name", "status"}
	if len(changed) != len(wantFields) {
		t.Fatalf("changed_fields = %v, want %v", changed, wantFields)
	}
	for i, want := range wantFields {
		if changed[i] != want {
			t.Fatalf("changed_fields[%d] = %v, want %q", i, changed[i], want)
		}
	}
}
