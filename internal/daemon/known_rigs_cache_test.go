package daemon

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestGetKnownRigs_ReadsFresh verifies that d.getKnownRigs() reads mayor/rigs.json
// fresh on every call. The per-heartbeat-tick memoization was removed in gt-4ecf
// once the patrol dogs began calling this concurrently from their own goroutines:
// a shared, unsynchronized cache is unsafe there, and rigs.json is small enough to
// re-read. Reading fresh also surfaces rig add/remove immediately rather than at
// the next tick.
func TestGetKnownRigs_ReadsFresh(t *testing.T) {
	townRoot := t.TempDir()
	mayorDir := filepath.Join(townRoot, "mayor")
	if err := os.MkdirAll(mayorDir, 0o755); err != nil {
		t.Fatalf("mkdir mayor: %v", err)
	}
	rigsPath := filepath.Join(mayorDir, "rigs.json")
	write := func(contents string) {
		if err := os.WriteFile(rigsPath, []byte(contents), 0o644); err != nil {
			t.Fatalf("write rigs.json: %v", err)
		}
	}

	d := &Daemon{config: &Config{TownRoot: townRoot}}

	write(`{"rigs":{"alpha":{},"beta":{}}}`)
	got := d.getKnownRigs()
	slices.Sort(got)
	if !slices.Equal(got, []string{"alpha", "beta"}) {
		t.Fatalf("initial read: got %v, want [alpha beta]", got)
	}

	// A rewrite must be visible on the very next call — no invalidation needed.
	write(`{"rigs":{"gamma":{}}}`)
	if got := d.getKnownRigs(); !slices.Equal(got, []string{"gamma"}) {
		t.Fatalf("read after rewrite: got %v, want [gamma] (stale cache?)", got)
	}

	// Deleting the file surfaces immediately as an empty list.
	if err := os.Remove(rigsPath); err != nil {
		t.Fatalf("remove rigs.json: %v", err)
	}
	if got := d.getKnownRigs(); len(got) != 0 {
		t.Fatalf("read after delete: got %v, want empty (stale cache?)", got)
	}
}
