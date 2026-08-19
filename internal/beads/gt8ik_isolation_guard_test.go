package beads

import (
	"os/exec"
	"strings"
	"testing"
)

// TestRunWithStdin_BlocksNonIsolatedMutation simulates a stale/unprotected
// worktree's test: a Beads instance that never opted into isolation
// (b.isolated is false) and whose working directory resolves to a plain
// repo path, exactly what a test binary inherits when its own TestMain
// never overrode GT_ROOT/BEADS_DIR. It proves the mutation never reaches a
// bd subprocess. See gt-8ik.
func TestRunWithStdin_BlocksNonIsolatedMutation(t *testing.T) {
	b := &Beads{workDir: "."}
	_, err := b.runWithStdin(nil, "create", "-t", "task", "-d", "should never run")
	if err == nil {
		t.Fatal("expected runWithStdin to fail closed for a non-isolated mutation")
	}
	if !strings.Contains(err.Error(), "gt-8ik guard") {
		t.Fatalf("expected gt-8ik guard error, got: %v", err)
	}
}

// TestRunWithStdin_AllowsReadOnlyAgainstNonIsolatedPath confirms the guard
// only gates mutations, not reads — a stale test reading live data isn't
// the threat this bead addresses, and over-blocking reads would make the
// guard indistinguishable from a general "no live GT_ROOT in tests" ban
// that this repo's existing tests don't uphold.
func TestRunWithStdin_AllowsReadOnlyAgainstNonIsolatedPath(t *testing.T) {
	b := &Beads{workDir: "."}
	_, err := b.runWithStdin(nil, "list", "--status=open")
	if err != nil && strings.Contains(err.Error(), "gt-8ik guard") {
		t.Fatalf("expected read-only command to bypass the guard, got: %v", err)
	}
}

// TestRunWithStdin_AllowsIsolatedInstance confirms an explicitly isolated
// Beads instance (as constructed by NewIsolatedWithPort/NewIsolated, the
// pattern this repo's legitimate integration tests use) is never blocked by
// the guard, regardless of its working directory.
func TestRunWithStdin_AllowsIsolatedInstance(t *testing.T) {
	b := &Beads{workDir: ".", isolated: true}
	_, err := b.runWithStdin(nil, "create", "-t", "task", "-d", "isolated instance")
	if err != nil && strings.Contains(err.Error(), "gt-8ik guard") {
		t.Fatalf("expected isolated instance to bypass the guard, got: %v", err)
	}
}

// TestConfigureCommand_BlocksNonIsolatedMutation covers the second bd
// subprocess chokepoint (internal/beads/exec.go), used directly by many
// internal/cmd, internal/refinery, internal/deacon, and internal/plugin
// call sites via beads.Command/CommandContext. See gt-8ik.
func TestConfigureCommand_BlocksNonIsolatedMutation(t *testing.T) {
	cmd := exec.Command("bd", "close", "gt-live-issue")
	ConfigureCommand(cmd, ".", "", MutationRouting)
	// Assert the guard actually rewired the command (not just "it happened
	// to fail," which could also be true if bd were simply missing from
	// PATH — that would make this test pass for the wrong reason).
	if !strings.Contains(cmd.Path, "gt-8ik-isolation-guard-blocked") {
		t.Fatalf("expected command to be rewired to the guard's blocked sentinel, got path %q", cmd.Path)
	}
	if err := cmd.Run(); err == nil {
		t.Fatal("expected blocked command to fail on Run()")
	}
}

// TestConfigureCommand_AllowsIsolatedMutation confirms a mutation targeting
// a recognized isolated sandbox is left alone (still attempts to run real
// bd; we only assert it wasn't rewired to the guard's blocked sentinel).
func TestConfigureCommand_AllowsIsolatedMutation(t *testing.T) {
	sandbox := t.TempDir()
	cmd := exec.Command("bd", "close", "gt-test-issue")
	ConfigureCommand(cmd, sandbox, sandbox, MutationRouting)
	if strings.Contains(cmd.Path, "gt-8ik-isolation-guard-blocked") {
		t.Fatalf("expected isolated sandbox mutation to bypass the guard, got blocked path %q", cmd.Path)
	}
}

// TestConfigureCommand_AllowsReadOnly confirms read-only modes are never
// guarded, matching the mutation-only scope of gt-8ik's acceptance criteria.
func TestConfigureCommand_AllowsReadOnly(t *testing.T) {
	cmd := exec.Command("bd", "show", "gt-live-issue")
	ConfigureCommand(cmd, ".", "", ReadOnlyRouting)
	if strings.Contains(cmd.Path, "gt-8ik-isolation-guard-blocked") {
		t.Fatalf("expected read-only command to bypass the guard, got blocked path %q", cmd.Path)
	}
}
