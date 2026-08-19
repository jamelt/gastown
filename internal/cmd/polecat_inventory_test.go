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
			item := buildPolecatInventoryItem("gastown", tt.polecatName, tt.fields, tt.activeWork, sessions, polecatMQIndex{})
			if item.State != tt.wantState || item.Issue != tt.wantIssue || item.Disposition.Verdict != tt.wantVerdict || item.Disposition.Reusable != tt.wantReusable || item.Disposition.NeedsRecovery != tt.wantRecovery || item.Disposition.CountsTowardCapacity != tt.wantCapacity {
				t.Fatalf("item = %+v disposition=%+v", item, item.Disposition)
			}
		})
	}
}

// TestBuildPolecatInventoryItemMQIndex proves gt-h6u4's fix: a polecat with an
// unsubmitted branch is no longer reported reusable by list/capacity just
// because cleanup_status is clean. Before the fix, buildPolecatInventoryItem
// never set WorkstateInput.MQCheckRequired, so NEEDS_MQ_SUBMIT was
// structurally unreachable regardless of the branch/MR state below.
func TestBuildPolecatInventoryItemMQIndex(t *testing.T) {
	setupPolecatTestRegistry(t)
	sessions := polecatSessionSet{}
	branch := "polecat/synth/gt-work+abc123"

	t.Run("unsubmitted branch is not reusable", func(t *testing.T) {
		fields := &beads.AgentFields{
			AgentState:    string(beads.AgentStateIdle),
			CleanupStatus: string(polecat.CleanupClean),
			Branch:        branch,
		}
		mq := polecatMQIndex{mrByBranch: map[string]*beads.Issue{}, sourceIssues: map[string]*beads.Issue{}}

		item := buildPolecatInventoryItem("gastown", "synth", fields, nil, sessions, mq)

		if item.Disposition.Reusable {
			t.Fatalf("disposition = %+v, want Reusable=false for unsubmitted branch", item.Disposition)
		}
		if item.Disposition.Verdict != polecat.WorkstateVerdictNeedsMQSubmit {
			t.Fatalf("verdict = %q, want %q", item.Disposition.Verdict, polecat.WorkstateVerdictNeedsMQSubmit)
		}
	})

	t.Run("submitted branch remains reusable", func(t *testing.T) {
		fields := &beads.AgentFields{
			AgentState:    string(beads.AgentStateIdle),
			CleanupStatus: string(polecat.CleanupClean),
			Branch:        branch,
		}
		mrIssue := &beads.Issue{ID: "gt-mr-1", Status: string(beads.StatusOpen), Description: beads.FormatMRFields(&beads.MRFields{Branch: branch})}
		mq := polecatMQIndex{
			mrByBranch:   map[string]*beads.Issue{branch: mrIssue},
			sourceIssues: map[string]*beads.Issue{},
		}

		item := buildPolecatInventoryItem("gastown", "synth", fields, nil, sessions, mq)

		if !item.Disposition.Reusable {
			t.Fatalf("disposition = %+v, want Reusable=true once an MR exists for the branch", item.Disposition)
		}
	})

	t.Run("mq lookup failure fails closed", func(t *testing.T) {
		fields := &beads.AgentFields{
			AgentState:    string(beads.AgentStateIdle),
			CleanupStatus: string(polecat.CleanupClean),
			Branch:        branch,
		}
		mq := polecatMQIndex{lookupFailed: true}

		item := buildPolecatInventoryItem("gastown", "synth", fields, nil, sessions, mq)

		if item.Disposition.Reusable {
			t.Fatalf("disposition = %+v, want Reusable=false on mq lookup failure", item.Disposition)
		}
		if item.Disposition.Reason != "mq-lookup-failed" {
			t.Fatalf("reason = %q, want mq-lookup-failed", item.Disposition.Reason)
		}
	})
}

type fakePolecatMQIndexSource struct {
	mrs        []*beads.Issue
	mrErr      error
	issues     map[string]*beads.Issue
	showErr    error
	gotShowIDs []string
}

func (f *fakePolecatMQIndexSource) ListMergeRequests(beads.ListOptions) ([]*beads.Issue, error) {
	return f.mrs, f.mrErr
}

func (f *fakePolecatMQIndexSource) ShowMultiple(ids []string) (map[string]*beads.Issue, error) {
	f.gotShowIDs = ids
	if f.showErr != nil {
		return nil, f.showErr
	}
	return f.issues, nil
}

// TestBuildPolecatMQIndex proves the batching this fix relies on: the newest
// MR for a branch wins the dedup, and source-issue terminal/attachment state
// is threaded through via one bulk ShowMultiple call rather than a per-polecat
// bd.Show (gt-h6u4).
func TestBuildPolecatMQIndex(t *testing.T) {
	t.Run("newest MR for a branch wins", func(t *testing.T) {
		older := &beads.Issue{ID: "gt-mr-old", CreatedAt: "2026-01-01T00:00:00Z", Description: beads.FormatMRFields(&beads.MRFields{Branch: "polecat/synth/gt-work"})}
		newer := &beads.Issue{ID: "gt-mr-new", CreatedAt: "2026-02-01T00:00:00Z", Description: beads.FormatMRFields(&beads.MRFields{Branch: "polecat/synth/gt-work"})}
		src := &fakePolecatMQIndexSource{mrs: []*beads.Issue{older, newer}, issues: map[string]*beads.Issue{}}

		idx := buildPolecatMQIndex(src, nil)

		got := idx.mrByBranch["polecat/synth/gt-work"]
		if got == nil || got.ID != "gt-mr-new" {
			t.Fatalf("mrByBranch[...] = %v, want gt-mr-new", got)
		}
	})

	t.Run("source issue terminal and no-merge flow through", func(t *testing.T) {
		fields := &beads.AgentFields{LastSourceIssue: "gt-src-1"}
		sourceIssue := &beads.Issue{
			ID:          "gt-src-1",
			Status:      string(beads.StatusClosed),
			Description: "no_merge: true\n",
		}
		src := &fakePolecatMQIndexSource{
			mrs:    nil,
			issues: map[string]*beads.Issue{"gt-src-1": sourceIssue},
		}

		idx := buildPolecatMQIndex(src, map[string]*beads.AgentFields{"synth": fields})

		if len(src.gotShowIDs) != 1 || src.gotShowIDs[0] != "gt-src-1" {
			t.Fatalf("ShowMultiple called with %v, want [gt-src-1]", src.gotShowIDs)
		}

		input := &polecat.WorkstateInput{Branch: "polecat/synth/gt-work"}
		applyMQIndexToWorkstateInput(input, fields, idx)
		if !input.AssignedBeadTerminal {
			t.Error("AssignedBeadTerminal = false, want true for a closed source issue")
		}
		if !input.MQNotRequired {
			t.Error("MQNotRequired = false, want true for a no_merge source issue")
		}
	})

	t.Run("lookup errors mark the index failed", func(t *testing.T) {
		src := &fakePolecatMQIndexSource{mrErr: errors.New("bd list failed")}
		idx := buildPolecatMQIndex(src, nil)
		if !idx.lookupFailed {
			t.Error("lookupFailed = false, want true when ListMergeRequests errors")
		}
	})
}

func TestBuildPolecatInventoryItemActiveWorkLookupErrorFailsClosed(t *testing.T) {
	item := buildPolecatInventoryItemFromEvidence(
		"gastown",
		"lookup",
		&beads.AgentFields{AgentState: string(beads.AgentStateIdle), CleanupStatus: string(polecat.CleanupClean)},
		polecatActiveWorkLookupError(errors.New("bd failed")),
		polecatSessionSet{},
		polecatMQIndex{},
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
