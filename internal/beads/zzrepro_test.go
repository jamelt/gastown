package beads

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/gastown/internal/scheduler/capacity"
)

// zzrepro is a throwaway reproduction test for gt-76w, not meant to be committed.
func TestZZReproCreateSlingContext(t *testing.T) {
	beadsDir := "/tmp/claude-1000/-home-jamel-Projects-gastown-ops-gastown-polecats-minuteman-gastown/935e219e-8d2b-4efa-a102-72394c8d2613/scratchpad/bd-repro/.beads"
	if _, err := os.Stat(beadsDir); err != nil {
		t.Skipf("repro db not present: %v", err)
	}
	workDir := filepath.Dir(beadsDir)
	os.Setenv("GT_DOLT_PORT", "13307")
	os.Setenv("BEADS_DOLT_SERVER_PORT", "13307")
	os.Setenv("BEADS_DOLT_PORT", "13307")
	os.Setenv("BEADS_DOLT_AUTO_START", "0")

	b := NewWithBeadsDir(workDir, beadsDir)
	fields := &capacity.SlingContextFields{
		Version:    1,
		WorkBeadID: "gt-work-1",
		TargetRig:  "gastown",
		EnqueuedBy: "test",
		EnqueuedAt: "2026-08-19T00:00:00Z",
	}
	issue, err := b.CreateSlingContext("Some work bead title", "gt-work-1", fields)
	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
		t.Fatalf("CreateSlingContext failed: %v", err)
	}
	fmt.Printf("OK: %+v\n", issue)
}
