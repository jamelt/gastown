package testguard

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRequireIsolated_BlocksLiveLookingPath(t *testing.T) {
	// A repo-relative path is exactly what a stale worktree's test binary
	// would resolve GT_ROOT/BEADS_DIR to if it never overrode them: the
	// process's own working directory, not a temp sandbox.
	live := "."
	if err := RequireIsolated("mutate agent bead", live); err == nil {
		t.Fatal("expected RequireIsolated to fail closed for a non-isolated path")
	} else if !strings.Contains(err.Error(), "gt-8ik guard") {
		t.Fatalf("expected gt-8ik guard error, got: %v", err)
	}
}

func TestRequireIsolated_AllowsIsolatedSandbox(t *testing.T) {
	sandbox := t.TempDir()
	if err := RequireIsolated("mutate agent bead", sandbox); err != nil {
		t.Fatalf("expected isolated sandbox to pass, got: %v", err)
	}
}

func TestRequireIsolated_AllowsEmptyPath(t *testing.T) {
	if err := RequireIsolated("no-op", ""); err != nil {
		t.Fatalf("expected empty path (nothing to check) to pass, got: %v", err)
	}
}

func TestRequireIsolatedSocket_BlocksLiveDerivedSocket(t *testing.T) {
	t.Setenv("GT_ROOT", "/home/user/gt")
	t.Setenv("GT_TOWN_ROOT", "")
	live := LiveSocketName("/home/user/gt")
	if err := RequireIsolatedSocket("tmux new-session", live); err == nil {
		t.Fatalf("expected RequireIsolatedSocket to fail closed for live-derived socket %q", live)
	} else if !strings.Contains(err.Error(), "gt-8ik guard") {
		t.Fatalf("expected gt-8ik guard error, got: %v", err)
	}
}

func TestRequireIsolatedSocket_AllowsUnrelatedSocketName(t *testing.T) {
	t.Setenv("GT_ROOT", "/home/user/gt")
	t.Setenv("GT_TOWN_ROOT", "")
	// A socket name that doesn't match what /home/user/gt would derive —
	// e.g. this repo's many arbitrary unique-per-test socket names — must
	// never be blocked just because *some* live GT_ROOT happens to be set.
	if err := RequireIsolatedSocket("tmux new-session", "gt-h9z-some-test-12345"); err != nil {
		t.Fatalf("expected unrelated socket name to pass, got: %v", err)
	}
}

func TestRequireIsolatedSocket_AllowsWhenGTRootUnset(t *testing.T) {
	t.Setenv("GT_ROOT", "")
	t.Setenv("GT_TOWN_ROOT", "")
	if err := RequireIsolatedSocket("tmux new-session", "anything"); err != nil {
		t.Fatalf("expected no live GT_ROOT to mean nothing to check, got: %v", err)
	}
}

func TestRequireIsolatedSocket_AllowsWhenGTRootIsIsolatedSandbox(t *testing.T) {
	sandbox := t.TempDir()
	t.Setenv("GT_ROOT", sandbox)
	t.Setenv("GT_TOWN_ROOT", "")
	live := LiveSocketName(sandbox)
	if err := RequireIsolatedSocket("tmux new-session", live); err != nil {
		t.Fatalf("expected isolated GT_ROOT sandbox to pass even for its own derived socket, got: %v", err)
	}
}

func TestRequireIsolatedSocket_BlocksEmptySocket(t *testing.T) {
	t.Setenv("GT_ROOT", "/home/user/gt")
	t.Setenv("GT_TOWN_ROOT", "")
	// An empty socket means tmux falls back to ambient/default resolution —
	// exactly what a test binary produces when its own TestMain never set
	// an isolated socket. This must fail closed, not just the case where
	// the socket exactly matches the live derived name.
	if err := RequireIsolatedSocket("tmux new-session", ""); err == nil {
		t.Fatal("expected RequireIsolatedSocket to fail closed for an empty socket while GT_ROOT is live")
	} else if !strings.Contains(err.Error(), "gt-8ik guard") {
		t.Fatalf("expected gt-8ik guard error, got: %v", err)
	}
}

func TestRequireIsolatedSocket_UsesGTTownRootFallback(t *testing.T) {
	t.Setenv("GT_ROOT", "")
	t.Setenv("GT_TOWN_ROOT", "/home/user/gt")
	live := LiveSocketName("/home/user/gt")
	if err := RequireIsolatedSocket("tmux new-session", live); err == nil {
		t.Fatal("expected GT_TOWN_ROOT fallback to be honored when GT_ROOT is unset")
	} else if !strings.Contains(err.Error(), "gt-8ik guard") {
		t.Fatalf("expected gt-8ik guard error, got: %v", err)
	}
}

func TestIsIsolatedSandbox_RejectsRepoPath(t *testing.T) {
	if IsIsolatedSandbox(".") {
		t.Fatal("expected repo-relative path to not be classified as an isolated sandbox")
	}
}

func TestIsIsolatedSandbox_AcceptsNestedTempPath(t *testing.T) {
	root := t.TempDir()
	nested := root + "/nested/town"
	if !IsIsolatedSandbox(nested) {
		t.Fatalf("expected nested path under t.TempDir() (%s) to be isolated", root)
	}
}

func TestIsIsolatedSandbox_ResolvesSymlinkedTempPath(t *testing.T) {
	real := t.TempDir()
	linkDir := t.TempDir()
	link := linkDir + "/town-link"
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}
	if !IsIsolatedSandbox(link) {
		t.Fatalf("expected a symlink (%s -> %s) into an isolated sandbox to itself be recognized as isolated", link, real)
	}
}

func TestBlock_PreventsSubprocessExecution(t *testing.T) {
	cmd := exec.Command("bd", "close", "gt-live-issue")
	Block(cmd, RequireIsolated("bd close gt-live-issue", "."))
	if err := cmd.Run(); err == nil {
		t.Fatal("expected blocked command to fail on Run()")
	}
}
