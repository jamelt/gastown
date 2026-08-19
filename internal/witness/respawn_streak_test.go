package witness

import (
	"path/filepath"
	"testing"
)

// The respawn guard is meant to stop a bead that repeatedly KILLS its polecat.
// RecordBeadRespawn increments before the spawn is attempted, so without a
// reset-on-success the counter measures dispatches rather than failures: a bead
// that succeeded three times was blocked forever, and any unrelated outage
// (2026-08-19: a full polecat-directory cap) burned every queued bead's three
// attempts against a condition that had nothing to do with the task.
func TestRespawnCounterResetsOnSuccess(t *testing.T) {
	dir := t.TempDir()
	bead := "gt-example"

	// Two dispatches that failed — the streak builds, as intended.
	RecordBeadRespawn(dir, bead)
	RecordBeadRespawn(dir, bead)
	if ShouldBlockRespawn(dir, bead) {
		t.Fatal("blocked after 2 attempts; the guard should allow the third")
	}

	// A successful dispatch clears the streak.
	if err := ResetBeadRespawnCount(dir, bead); err != nil {
		t.Fatalf("ResetBeadRespawnCount: %v", err)
	}

	// The bead must now get a full allowance again rather than latching on the
	// next attempt. This is the regression: previously the count survived the
	// success and the bead latched on its third-ever dispatch.
	RecordBeadRespawn(dir, bead)
	RecordBeadRespawn(dir, bead)
	if ShouldBlockRespawn(dir, bead) {
		t.Fatal("blocked after a success reset plus 2 attempts — the success did not clear the streak")
	}
}

// Consecutive failures with no intervening success must still latch: the guard
// exists to stop spawn storms and this fix must not disable it.
func TestRespawnCounterStillLatchesOnConsecutiveFailures(t *testing.T) {
	dir := t.TempDir()
	bead := "gt-persistent-failure"
	for i := 0; i < 4; i++ {
		if ShouldBlockRespawn(dir, bead) {
			return // latched, as intended
		}
		RecordBeadRespawn(dir, bead)
	}
	t.Fatalf("never latched after 4 consecutive failed attempts; state file %s",
		filepath.Join(dir, ".runtime"))
}
