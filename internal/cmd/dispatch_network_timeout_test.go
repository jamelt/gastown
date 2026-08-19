package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/scheduler/capacity"
)

// installHangingGitStubForDispatchTest writes a fake `git` binary that hangs
// on any invocation whose arguments contain hangOn, and passes everything
// else through to the real git binary captured before PATH is swapped.
// Mirrors the equivalent helper in internal/git/network_timeout_test.go —
// duplicated here since test helpers aren't shared across package
// boundaries. Named distinctly from that package's installHangingGitStub to
// avoid any confusion about which package's version is in scope.
func installHangingGitStubForDispatchTest(t *testing.T, hangOn string) {
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

// TestDispatchCycle_RunPlan_BoundedOnSecondRigNetworkHang reproduces gt-vyik
// end to end at the orchestrator level shared by both `gt scheduler run` and
// the daemon: a batch dispatches a bead to one rig ("gastown") successfully,
// then reaches a bead for a second, different rig ("dotfiles") whose spawn
// path hits a genuinely slow/wedged git remote. Before the fix, the git
// operations reached during a fresh-polecat spawn (Fetch, WorktreeAdd*) had
// no timeout at all, so capacity.DispatchCycle.RunPlan's per-bead loop
// (which itself also has no aggregate deadline) blocked forever on the
// second bead -- this is the literal "gt scheduler run hangs dispatching
// dotfiles bead after Gastown batch" symptom. After the fix, git.Fetch is
// bounded, so RunPlan returns promptly with the first bead dispatched and
// the second recorded as a bounded failure via OnFailure -- never a hang.
func TestDispatchCycle_RunPlan_BoundedOnSecondRigNetworkHang(t *testing.T) {
	installHangingGitStubForDispatchTest(t, "fetch")
	t.Setenv("GT_GIT_NETWORK_TIMEOUT_SEC", "1")

	dir := t.TempDir()
	executed := []string{}
	failureErrs := map[string]error{}

	cycle := &capacity.DispatchCycle{
		AvailableCapacity: func() (int, error) { return 100, nil },
		QueryPending: func() ([]capacity.PendingBead, error) {
			return []capacity.PendingBead{
				{ID: "ctx-gastown", WorkBeadID: "gt-abc", TargetRig: "gastown"},
				{ID: "ctx-dotfiles", WorkBeadID: "do-99a", TargetRig: "dotfiles"},
			}, nil
		},
		Execute: func(b capacity.PendingBead) error {
			executed = append(executed, b.WorkBeadID)
			if b.TargetRig == "dotfiles" {
				// Simulates spawnPolecatForSling's git fetch during a
				// fresh-polecat spawn for a rig whose remote is wedged.
				return git.NewGit(dir).Fetch("origin")
			}
			return nil
		},
		OnSuccess: func(b capacity.PendingBead) error { return nil },
		OnFailure: func(b capacity.PendingBead, err error) { failureErrs[b.WorkBeadID] = err },
		BatchSize: 10,
	}

	start := time.Now()
	report, err := cycle.Run()
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("RunPlan took %v, want bounded by the ~1s GT_GIT_NETWORK_TIMEOUT_SEC override, not an indefinite hang", elapsed)
	}
	if report.Dispatched != 1 {
		t.Errorf("Dispatched = %d, want 1 (gastown succeeds)", report.Dispatched)
	}
	if report.Failed != 1 {
		t.Errorf("Failed = %d, want 1 (dotfiles times out, bounded)", report.Failed)
	}
	if len(executed) != 2 {
		t.Errorf("Execute should run for both beads (gastown then dotfiles), got %v", executed)
	}
	dotfilesErr := failureErrs["do-99a"]
	if dotfilesErr == nil {
		t.Fatal("expected a recorded failure for do-99a, got nil")
	}
	if !strings.Contains(dotfilesErr.Error(), "timed out") {
		t.Errorf("do-99a failure = %q, want it to mention a timeout", dotfilesErr.Error())
	}
}
