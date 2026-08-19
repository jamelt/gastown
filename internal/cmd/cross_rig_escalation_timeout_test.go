package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// writeHangingGTStub writes a fake `gt` that hangs on `gt escalate ...` and
// responds instantly to everything else. Mirrors the writeHangOnAgentQueryBDStub
// pattern from polecat_capacity_inventory_timeout_test.go (gt-5p9), applied to
// gt-vyik's second unbounded call site: fireCrossRigEscalation's exec.Command
// invocation of `gt escalate`, which previously had no timeout at all and
// could block the whole dispatch cycle (it runs from Validate, before Execute).
func writeHangingGTStub(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix shell stub")
	}
	scriptPath := filepath.Join(dir, "gt")
	script := `#!/bin/sh
case "$*" in
  escalate*)
    exec sleep 300
    ;;
  *)
    exit 0
    ;;
esac
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake gt: %v", err)
	}
}

// TestFireCrossRigEscalationBoundedOnHang reproduces the second half of
// gt-vyik: before the fix, fireCrossRigEscalation ran `gt escalate` via a
// bare exec.Command(...).Run() with no timeout, and `gt escalate` can itself
// block on unbounded SMTP/HTTP delivery. Because this call happens inside
// Validate — which runs before Execute in DispatchCycle.RunPlan's per-bead
// loop — a hang here would wedge the whole dispatch cycle before any bead's
// actual dispatch even starts.
func TestFireCrossRigEscalationBoundedOnHang(t *testing.T) {
	binDir := t.TempDir()
	writeHangingGTStub(t, binDir)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GT_BD_TIMEOUT_SEC", "1")

	start := time.Now()
	fireCrossRigEscalation("dotfiles", "do", "do-99a")
	elapsed := time.Since(start)

	if elapsed > 10*time.Second {
		t.Fatalf("fireCrossRigEscalation took %v, want bounded by the ~1s GT_BD_TIMEOUT_SEC override, not the fake gt's 300s hang", elapsed)
	}
}
