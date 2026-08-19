package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/lock"
)

// writeHangOnAgentQueryBDStub writes a fake bd that hangs (via exec, so
// killing the tracked pid actually stops it, matching how a real bd
// subprocess is cancelled) on the two query shapes the polecat capacity
// snapshot issues per rig (`bd list --label=gt:agent ...` and
// `bd query ... "ephemeral=false AND (...)"`), and responds instantly to
// everything else (sling-context scans, merge-request lookups, etc.).
// This reproduces the gt-5p9 incident: a slow/wedged rig database blocking
// the multi-rig inventory loop that runs while the scheduler dispatch lock
// is held.
func writeHangOnAgentQueryBDStub(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix shell stub")
	}
	scriptPath := filepath.Join(dir, "bd")
	script := `#!/bin/sh
case "$*" in
  *gt:agent*|*"ephemeral=false"*)
    exec sleep 30
    ;;
  *)
    printf '[]\n'
    exit 0
    ;;
esac
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake bd: %v", err)
	}
}

func writeAlwaysEmptyBDStub(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix shell stub")
	}
	scriptPath := filepath.Join(dir, "bd")
	script := `#!/bin/sh
printf '[]\n'
exit 0
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake bd: %v", err)
	}
}

// setupMultiRigCapacityTown creates a town with maxPolecats > 0 and the
// given rig names each registered with one polecat directory, so the
// capacity snapshot's per-rig loop actually issues bd queries for each.
func setupMultiRigCapacityTown(t *testing.T, rigNames ...string) string {
	t.Helper()
	townRoot := t.TempDir()
	configureScheduler(t, townRoot, len(rigNames)+1, 1)
	rigs := map[string]config.RigEntry{}
	for _, name := range rigNames {
		rigs[name] = config.RigEntry{GitURL: "https://example.invalid/" + name + ".git"}
		if err := os.MkdirAll(filepath.Join(townRoot, name, "polecats", "p1"), 0755); err != nil {
			t.Fatalf("mkdir polecat dir for %s: %v", name, err)
		}
		if err := os.MkdirAll(filepath.Join(townRoot, name, ".beads"), 0755); err != nil {
			t.Fatalf("mkdir beads dir for %s: %v", name, err)
		}
	}
	if err := config.SaveRigsConfig(filepath.Join(townRoot, "mayor", "rigs.json"), &config.RigsConfig{
		Version: config.CurrentRigsVersion,
		Rigs:    rigs,
	}); err != nil {
		t.Fatalf("SaveRigsConfig: %v", err)
	}
	return townRoot
}

// TestPolecatCapacitySnapshotBoundsHungRigQuery reproduces gt-5p9: one rig's
// database is wedged and its bd query never returns. Before the fix, the
// per-rig loop had no aggregate deadline, so this call would block for as
// long as the (generous, 20s-in-this-test) per-call subprocess timeout —
// and in production, for as long as every configured rig took to time out
// in turn. The fix bounds the whole multi-rig loop with a single deadline
// (GT_SCHEDULER_INVENTORY_TIMEOUT_SEC), so the call must return promptly
// and name the stalled rig.
func TestPolecatCapacitySnapshotBoundsHungRigQuery(t *testing.T) {
	binDir := t.TempDir()
	writeHangOnAgentQueryBDStub(t, binDir)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Generous per-call timeout: a fast return here can only be explained by
	// the aggregate inventory deadline, not the existing per-call bound.
	t.Setenv("GT_BD_TIMEOUT_SEC", "20")
	t.Setenv("GT_SCHEDULER_INVENTORY_TIMEOUT_SEC", "1")

	townRoot := setupMultiRigCapacityTown(t, "rigone", "rigtwo")

	start := time.Now()
	_, err := polecatCapacitySnapshotForTownNoCleanup(townRoot)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from hung rig query, got nil")
	}
	if elapsed > 10*time.Second {
		t.Fatalf("snapshot took %v, want bounded by the ~1s inventory timeout (not the 20s per-call timeout or worse)", elapsed)
	}
	if !strings.Contains(err.Error(), "capacity") {
		t.Fatalf("error = %q, want it to identify the capacity-snapshot phase", err.Error())
	}
}

// TestPolecatCapacitySnapshotMultiRigHealthySucceeds is the sanity
// counterpart to the hang test above: with several rigs that all respond
// quickly, the added aggregate deadline must not break normal multi-rig
// operation.
func TestPolecatCapacitySnapshotMultiRigHealthySucceeds(t *testing.T) {
	binDir := t.TempDir()
	writeAlwaysEmptyBDStub(t, binDir)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GT_SCHEDULER_INVENTORY_TIMEOUT_SEC", "5")

	townRoot := setupMultiRigCapacityTown(t, "rigone", "rigtwo", "rigthree")

	start := time.Now()
	snapshot, err := polecatCapacitySnapshotForTownNoCleanup(townRoot)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("snapshot with healthy rigs: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("healthy multi-rig snapshot took %v, want well under the 5s inventory deadline", elapsed)
	}
	if snapshot.Max != 4 {
		t.Fatalf("snapshot.Max = %d, want 4", snapshot.Max)
	}
}

// TestDispatchScheduledWorkReleasesLockAfterInventoryTimeout verifies the
// scheduler-dispatch.lock (gt-5p9) is released promptly when the capacity
// snapshot phase times out, instead of being held for as long as a wedged
// rig database takes to (eventually) fail. Nothing is dispatched before the
// capacity snapshot completes, so a timeout here cannot duplicate or lose an
// already-dispatched context — the lock simply becomes available again for
// the next dispatch attempt.
func TestDispatchScheduledWorkReleasesLockAfterInventoryTimeout(t *testing.T) {
	binDir := t.TempDir()
	writeHangOnAgentQueryBDStub(t, binDir)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GT_BD_TIMEOUT_SEC", "20")
	t.Setenv("GT_SCHEDULER_INVENTORY_TIMEOUT_SEC", "1")

	townRoot := setupMultiRigCapacityTown(t, "rigone")

	start := time.Now()
	_, err := dispatchScheduledWork(townRoot, "test", 1, false)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected dispatchScheduledWork to fail when the capacity snapshot times out")
	}
	if elapsed > 10*time.Second {
		t.Fatalf("dispatchScheduledWork took %v, want bounded by the ~1s inventory timeout", elapsed)
	}

	lockFile := schedulerDispatchLockPath(townRoot)
	if _, err := lock.New(lockFile).Read(); !errors.Is(err, lock.ErrNotLocked) {
		t.Fatalf("scheduler-dispatch.lock Read() after timeout = %v, want ErrNotLocked (lock released)", err)
	}
}
