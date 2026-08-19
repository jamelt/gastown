package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// installHangingGitStub writes a fake `git` binary that hangs (via exec, so
// killing the tracked pid actually stops it) on any invocation whose
// arguments contain hangOn (e.g. "fetch" or "worktree"), and passes
// everything else through to the real git binary captured before PATH is
// swapped. Mirrors installFakeGH's PATH-stub pattern in pr_lookup_test.go.
// Reproduces gt-vyik: a slow/wedged remote hanging a network git operation
// that previously had no bound.
func installHangingGitStub(t *testing.T, hangOn string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix shell stub")
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("finding real git: %v", err)
	}
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "git")
	script := "#!/bin/sh\ncase \"$*\" in\n  *" + hangOn + "*)\n    exec sleep 300\n    ;;\n  *)\n    exec " + realGit + " \"$@\"\n    ;;\nesac\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestFetchBoundedOnHangingRemote reproduces gt-vyik: before the fix, Fetch
// shelled out via a plain exec.Command with no timeout, so a wedged remote
// hung the caller (and, transitively, the whole scheduler dispatch loop)
// forever. The fix routes it through runWithTimeout, bounded by
// GT_GIT_NETWORK_TIMEOUT_SEC.
func TestFetchBoundedOnHangingRemote(t *testing.T) {
	dir := initTestRepo(t)
	g := NewGit(dir)
	installHangingGitStub(t, "fetch")
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
	installHangingGitStub(t, "worktree")
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
