package tmux

import (
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/testutil"
)

// TestHasSessionBoundedOnHangingTmuxServer reproduces gt-h8bj: before the fix,
// Tmux.run shelled out to tmux via context.Background() (no deadline), so a
// wedged tmux server hung every caller forever -- including HasSession, which
// isHookedAgentDeadFn calls on every sling dispatch to check whether a
// hooked/in_progress agent's session is still alive. The fix bounds every
// call by GT_TMUX_TIMEOUT_SEC.
func TestHasSessionBoundedOnHangingTmuxServer(t *testing.T) {
	testutil.InstallHangingBinaryStub(t, "tmux", "has-session")
	t.Setenv("GT_TMUX_TIMEOUT_SEC", "1")

	tm := NewTmuxWithSocket("gt-test-h8bj")

	start := time.Now()
	_, err := tm.HasSession("nonexistent-session")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error from hung has-session, got nil")
	}
	if elapsed > 10*time.Second {
		t.Fatalf("HasSession took %v, want bounded by the ~1s GT_TMUX_TIMEOUT_SEC override", elapsed)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %q, want it to mention a timeout", err.Error())
	}
}

// TestHasSessionHealthySucceeds is the sanity counterpart: a normal, fast
// call against a real (if empty) tmux server must still succeed under the
// new bound.
func TestHasSessionHealthySucceeds(t *testing.T) {
	tm := NewTmuxWithSocket("gt-test-h8bj-healthy")
	defer func() { _ = tm.KillServer() }()

	if _, err := tm.HasSession("nonexistent-session"); err != nil {
		t.Fatalf("HasSession against a healthy tmux server: %v", err)
	}
}
