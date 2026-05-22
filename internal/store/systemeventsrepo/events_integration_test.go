//go:build integration

package systemeventsrepo_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/aklmans/wow-dashboard-api/internal/store/storetest"
	"github.com/aklmans/wow-dashboard-api/internal/store/systemeventsrepo"
	"github.com/aklmans/wow-dashboard-api/internal/systemevents/domain"
)

func TestEventStoreListEventsIntegration(t *testing.T) {
	ctx := context.Background()
	pool := storetest.NewPostgresPool(t, ctx, "test_systemeventsrepo_events_db", "../../../migrations")
	queries := query.New(pool)
	store := systemeventsrepo.NewEventStore(queries)

	olderID := uuid.New()
	newerID := uuid.New()
	olderCreatedAt := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	newerCreatedAt := olderCreatedAt.Add(time.Minute)

	insertSystemEvent(t, ctx, queries, olderID, "projects.project.created", "Project created.", []byte(`{"project_id":"project-1","count":2}`), olderCreatedAt)
	insertSystemEvent(t, ctx, queries, newerID, "auth.sign_in.succeeded", "Auth sign-in succeeded.", []byte(`["not","an","object"]`), newerCreatedAt)

	result, err := store.ListEvents(ctx, domain.ListEventsInput{Limit: 10})
	if err != nil {
		t.Fatalf("ListEvents returned error: %v", err)
	}

	if len(result.Events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(result.Events))
	}
	if result.Events[0].ID != newerID || result.Events[1].ID != olderID {
		t.Fatalf("events order = [%s, %s], want newest first [%s, %s]",
			result.Events[0].ID, result.Events[1].ID, newerID, olderID)
	}
	if len(result.Events[0].Metadata) != 0 {
		t.Fatalf("newer non-object metadata = %#v, want empty object", result.Events[0].Metadata)
	}
	if result.Events[1].Metadata["project_id"] != "project-1" {
		t.Fatalf("older metadata project_id = %#v, want project-1", result.Events[1].Metadata["project_id"])
	}
	if result.Events[1].Metadata["count"] != float64(2) {
		t.Fatalf("older metadata count = %#v, want 2", result.Events[1].Metadata["count"])
	}
}

func insertSystemEvent(t *testing.T, ctx context.Context, queries *query.Queries, id uuid.UUID, eventType string, message string, metadata []byte, createdAt time.Time) {
	t.Helper()

	_, err := queries.CreateSystemEvent(ctx, query.CreateSystemEventParams{
		ID:        pgUUID(t, id),
		EventType: eventType,
		Message:   message,
		Metadata:  metadata,
		CreatedAt: pgTimestamp(t, createdAt),
	})
	if err != nil {
		t.Fatalf("CreateSystemEvent(%s) failed: %v", eventType, err)
	}
}

func pgUUID(t *testing.T, id uuid.UUID) pgtype.UUID {
	t.Helper()

	var out pgtype.UUID
	if err := out.Scan(id.String()); err != nil {
		t.Fatalf("scan uuid: %v", err)
	}
	return out
}

func pgTimestamp(t *testing.T, ts time.Time) pgtype.Timestamptz {
	t.Helper()

	var out pgtype.Timestamptz
	if err := out.Scan(ts); err != nil {
		t.Fatalf("scan timestamptz: %v", err)
	}
	return out
}
