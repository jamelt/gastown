package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/polecat"
)

func TestPolecatSessionSet(t *testing.T) {
	setupPolecatTestRegistry(t)
	sessions := newPolecatSessionSet([]string{
		"gt-thunder",
		"gt-crew-dom",
		"gp-mirelurk",
		"not-a-polecat",
	})

	if got, ok := sessions.lookup("gastown", "thunder"); !ok || got != "gt-thunder" {
		t.Fatalf("lookup gastown/thunder = %q, %v", got, ok)
	}
	if _, ok := sessions.lookup("gastown", "dom"); ok {
		t.Fatal("crew session should not be indexed as polecat")
	}
	if got := sessions.namesForRig("gastown"); len(got) != 1 || got[0] != "gt-thunder" {
		t.Fatalf("namesForRig(gastown) = %v", got)
	}
}

func TestBuildPolecatInventoryItem(t *testing.T) {
	setupPolecatTestRegistry(t)
	sessions := newPolecatSessionSet([]string{"gt-running"})
	tests := []struct {
		name         string
		polecatName  string
		fields       *beads.AgentFields
		activeWork   *beads.Issue
		wantState    polecat.State
		wantIssue    string
		wantVerdict  string
		wantReusable bool
		wantRecovery bool
		wantCapacity bool
	}{
		{
			name:         "clean idle reusable",
			polecatName:  "idle",
			fields:       &beads.AgentFields{AgentState: string(beads.AgentStateIdle), CleanupStatus: string(polecat.CleanupClean)},
			wantState:    polecat.StateIdle,
			wantVerdict:  polecat.WorkstateVerdictSafeToNuke,
			wantReusable: true,
		},
		{
			name:         "hooked running is working capacity",
			polecatName:  "running",
			fields:       &beads.AgentFields{AgentState: string(beads.AgentStateIdle), CleanupStatus: string(polecat.CleanupClean)},
			activeWork:   &beads.Issue{ID: "gt-hook", Status: string(beads.IssueStatusHooked), Assignee: "gastown/polecats/running"},
			wantState:    polecat.StateWorking,
			wantIssue:    "gt-hook",
			wantVerdict:  polecat.WorkstateVerdictWorking,
			wantCapacity: true,
		},
		{
			name:         "open stopped is stalled capacity",
			polecatName:  "stopped",
			fields:       &beads.AgentFields{AgentState: string(beads.AgentStateIdle), CleanupStatus: string(polecat.CleanupClean)},
			activeWork:   &beads.Issue{ID: "gt-open", Status: string(beads.StatusOpen), Assignee: "gastown/polecats/stopped"},
			wantState:    polecat.StateStalled,
			wantIssue:    "gt-open",
			wantVerdict:  polecat.WorkstateVerdictNeedsRecovery,
			wantRecovery: true,
			wantCapacity: true,
		},
		{
			name:         "deferred protects without capacity",
			polecatName:  "deferred",
			fields:       &beads.AgentFields{AgentState: string(beads.AgentStateIdle), CleanupStatus: string(polecat.CleanupClean)},
			activeWork:   &beads.Issue{ID: "gt-deferred", Status: string(beads.StatusDeferred), Assignee: "gastown/polecats/deferred"},
			wantState:    polecat.StateIdle,
			wantIssue:    "gt-deferred",
			wantVerdict:  polecat.WorkstateVerdictNeedsRecovery,
			wantRecovery: true,
		},
		{
			name:         "hook fallback protects without capacity",
			polecatName:  "hookonly",
			fields:       &beads.AgentFields{AgentState: string(beads.AgentStateIdle), CleanupStatus: string(polecat.CleanupClean), HookBead: "gt-old"},
			wantState:    polecat.StateIdle,
			wantVerdict:  polecat.WorkstateVerdictNeedsRecovery,
			wantRecovery: true,
		},
		{
			name:         "paused agent state protects without capacity",
			polecatName:  "paused",
			fields:       &beads.AgentFields{AgentState: string(beads.AgentStatePaused), CleanupStatus: string(polecat.CleanupClean)},
			wantState:    polecat.StateIdle,
			wantVerdict:  polecat.WorkstateVerdictNeedsRecovery,
			wantRecovery: true,
		},
		{
			name:        "active mr is pending non capacity",
			polecatName: "pendingmr",
			fields:      &beads.AgentFields{AgentState: string(beads.AgentStateIdle), CleanupStatus: string(polecat.CleanupClean), ActiveMR: "gt-mr"},
			wantState:   polecat.StateIdle,
			wantVerdict: polecat.WorkstateVerdictPendingMR,
		},
		{
			name:         "done without active mr and clean cleanup is reusable",
			polecatName:  "done",
			fields:       &beads.AgentFields{AgentState: string(beads.AgentStateDone), CleanupStatus: string(polecat.CleanupClean)},
			wantState:    polecat.StateDone,
			wantVerdict:  polecat.WorkstateVerdictSafeToNuke,
			wantReusable: true,
		},
		{
			name:         "done without active mr blocks reuse when cleanup is dirty",
			polecatName:  "donedirty",
			fields:       &beads.AgentFields{AgentState: string(beads.AgentStateDone), CleanupStatus: string(polecat.CleanupUnpushed)},
			wantState:    polecat.StateDone,
			wantVerdict:  polecat.WorkstateVerdictNeedsRecovery,
			wantRecovery: true,
			wantCapacity: true,
		},
		{
			name:        "done with active mr remains pending",
			polecatName: "donepending",
			fields:      &beads.AgentFields{AgentState: string(beads.AgentStateDone), CleanupStatus: string(polecat.CleanupClean), ActiveMR: "gt-mr"},
			wantState:   polecat.StateDone,
			wantVerdict: polecat.WorkstateVerdictPendingMR,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := buildPolecatInventoryItem("gastown", tt.polecatName, tt.fields, tt.activeWork, sessions)
			if item.State != tt.wantState || item.Issue != tt.wantIssue || item.Disposition.Verdict != tt.wantVerdict || item.Disposition.Reusable != tt.wantReusable || item.Disposition.NeedsRecovery != tt.wantRecovery || item.Disposition.CountsTowardCapacity != tt.wantCapacity {
				t.Fatalf("item = %+v disposition=%+v", item, item.Disposition)
			}
		})
	}
}

func TestBuildPolecatInventoryItemActiveWorkLookupErrorFailsClosed(t *testing.T) {
	item := buildPolecatInventoryItemFromEvidence(
		"gastown",
		"lookup",
		&beads.AgentFields{AgentState: string(beads.AgentStateIdle), CleanupStatus: string(polecat.CleanupClean)},
		polecatActiveWorkLookupError(errors.New("bd failed")),
		polecatSessionSet{},
	)

	if item.Disposition.Reusable || item.Disposition.SafeToNuke || !item.Disposition.NeedsRecovery || item.Disposition.CountsTowardCapacity {
		t.Fatalf("lookup error disposition = %+v", item.Disposition)
	}
	if item.Disposition.Reason != "active-work" {
		t.Fatalf("reason = %q, want active-work", item.Disposition.Reason)
	}
	if len(item.Disposition.Blockers) != 1 || !strings.Contains(item.Disposition.Blockers[0], "lookup_error") {
		t.Fatalf("blockers = %v, want lookup_error", item.Disposition.Blockers)
	}
}

func TestApplyLegacyCleanupCompatibilityUpgradeAndRestart(t *testing.T) {
	fields := &beads.AgentFields{AgentState: string(beads.AgentStateDone)}
	assessed := polecat.DecideWorkstate(polecat.WorkstateInput{
		State:         polecat.StateDone,
		CleanupStatus: polecat.CleanupClean,
		Branch:        "polecat/legacy/completed",
	})

	classify := func(sessions polecatSessionSet) polecatInventoryItem {
		item := buildPolecatInventoryItemFromEvidence("gastown", "legacy", fields, polecatActiveWorkEvidence{}, sessions)
		return applyLegacyCleanupCompatibility(item, fields, polecatActiveWorkEvidence{}, assessed)
	}

	// The legacy metadata-only classifier is fail-closed but no longer reserves
	// capacity solely because the optional field is absent.
	before := buildPolecatInventoryItemFromEvidence("gastown", "legacy", fields, polecatActiveWorkEvidence{}, nil)
	if !before.Disposition.NeedsRecovery || before.Disposition.CountsTowardCapacity {
		t.Fatalf("legacy pre-compat disposition = %+v", before.Disposition)
	}

	for _, phase := range []string{"after-upgrade", "after-daemon-restart"} {
		t.Run(phase, func(t *testing.T) {
			got := classify(newPolecatSessionSet(nil))
			if !got.Disposition.Reusable || got.Disposition.Verdict != polecat.WorkstateVerdictSafeToNuke {
				t.Fatalf("disposition = %+v, want reusable after read-only reconciliation", got.Disposition)
			}
			if got.CleanupProvenance != legacyCleanupReadOnlyProvenance {
				t.Fatalf("cleanup provenance = %q", got.CleanupProvenance)
			}
		})
	}
}

func TestApplyLegacyCleanupCompatibilityFailsClosed(t *testing.T) {
	legacyDone := &beads.AgentFields{AgentState: string(beads.AgentStateDone)}
	cleanAssessment := polecat.DecideWorkstate(polecat.WorkstateInput{State: polecat.StateDone, CleanupStatus: polecat.CleanupClean})
	tests := []struct {
		name       string
		fields     *beads.AgentFields
		work       polecatActiveWorkEvidence
		sessions   polecatSessionSet
		assessment polecat.WorkstateDisposition
	}{
		{name: "missing agent bead", fields: nil, assessment: cleanAssessment},
		{name: "live session", fields: legacyDone, sessions: polecatSessionSet{polecatSessionKey("gastown", "legacy"): "gt-legacy"}, assessment: cleanAssessment},
		{name: "active work", fields: legacyDone, work: polecatActiveWorkEvidence{BlocksCleanup: true, RequiresRestart: true, CountsTowardCapacity: true, Blocker: "assigned_work=gt-open status=open", AssignedIssue: "gt-open"}, assessment: cleanAssessment},
		{name: "hooked work evidence", fields: legacyDone, assessment: polecat.DecideWorkstate(polecat.WorkstateInput{State: polecat.StateDone, CleanupStatus: polecat.CleanupClean, HookBead: "gt-open"})},
		{name: "dirty worktree evidence", fields: legacyDone, assessment: polecat.DecideWorkstate(polecat.WorkstateInput{State: polecat.StateDone, CleanupStatus: "", GitDirty: true})},
		{name: "uncertain worktree evidence", fields: legacyDone, assessment: polecat.DecideWorkstate(polecat.WorkstateInput{State: polecat.StateDone, CleanupStatus: "", GitCheckFailed: true})},
		{name: "pending mr evidence", fields: legacyDone, assessment: polecat.DecideWorkstate(polecat.WorkstateInput{State: polecat.StateDone, CleanupStatus: polecat.CleanupClean, ActiveMRBlocker: "active_mr=gt-mr status=open"})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := buildPolecatInventoryItemFromEvidence("gastown", "legacy", tt.fields, tt.work, tt.sessions)
			got := applyLegacyCleanupCompatibility(item, tt.fields, tt.work, tt.assessment)
			if got.Disposition.Reusable {
				t.Fatalf("disposition unexpectedly reusable: %+v", got.Disposition)
			}
		})
	}
}

func TestPolecatSummaryIssueRankPrefersActiveWork(t *testing.T) {
	ordered := []*beads.Issue{
		{ID: "hook", Status: string(beads.IssueStatusHooked)},
		{ID: "progress", Status: string(beads.StatusInProgress)},
		{ID: "open", Status: string(beads.StatusOpen)},
		{ID: "blocked", Status: string(beads.StatusBlocked)},
		{ID: "deferred", Status: string(beads.StatusDeferred)},
	}
	for i := 1; i < len(ordered); i++ {
		if polecatSummaryIssueRank(ordered[i-1]) >= polecatSummaryIssueRank(ordered[i]) {
			t.Fatalf("rank(%s) should be before rank(%s)", ordered[i-1].Status, ordered[i].Status)
		}
	}
}

func TestPolecatNameFromAssignee(t *testing.T) {
	tests := []struct {
		assignee string
		wantName string
		wantOK   bool
	}{
		{assignee: "gastown/polecats/thunder", wantName: "thunder", wantOK: true},
		{assignee: "other/polecats/thunder"},
		{assignee: "gastown/crew/dom"},
		{assignee: "gastown/polecats/"},
		{assignee: "gastown/polecats/a/b"},
	}
	for _, tt := range tests {
		got, ok := polecatNameFromAssignee("gastown", tt.assignee)
		if got != tt.wantName || ok != tt.wantOK {
			t.Fatalf("polecatNameFromAssignee(%q) = %q, %v", tt.assignee, got, ok)
		}
	}
}
