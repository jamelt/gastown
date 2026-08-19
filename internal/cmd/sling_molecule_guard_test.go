package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// moleculeStepBeadBDStub is a bd stub whose `show` returns a type=task step
// bead parented to a molecule via a parent-child dependency — the exact shape
// of the stale gt-6va3 fixture beads (e.g. gt-nhyl parented to gt-vf2u).
const moleculeStepBeadBDStub = `#!/bin/sh
case "$1" in
  show)
    echo '[{"id":"gt-step1","title":"Load context and verify assignment","status":"open","assignee":"","description":"","issue_type":"task","dependencies":[{"id":"gt-vf2u","issue_type":"molecule","dependency_type":"parent-child"}]}]'
    ;;
esac
exit 0
`

// TestExecuteSling_MoleculeStepBead verifies executeSling rejects a materialized
// formula-molecule step bead (parent-child dependency to a molecule). Guards the
// call-site wiring at sling_dispatch.go, not just moleculeScaffoldRejectReason:
// deleting the guard invocation must make this fail (gt-6va3).
func TestExecuteSling_MoleculeStepBead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	townRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(townRoot, ".beads"), 0o755); err != nil {
		t.Fatalf("failed to create .beads: %v", err)
	}
	binDir := filepath.Join(townRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir binDir: %v", err)
	}
	writeBDStub(t, binDir, moleculeStepBeadBDStub, "")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	result, err := executeSling(SlingParams{
		BeadID:   "gt-step1",
		RigName:  "testrig",
		TownRoot: townRoot,
	})
	if err == nil {
		t.Fatal("expected error when slinging a molecule step bead, got nil")
	}
	if !strings.Contains(err.Error(), "formula molecule machinery") {
		t.Errorf("error should mention formula molecule machinery: %v", err)
	}
	if !strings.Contains(result.ErrMsg, "formula molecule machinery") {
		t.Errorf("expected ErrMsg to mention formula molecule machinery, got %q", result.ErrMsg)
	}
}

// TestExecuteSling_MoleculeStepBead_ForceDoesNotBypass verifies --force does not
// bypass the molecule guard — scaffolding is never dispatchable, with or without
// force (mirrors the closed/tombstone guard's no-override semantics).
func TestExecuteSling_MoleculeStepBead_ForceDoesNotBypass(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	townRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(townRoot, ".beads"), 0o755); err != nil {
		t.Fatalf("failed to create .beads: %v", err)
	}
	binDir := filepath.Join(townRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir binDir: %v", err)
	}
	writeBDStub(t, binDir, moleculeStepBeadBDStub, "")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := executeSling(SlingParams{
		BeadID:   "gt-step1",
		RigName:  "testrig",
		TownRoot: townRoot,
		Force:    true,
	})
	if err == nil {
		t.Fatal("expected --force to not bypass the molecule guard, got nil error")
	}
	if !strings.Contains(err.Error(), "formula molecule machinery") {
		t.Errorf("--force should not bypass molecule guard: %v", err)
	}
}
