package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestExecuteSling_ClosedBead verifies that executeSling rejects closed beads.
func TestExecuteSling_ClosedBead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	townRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(townRoot, ".beads"), 0o755); err != nil {
		t.Fatalf("failed to create .beads: %v", err)
	}

	// Create bd stub that returns status:"closed"
	binDir := filepath.Join(townRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir binDir: %v", err)
	}
	bdScript := `#!/bin/sh
case "$1" in
  show)
    echo '[{"title":"Done task","status":"closed","assignee":"","description":""}]'
    ;;
esac
exit 0
`
	writeBDStub(t, binDir, bdScript, "")

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	params := SlingParams{
		BeadID:   "test-closed1",
		RigName:  "testrig",
		TownRoot: townRoot,
	}

	result, err := executeSling(params)
	if err == nil {
		t.Fatal("expected error when slinging closed bead, got nil")
	}

	if result.ErrMsg != "already closed" {
		t.Errorf("expected ErrMsg='already closed', got %q", result.ErrMsg)
	}

	if !strings.Contains(err.Error(), "closed") || !strings.Contains(err.Error(), "work already completed") {
		t.Errorf("error should mention closed status: %v", err)
	}
}

// TestExecuteSling_TombstoneBead verifies that executeSling rejects tombstone beads.
func TestExecuteSling_TombstoneBead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	townRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(townRoot, ".beads"), 0o755); err != nil {
		t.Fatalf("failed to create .beads: %v", err)
	}

	// Create bd stub that returns status:"tombstone"
	binDir := filepath.Join(townRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir binDir: %v", err)
	}
	bdScript := `#!/bin/sh
case "$1" in
  show)
    echo '[{"title":"Tombstoned task","status":"tombstone","assignee":"","description":""}]'
    ;;
esac
exit 0
`
	writeBDStub(t, binDir, bdScript, "")

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	params := SlingParams{
		BeadID:   "test-tomb1",
		RigName:  "testrig",
		TownRoot: townRoot,
	}

	result, err := executeSling(params)
	if err == nil {
		t.Fatal("expected error when slinging tombstone bead, got nil")
	}

	if result.ErrMsg != "already tombstone" {
		t.Errorf("expected ErrMsg='already tombstone', got %q", result.ErrMsg)
	}

	if !strings.Contains(err.Error(), "tombstone") || !strings.Contains(err.Error(), "work already completed") {
		t.Errorf("error should mention tombstone status: %v", err)
	}
}

// TestExecuteSling_ClosedBead_ForceDoesNotBypass verifies that --force does NOT
// bypass the closed bead guard. To re-dispatch, the bead must be reopened first.
func TestExecuteSling_ClosedBead_ForceDoesNotBypass(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}

	townRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(townRoot, ".beads"), 0o755); err != nil {
		t.Fatalf("failed to create .beads: %v", err)
	}

	// Create bd stub that returns status:"closed"
	binDir := filepath.Join(townRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir binDir: %v", err)
	}
	bdScript := `#!/bin/sh
case "$1" in
  show)
    echo '[{"title":"Done task","status":"closed","assignee":"","description":""}]'
    ;;
esac
exit 0
`
	writeBDStub(t, binDir, bdScript, "")

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	params := SlingParams{
		BeadID:   "test-closed2",
		RigName:  "testrig",
		TownRoot: townRoot,
		Force:    true, // --force should NOT bypass closed guard
	}

	_, err := executeSling(params)
	if err == nil {
		t.Fatal("expected error when slinging closed bead with --force, got nil")
	}

	if !strings.Contains(err.Error(), "closed") || !strings.Contains(err.Error(), "work already completed") {
		t.Errorf("--force should not bypass closed guard: %v", err)
	}
}

// TestExecuteSling_HardProhibition verifies that executeSling rejects
// hq-1s4w hard-prohibition-labeled beads (gt-b2qi) — batch, convoy, epic
// sling, and queue dispatch all funnel through executeSling, so this is the
// one call site with no --confirm-human-approved override.
func TestExecuteSling_HardProhibition(t *testing.T) {
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
	bdScript := `#!/bin/sh
case "$1" in
  show)
    echo '[{"title":"Credentials bead","status":"open","assignee":"","description":"","labels":["human","area:security"]}]'
    ;;
esac
exit 0
`
	writeBDStub(t, binDir, bdScript, "")

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	params := SlingParams{
		BeadID:   "test-hardprohib1",
		RigName:  "testrig",
		TownRoot: townRoot,
	}

	result, err := executeSling(params)
	if err == nil {
		t.Fatal("expected error when slinging an hq-1s4w hard-prohibition-labeled bead, got nil")
	}

	if !strings.Contains(err.Error(), "fresh human approval") {
		t.Errorf("error should mention fresh human approval: %v", err)
	}
	if !strings.Contains(err.Error(), "--confirm-human-approved") {
		t.Errorf("error should point at the single-bead recovery path: %v", err)
	}
	if !strings.Contains(result.ErrMsg, "hard-prohibition") {
		t.Errorf("expected ErrMsg to mention hard-prohibition, got %q", result.ErrMsg)
	}
}

// TestExecuteSling_HardProhibition_ForceDoesNotBypass verifies that --force
// does NOT bypass the hard-prohibition guard — only a human running
// single-bead gt sling with --confirm-human-approved can (and executeSling
// itself never accepts that override at all; see the call site in
// sling_dispatch.go).
func TestExecuteSling_HardProhibition_ForceDoesNotBypass(t *testing.T) {
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
	bdScript := `#!/bin/sh
case "$1" in
  show)
    echo '[{"title":"Money-policy bead","status":"open","assignee":"","description":"","labels":["risk:money"]}]'
    ;;
esac
exit 0
`
	writeBDStub(t, binDir, bdScript, "")

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	params := SlingParams{
		BeadID:   "test-hardprohib2",
		RigName:  "testrig",
		TownRoot: townRoot,
		Force:    true,
	}

	_, err := executeSling(params)
	if err == nil {
		t.Fatal("expected --force to not bypass the hard-prohibition guard, got nil error")
	}
	if !strings.Contains(err.Error(), "fresh human approval") {
		t.Errorf("--force should not bypass hard-prohibition guard: %v", err)
	}
}

// TestRunSlingDirectDispatch_HardProhibition verifies that runSling's own
// direct-dispatch path (which never calls executeSling or scheduleBead — see
// the TODO in sling.go) also rejects hq-1s4w hard-prohibition-labeled beads,
// and that the hook is never attempted. --confirm-human-approved lets it
// through.
func TestRunSlingDirectDispatch_HardProhibition(t *testing.T) {
	townRoot, _, descPath := setupMutableBDRawSlingTest(t, "Keep this body.")

	binDir := filepath.Join(townRoot, "bin")
	bdScript := `#!/bin/sh
set -eu
if [ "${1:-}" = "--allow-stale" ] && [ "${2:-}" = "version" ]; then
  echo "bd test"
  exit 0
fi
while [ "$#" -gt 0 ]; do
  case "$1" in
    --allow-stale) shift ;;
    *) break ;;
  esac
done
cmd="${1:-}"
if [ "$#" -gt 0 ]; then shift; fi
case "$cmd" in
  show)
    desc=""
    if [ -f "$BD_DESC_FILE" ]; then
      desc=$(awk 'BEGIN{first=1} {gsub(/\\/,"\\\\"); gsub(/"/,"\\\""); if(!first){printf "\\n"} printf "%s",$0; first=0}' "$BD_DESC_FILE")
    fi
    status="open"
    if [ -f "$BD_STATUS_FILE" ]; then status=$(cat "$BD_STATUS_FILE"); fi
    assignee=""
    if [ -f "$BD_ASSIGNEE_FILE" ]; then assignee=$(cat "$BD_ASSIGNEE_FILE"); fi
    printf '[{"id":"gt-rawrollback","title":"Credentials bead","status":"%s","assignee":"%s","description":"%s","labels":["human","area:security"],"dependencies":[]}]\n' "$status" "$assignee" "$desc"
    ;;
  update)
    for arg in "$@"; do
      case "$arg" in
        --description=*) printf "%s" "${arg#--description=}" > "$BD_DESC_FILE" ;;
        --status=*) printf "%s" "${arg#--status=}" > "$BD_STATUS_FILE" ;;
        --assignee=*) printf "%s" "${arg#--assignee=}" > "$BD_ASSIGNEE_FILE" ;;
      esac
    done
    ;;
  version)
    echo "bd test"
    ;;
esac
exit 0
`
	writeBDStub(t, binDir, bdScript, "")

	prevHookRaw := slingHookRawBead
	prevNoMerge := slingNoMerge
	prevReviewOnly := slingReviewOnly
	prevNoConvoy := slingNoConvoy
	prevDryRun := slingDryRun
	prevConfirm := slingConfirmHumanApproved
	prevResolve := resolveTargetAgentFn
	prevHook := hookBeadWithRetryFn
	t.Cleanup(func() {
		slingHookRawBead = prevHookRaw
		slingNoMerge = prevNoMerge
		slingReviewOnly = prevReviewOnly
		slingNoConvoy = prevNoConvoy
		slingDryRun = prevDryRun
		slingConfirmHumanApproved = prevConfirm
		resolveTargetAgentFn = prevResolve
		hookBeadWithRetryFn = prevHook
	})
	slingHookRawBead = true
	slingNoMerge = true
	slingReviewOnly = true
	slingNoConvoy = true
	slingDryRun = false
	resolveTargetAgentFn = func(target string) (string, string, string, error) {
		return "gastown/polecats/toast", "", filepath.Join(townRoot, "gastown", "polecats", "toast"), nil
	}
	hookCalled := false
	hookBeadWithRetryFn = func(beadID, targetAgent, hookDir string) error {
		hookCalled = true
		return nil
	}

	// Without --confirm-human-approved: rejected, hook never attempted.
	slingConfirmHumanApproved = false
	err := runSling(nil, []string{"gt-rawrollback", "gastown/polecats/toast"})
	if err == nil {
		t.Fatal("expected runSling to reject an hq-1s4w hard-prohibition-labeled bead")
	}
	if !strings.Contains(err.Error(), "fresh human approval") {
		t.Errorf("error should mention fresh human approval: %v", err)
	}
	if hookCalled {
		t.Fatal("hookBeadWithRetryFn must not be called when the hard-prohibition guard rejects the bead")
	}
	desc := readMutableBDDescription(t, descPath)
	if !strings.Contains(desc, "Keep this body.") {
		t.Fatalf("rejection must not mutate bead description:\n%s", desc)
	}

	// With --confirm-human-approved: allowed through to the hook.
	slingConfirmHumanApproved = true
	if err := runSling(nil, []string{"gt-rawrollback", "gastown/polecats/toast"}); err != nil {
		t.Fatalf("expected --confirm-human-approved to allow dispatch, got: %v", err)
	}
	if !hookCalled {
		t.Fatal("expected hookBeadWithRetryFn to be called once --confirm-human-approved is set")
	}
}
