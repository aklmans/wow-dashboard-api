//go:build integration

package authrepo_test

import (
	"context"
	"encoding/json"
	"testing"

	authservice "github.com/aklmans/wow-dashboard-api/internal/auth/service"
	"github.com/aklmans/wow-dashboard-api/internal/store/authrepo"
)

func TestSystemEventRecorderIntegration(t *testing.T) {
	ctx := context.Background()
	queries := newAuthRepoTestQueries(t, ctx, "test_authrepo_audit_db")
	recorder := authrepo.NewSystemEventRecorder(queries)

	err := recorder.RecordAuthEvent(ctx, authservice.AuditEvent{
		EventType: authservice.EventAuthSignInSucceeded,
		Message:   "Auth sign-in succeeded.",
		Metadata: authservice.AuditMetadata{
			Email:     "demo@example.com",
			UserID:    "00000000-0000-0000-0000-000000000123",
			Role:      "admin",
			RequestID: "req-audit-123",
		},
	})
	if err != nil {
		t.Fatalf("RecordAuthEvent returned error: %v", err)
	}

	events, err := queries.ListSystemEvents(ctx, 10)
	if err != nil {
		t.Fatalf("ListSystemEvents failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("system event count = %d, want 1", len(events))
	}
	if events[0].EventType != authservice.EventAuthSignInSucceeded {
		t.Fatalf("event_type = %q, want sign-in success", events[0].EventType)
	}

	var metadata map[string]string
	if err := json.Unmarshal(events[0].Metadata, &metadata); err != nil {
		t.Fatalf("metadata is not JSON object: %v", err)
	}
	for key, want := range map[string]string{
		"email":      "demo@example.com",
		"user_id":    "00000000-0000-0000-0000-000000000123",
		"role":       "admin",
		"request_id": "req-audit-123",
	} {
		if got := metadata[key]; got != want {
			t.Fatalf("metadata[%q] = %q, want %q; metadata=%v", key, got, want, metadata)
		}
	}
}
