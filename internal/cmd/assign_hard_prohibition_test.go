package cmd

import (
	"os"
	"strings"
	"testing"
)

// TestRunAssignChecksHardProhibition guards against a regression of gt-kzr8:
// gt assign created and hooked a bead to a crew member with no hq-1s4w
// hard-prohibition check at all, unlike every other single-bead dispatch
// entry point (sling.go, sling_schedule.go). This is a source-inspection
// test, mirroring TestRunSlingHardProhibitionRollsBackDogAssignment in
// sling_closed_guard_test.go: exercising runAssign end-to-end would require
// stubbing out bd and crew directories for little additional assurance,
// since checkHardProhibition's own logic is already fully covered by
// TestCheckHardProhibition in scheduler_feed_test.go. What matters here is
// that runAssign actually calls the gate, and calls it early enough to
// cover both the real path and --dry-run.
func TestRunAssignChecksHardProhibition(t *testing.T) {
	data, err := os.ReadFile("assign.go")
	if err != nil {
		t.Fatalf("read assign.go: %v", err)
	}
	source := string(data)

	guardCall := "checkHardProhibition(title, assignDescription, assignLabels, false)"
	guardIdx := strings.Index(source, guardCall)
	if guardIdx == -1 {
		t.Fatal("runAssign must call checkHardProhibition(title, assignDescription, assignLabels, false) before hooking a bead to a crew member")
	}

	dryRunIdx := strings.Index(source, "if assignDryRun {")
	if dryRunIdx == -1 {
		t.Fatal("could not find assignDryRun branch in assign.go")
	}
	if guardIdx > dryRunIdx {
		t.Fatal("hard-prohibition guard must run before the --dry-run branch, so --dry-run surfaces a rejection instead of silently previewing a blocked dispatch")
	}

	createIdx := strings.Index(source, `createArgs := []string{"create",`)
	if createIdx == -1 {
		t.Fatal("could not find bead creation in assign.go")
	}
	if guardIdx > createIdx {
		t.Fatal("hard-prohibition guard must run before bd create, so a rejected bead is never created in the first place")
	}
}
