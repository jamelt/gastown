package tmux

import (
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/testguard"
)

// TestNewSession_BlocksLiveDerivedSocket simulates a stale/unprotected
// worktree's test: GT_ROOT resolves to a live (non-isolated) town, exactly
// what a test binary inherits when its own TestMain never overrode it, and
// the Tmux instance targets exactly the socket that GT_ROOT derives as its
// default. It proves session creation never reaches the tmux binary. See
// gt-8ik.
func TestNewSession_BlocksLiveDerivedSocket(t *testing.T) {
	t.Setenv("GT_ROOT", "/home/user/gt")
	t.Setenv("GT_TOWN_ROOT", "")
	live := NewTmuxWithSocket(testguard.LiveSocketName("/home/user/gt"))
	err := live.NewSession("gt-test-should-never-be-created", "")
	if err == nil {
		t.Fatal("expected NewSession to fail closed against the live town's derived socket")
	}
	if !strings.Contains(err.Error(), "gt-8ik guard") {
		t.Fatalf("expected gt-8ik guard error, got: %v", err)
	}
}

// TestNewSession_BlocksEmptySocket covers the realistic "isolation code
// never ran" case: a test binary whose TestMain never called
// SetDefaultSocket/set GT_TMUX_SOCKET ends up with an unnamed socket, which
// tmux resolves via ambient/default socket lookup rather than a named one.
// With a live GT_ROOT in play, that must fail closed too, not just the
// exact-live-socket-name case. See gt-8ik.
func TestNewSession_BlocksEmptySocket(t *testing.T) {
	t.Setenv("GT_ROOT", "/home/user/gt")
	t.Setenv("GT_TOWN_ROOT", "")
	unnamed := NewTmuxWithSocket("")
	err := unnamed.NewSession("gt-test-should-never-be-created", "")
	if err == nil {
		t.Fatal("expected NewSession to fail closed for an empty (ambient/default) socket while GT_ROOT is live")
	}
	if !strings.Contains(err.Error(), "gt-8ik guard") {
		t.Fatalf("expected gt-8ik guard error, got: %v", err)
	}
}

// TestNewSession_AllowsUnrelatedSocketName confirms this repo's existing
// tests, which use many different unique-per-run socket names rather than
// one fixed convention, are never blocked just because some live GT_ROOT
// happens to be set in the test process's environment.
func TestNewSession_AllowsUnrelatedSocketName(t *testing.T) {
	if !hasTmux() {
		t.Skip("tmux not installed")
	}
	t.Setenv("GT_ROOT", "/home/user/gt")
	t.Setenv("GT_TOWN_ROOT", "")
	isolated := NewTmuxWithSocket("gt-test-guard-check")
	defer func() { _ = isolated.KillServer() }()
	if err := isolated.NewSession("gt-test-guard-session", ""); err != nil {
		t.Fatalf("expected unrelated socket name to bypass the guard, got: %v", err)
	}
}
