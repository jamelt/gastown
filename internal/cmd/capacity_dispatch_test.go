package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/scheduler/capacity"
)

func TestShouldFireCrossRigEscalation_Debounces(t *testing.T) {
	resetCrossRigEscalationStateForTest()
	t.Cleanup(resetCrossRigEscalationStateForTest)

	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	if !shouldFireCrossRigEscalation("walletui", "hq", now) {
		t.Fatalf("first call must fire")
	}
	// Second call inside the debounce window must NOT fire.
	if shouldFireCrossRigEscalation("walletui", "hq", now.Add(30*time.Minute)) {
		t.Fatalf("second call inside debounce window must not fire")
	}
	// After the debounce window elapses, fire again.
	if !shouldFireCrossRigEscalation("walletui", "hq", now.Add(crossRigEscalationDebounce+time.Minute)) {
		t.Fatalf("call past debounce window must fire")
	}
}

func TestShouldFireCrossRigEscalation_KeyedByRigAndPrefix(t *testing.T) {
	resetCrossRigEscalationStateForTest()
	t.Cleanup(resetCrossRigEscalationStateForTest)

	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)

	if !shouldFireCrossRigEscalation("walletui", "hq", now) {
		t.Fatalf("walletui/hq first call must fire")
	}
	// Different rig — should fire independently.
	if !shouldFireCrossRigEscalation("furiosa", "hq", now) {
		t.Fatalf("furiosa/hq must fire (different rig)")
	}
	// Different prefix on same rig — should fire independently.
	if !shouldFireCrossRigEscalation("walletui", "wisp", now) {
		t.Fatalf("walletui/wisp must fire (different prefix)")
	}
	// Same (rig, prefix) repeats — debounced.
	if shouldFireCrossRigEscalation("walletui", "hq", now.Add(time.Minute)) {
		t.Fatalf("walletui/hq repeat must not fire")
	}
}

func TestDispatchSingleBeadRawReviewOnlyHookFailureClearsMetadata(t *testing.T) {
	townRoot, _, descPath := setupMutableBDRawSlingTest(t, "Keep this body.")

	prevSpawn := spawnPolecatForSling
	prevResolve := resolveTargetAgentFn
	prevHook := hookBeadWithRetryWithTownRootFn
	t.Cleanup(func() {
		spawnPolecatForSling = prevSpawn
		resolveTargetAgentFn = prevResolve
		hookBeadWithRetryWithTownRootFn = prevHook
	})
	spawnPolecatForSling = func(rigName string, opts SlingSpawnOptions) (*SpawnedPolecatInfo, error) {
		return &SpawnedPolecatInfo{
			RigName:     rigName,
			PolecatName: "toast",
			ClonePath:   filepath.Join(townRoot, "gastown", "polecats", "toast"),
		}, nil
	}
	hookBeadWithRetryWithTownRootFn = func(beadID, targetAgent, hookDir, townRoot string) error {
		desc := readMutableBDDescription(t, descPath)
		assertHasRawReviewMetadata(t, desc)
		receipt := beads.ParseAttachmentFields(&beads.Issue{Description: desc})
		if receipt == nil || receipt.DispatchContext != "gt-context" || receipt.DispatchedBy != "test" {
			t.Fatalf("worker cold-start saw incomplete scheduler receipt: %+v", receipt)
		}
		return errors.New("forced hook failure")
	}

	_, err := dispatchSingleBead(capacity.PendingBead{
		ID:         "gt-context",
		WorkBeadID: "gt-rawrollback",
		TargetRig:  "gastown",
		Context: &capacity.SlingContextFields{
			WorkBeadID:  "gt-rawrollback",
			TargetRig:   "gastown",
			HookRawBead: true,
			NoMerge:     true,
			ReviewOnly:  true,
		},
	}, townRoot, "test")
	if err == nil {
		t.Fatal("expected scheduler dispatch hook failure")
	}
	assertNoRawReviewMetadata(t, readMutableBDDescription(t, descPath))
}

func TestDispatchSingleBeadHonorsExactPolecatReservation(t *testing.T) {
	townRoot, _, _ := setupMutableBDRawSlingTest(t, "Keep this body.")

	prevSpawn := spawnPolecatForSling
	prevResolve := resolveTargetAgentFn
	prevVerifyWorktree := verifyDeferredTargetWorktreeFn
	prevBranch := deferredTargetBranchFn
	prevAssignment := deferredTargetAssignmentFn
	t.Cleanup(func() {
		spawnPolecatForSling = prevSpawn
		resolveTargetAgentFn = prevResolve
		verifyDeferredTargetWorktreeFn = prevVerifyWorktree
		deferredTargetBranchFn = prevBranch
		deferredTargetAssignmentFn = prevAssignment
	})
	spawnCalled := false
	resolveTargetAgentFn = func(string) (string, string, string, error) {
		return "gastown/polecats/toast", "", filepath.Join(townRoot, "gastown", "polecats", "toast", "gastown"), nil
	}
	spawnPolecatForSling = func(string, SlingSpawnOptions) (*SpawnedPolecatInfo, error) {
		spawnCalled = true
		return nil, errors.New("exact reservation must not spawn")
	}
	verifyDeferredTargetWorktreeFn = func(string) error { return nil }
	deferredTargetBranchFn = func(string) (string, error) { return "preserved-branch", nil }
	deferredTargetAssignmentFn = func(*beads.Beads, string) (*beads.Issue, error) { return nil, nil }

	result, err := dispatchSingleBead(capacity.PendingBead{
		ID:         "gt-context",
		WorkBeadID: "gt-rawrollback",
		TargetRig:  "gastown",
		Context: &capacity.SlingContextFields{
			WorkBeadID:   "gt-rawrollback",
			TargetRig:    "gastown",
			TargetAgent:  "gastown/polecats/toast",
			ResumeBranch: "preserved-branch",
			HookRawBead:  true,
		},
	}, townRoot, "test")
	if err != nil {
		t.Fatalf("dispatchSingleBead exact reservation: %v", err)
	}
	if spawnCalled {
		t.Fatal("exact reservation invoked polecat spawn")
	}
	if result == nil || !result.Success || result.PolecatName != "toast" || result.SpawnInfo != nil {
		t.Fatalf("result = %#v, want successful reuse of toast without SpawnInfo", result)
	}
}

func TestScheduleBeadExplicitTargetBranchMismatchFailsBeforeSideEffects(t *testing.T) {
	townRoot, _, descPath := setupMutableBDRawSlingTest(t, "Original description.")

	prevResolve := resolveTargetAgentFn
	prevVerifyWorktree := verifyDeferredTargetWorktreeFn
	prevBranch := deferredTargetBranchFn
	prevAssignment := deferredTargetAssignmentFn
	t.Cleanup(func() {
		resolveTargetAgentFn = prevResolve
		verifyDeferredTargetWorktreeFn = prevVerifyWorktree
		deferredTargetBranchFn = prevBranch
		deferredTargetAssignmentFn = prevAssignment
	})
	resolveTargetAgentFn = func(string) (string, string, string, error) {
		return "gastown/polecats/toast", "", filepath.Join(townRoot, "gastown", "polecats", "toast", "gastown"), nil
	}
	verifyDeferredTargetWorktreeFn = func(string) error { return nil }
	deferredTargetBranchFn = func(string) (string, error) { return "other-branch", nil }
	deferredTargetAssignmentFn = func(*beads.Beads, string) (*beads.Issue, error) {
		t.Fatal("assignment lookup must not run after branch mismatch")
		return nil, nil
	}

	err := scheduleBead("gt-rawrollback", "gastown", ScheduleOptions{
		TargetAgent:  "gastown/polecats/toast",
		ResumeBranch: "preserved-branch",
		Force:        true,
	})
	if err == nil || !strings.Contains(err.Error(), "refusing to create a duplicate checkout") {
		t.Fatalf("scheduleBead error = %v, want fail-closed branch mismatch", err)
	}
	if got := readMutableBDDescription(t, descPath); got != "Original description." {
		t.Fatalf("description mutated before validation: %q", got)
	}
}

func TestListBlockedWorkBeadIDStatesPartialFailureFailsClosedPerGroup(t *testing.T) {
	townRoot := t.TempDir()
	townBeadsDir := filepath.Join(townRoot, ".beads")
	if err := os.MkdirAll(townBeadsDir, 0o755); err != nil {
		t.Fatalf("mkdir town beads: %v", err)
	}
	routes := []beads.Route{
		{Prefix: "a-", Path: "rig-a"},
		{Prefix: "b-", Path: "rig-b"},
	}
	if err := beads.WriteRoutes(townBeadsDir, routes); err != nil {
		t.Fatalf("write routes: %v", err)
	}

	blocked, unknown, err := listBlockedWorkBeadIDStatesWithRunner(townRoot, []string{"a-ready", "b-ready", "b-other"}, func(beadsDir string, groupedIDs []string) ([]byte, error) {
		switch groupedIDs[0][:1] {
		case "a":
			return []byte(`[{"id":"a-ready"}]`), nil
		case "b":
			return nil, fmt.Errorf("blocked query failed")
		default:
			return nil, fmt.Errorf("unexpected group %s", beadsDir)
		}
	})
	if err != nil {
		t.Fatalf("partial blocked query failure returned error: %v", err)
	}
	if !blocked["a-ready"] {
		t.Fatalf("a-ready should be marked blocked from successful group")
	}
	if unknown["a-ready"] {
		t.Fatalf("a-ready should not be blocked-unknown")
	}
	if !unknown["b-ready"] || !unknown["b-other"] {
		t.Fatalf("failed group IDs should be blocked-unknown, got %#v", unknown)
	}

	_, unknown, err = listBlockedWorkBeadIDStatesWithRunner(townRoot, []string{"a-ready", "b-ready"}, func(string, []string) ([]byte, error) {
		return []byte(`not-json`), nil
	})
	if err == nil {
		t.Fatalf("all blocked query JSON failures should return an error")
	}
	if !unknown["a-ready"] || !unknown["b-ready"] {
		t.Fatalf("all failed groups should mark every ID blocked-unknown, got %#v", unknown)
	}
}

func TestIsScheduledWorkBeadReadyFailsClosedForBlockedUnknown(t *testing.T) {
	info := beadStatusInfo{Status: "open"}
	if isScheduledWorkBeadReady("gt-ready", info, true, nil, map[string]bool{"gt-ready": true}) {
		t.Fatalf("blocked-unknown source must not be scheduler-ready")
	}
}
