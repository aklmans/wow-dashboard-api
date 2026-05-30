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

func TestEventStoreKeysetPaginationIntegration(t *testing.T) {
	ctx := context.Background()
	pool := storetest.NewPostgresPool(t, ctx, "test_systemeventsrepo_keyset_db", "../../../migrations")
	queries := query.New(pool)
	store := systemeventsrepo.NewEventStore(queries)

	t0 := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)
	t2 := t0.Add(2 * time.Minute)
	t3 := t0.Add(3 * time.Minute)

	// Two events share t3 to exercise the (created_at, id) tiebreaker.
	type seed struct {
		id        uuid.UUID
		eventType string
		createdAt time.Time
	}
	seeds := []seed{
		{uuid.New(), "type.a", t0},
		{uuid.New(), "type.b", t1},
		{uuid.New(), "type.a", t2},
		{uuid.New(), "type.b", t3},
		{uuid.New(), "type.a", t3},
	}
	for _, s := range seeds {
		insertSystemEvent(t, ctx, queries, s.id, s.eventType, "msg", []byte(`{}`), s.createdAt)
	}

	// Page through with a page size of 2, following the keyset cursor.
	var all []domain.Event
	var cursorCreatedAt *time.Time
	var cursorID *uuid.UUID
	for i := 0; i < 10; i++ { // bound the loop defensively
		res, err := store.ListEvents(ctx, domain.ListEventsInput{Limit: 2, CursorCreatedAt: cursorCreatedAt, CursorID: cursorID})
		if err != nil {
			t.Fatalf("ListEvents page failed: %v", err)
		}
		if len(res.Events) == 0 {
			break
		}
		all = append(all, res.Events...)
		last := res.Events[len(res.Events)-1]
		lastCreatedAt, lastID := last.CreatedAt, last.ID
		cursorCreatedAt, cursorID = &lastCreatedAt, &lastID
		if len(res.Events) < 2 {
			break
		}
	}

	if len(all) != len(seeds) {
		t.Fatalf("keyset paging returned %d events, want %d (no gaps or dupes)", len(all), len(seeds))
	}
	seen := map[uuid.UUID]bool{}
	for i, e := range all {
		if seen[e.ID] {
			t.Fatalf("event %s returned twice across pages", e.ID)
		}
		seen[e.ID] = true
		if i > 0 {
			prev := all[i-1]
			// Strictly descending under (created_at DESC, id DESC).
			if e.CreatedAt.After(prev.CreatedAt) || (e.CreatedAt.Equal(prev.CreatedAt) && bytesGreater(e.ID, prev.ID)) {
				t.Fatalf("ordering violated at %d: %v/%s after %v/%s", i, e.CreatedAt, e.ID, prev.CreatedAt, prev.ID)
			}
		}
	}

	// event_type filter: only the three type.a events.
	typeA := "type.a"
	filtered, err := store.ListEvents(ctx, domain.ListEventsInput{Limit: 10, EventType: &typeA})
	if err != nil {
		t.Fatalf("filtered ListEvents failed: %v", err)
	}
	if len(filtered.Events) != 3 {
		t.Fatalf("event_type filter returned %d, want 3 type.a events", len(filtered.Events))
	}
	for _, e := range filtered.Events {
		if e.EventType != typeA {
			t.Fatalf("filtered event type = %q, want %q", e.EventType, typeA)
		}
	}

	// created_at range [t2, t3): only the single t2 event.
	ranged, err := store.ListEvents(ctx, domain.ListEventsInput{Limit: 10, CreatedAfter: &t2, CreatedBefore: &t3})
	if err != nil {
		t.Fatalf("ranged ListEvents failed: %v", err)
	}
	if len(ranged.Events) != 1 || !ranged.Events[0].CreatedAt.Equal(t2) {
		t.Fatalf("range [t2,t3) returned %d events, want 1 at t2", len(ranged.Events))
	}
}

func bytesGreater(a, b uuid.UUID) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
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
