package session

import (
	"strings"
	"testing"
)

// TestInitRegistry_BlocksNonIsolatedTownRoot simulates a stale/unprotected
// worktree's test: InitRegistry called with the process's own repo-relative
// working directory, exactly what a test binary resolves GT_ROOT to when
// its own TestMain never overrode it. It proves InitRegistry never installs
// the live town's derived socket as the process default. See gt-8ik.
func TestInitRegistry_BlocksNonIsolatedTownRoot(t *testing.T) {
	err := InitRegistry(".")
	if err == nil {
		t.Fatal("expected InitRegistry to fail closed for a non-isolated town root")
	}
	// Assert this is specifically the gt-8ik guard, not some unrelated
	// error from BuildPrefixRegistryFromTown failing on "." for other
	// reasons (which would make this test pass for the wrong reason).
	if !strings.Contains(err.Error(), "gt-8ik guard") {
		t.Fatalf("expected gt-8ik guard error, got: %v", err)
	}
}

// TestInitRegistry_AllowsIsolatedTownRoot confirms a t.TempDir()-rooted town
// (the pattern every isolated TestMain in this repo already uses) is never
// blocked by the guard.
func TestInitRegistry_AllowsIsolatedTownRoot(t *testing.T) {
	if err := InitRegistry(t.TempDir()); err != nil {
		t.Fatalf("expected isolated town root to bypass the guard, got: %v", err)
	}
}
