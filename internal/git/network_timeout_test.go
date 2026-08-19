package git

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/testutil"
)

// TestFetchBoundedOnHangingRemote reproduces gt-vyik: before the fix, Fetch
// shelled out via a plain exec.Command with no timeout, so a wedged remote
// hung the caller (and, transitively, the whole scheduler dispatch loop)
// forever. The fix routes it through runWithTimeout, bounded by
// GT_GIT_NETWORK_TIMEOUT_SEC.
func TestFetchBoundedOnHangingRemote(t *testing.T) {
	dir := initTestRepo(t)
	g := NewGit(dir)
	testutil.InstallHangingBinaryStub(t, "git", "fetch")
	t.Setenv("GT_GIT_NETWORK_TIMEOUT_SEC", "1")

	start := time.Now()
	err := g.Fetch("origin")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error from hung fetch, got nil")
	}
	if elapsed > 10*time.Second {
		t.Fatalf("Fetch took %v, want bounded by the ~1s GT_GIT_NETWORK_TIMEOUT_SEC override", elapsed)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %q, want it to mention a timeout", err.Error())
	}
}

// TestWorktreeAddFromRefBoundedOnHang is the WorktreeAdd-family counterpart:
// before the fix these ran via runWithEnv (timeout=0, i.e. unbounded).
func TestWorktreeAddFromRefBoundedOnHang(t *testing.T) {
	dir := initTestRepo(t)
	g := NewGit(dir)
	testutil.InstallHangingBinaryStub(t, "git", "worktree")
	t.Setenv("GT_GIT_NETWORK_TIMEOUT_SEC", "1")

	start := time.Now()
	err := g.WorktreeAddFromRef(filepath.Join(t.TempDir(), "wt"), "gt-vyik-test-branch", "HEAD")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error from hung worktree add, got nil")
	}
	if elapsed > 10*time.Second {
		t.Fatalf("WorktreeAddFromRef took %v, want bounded by the ~1s GT_GIT_NETWORK_TIMEOUT_SEC override", elapsed)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %q, want it to mention a timeout", err.Error())
	}
}

// TestFetchHealthySucceeds is the sanity counterpart: a normal, fast fetch
// against a real local remote must still succeed under the new bound.
func TestFetchHealthySucceeds(t *testing.T) {
	remoteDir := initTestRepo(t)
	cloneDir := filepath.Join(t.TempDir(), "clone")
	g := NewGit(cloneDir)
	if err := g.Clone(remoteDir, cloneDir); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if err := g.Fetch("origin"); err != nil {
		t.Fatalf("Fetch against a healthy local remote: %v", err)
	}
}
