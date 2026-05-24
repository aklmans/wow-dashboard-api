package jobs

import (
	"testing"

	"github.com/riverqueue/river"
)

// TestPingArgs_Kind locks in the persisted kind string so a rename does not
// silently strand jobs queued under the old name.
func TestPingArgs_Kind(t *testing.T) {
	if got := (PingArgs{}).Kind(); got != "ping" {
		t.Fatalf("Kind() = %q, want %q", got, "ping")
	}
}

// TestRegisterAll wires the worker registry the same way cmd/worker does and
// confirms the registration does not panic — duplicate registrations or
// type-mismatched workers would surface here at the boundary cmd/worker uses.
func TestRegisterAll(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RegisterAll panicked: %v", r)
		}
	}()
	RegisterAll(river.NewWorkers(), Dependencies{})
}
