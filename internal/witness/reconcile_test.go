package witness

import (
	"fmt"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

// fakeMRFinder is a minimal mrFinder test double keyed by branch name.
type fakeMRFinder struct {
	byBranch map[string]*beads.Issue
	err      error
}

func (f *fakeMRFinder) FindMRForBranchAny(branch string) (*beads.Issue, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byBranch[branch], nil
}

func mergedMR(id string) *beads.Issue {
	return &beads.Issue{ID: id, Status: "closed", Description: "close_reason: merged\n"}
}

func rejectedMR(id string) *beads.Issue {
	return &beads.Issue{ID: id, Status: "closed", Description: "close_reason: rejected\n"}
}

func openMR(id string) *beads.Issue {
	return &beads.Issue{ID: id, Status: "open", Description: ""}
}

// --- reconcileWispAction: pure decision logic, the 5 regression scenarios ---

func TestReconcileWispAction_MergedMRCloses(t *testing.T) {
	t.Parallel()
	if got := reconcileWispAction(mergedMR("gt-mr-1")); got != reconcileClose {
		t.Errorf("reconcileWispAction(merged) = %v, want reconcileClose", got)
	}
}

func TestReconcileWispAction_RejectedMRPreserves(t *testing.T) {
	t.Parallel()
	if got := reconcileWispAction(rejectedMR("gt-mr-2")); got != reconcilePreserve {
		t.Errorf("reconcileWispAction(rejected) = %v, want reconcilePreserve", got)
	}
}

func TestReconcileWispAction_PendingMRPreserves(t *testing.T) {
	t.Parallel()
	if got := reconcileWispAction(openMR("gt-mr-3")); got != reconcilePreserve {
		t.Errorf("reconcileWispAction(open/pending) = %v, want reconcilePreserve", got)
	}
}

func TestReconcileWispAction_NoCorrelatedMRSurfacesForAudit(t *testing.T) {
	t.Parallel()
	// No MR bead found at all (e.g. missing/never-created record) must never
	// be treated as proof of merge, and must never be silently destroyed.
	if got := reconcileWispAction(nil); got != reconcileAudit {
		t.Errorf("reconcileWispAction(nil) = %v, want reconcileAudit", got)
	}
}

func TestReconcileWispAction_ConflictAndSupersededPreserve(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"conflict", "superseded"} {
		mr := &beads.Issue{ID: "gt-mr-x", Status: "closed", Description: "close_reason: " + reason + "\n"}
		if got := reconcileWispAction(mr); got != reconcilePreserve {
			t.Errorf("reconcileWispAction(close_reason=%s) = %v, want reconcilePreserve", reason, got)
		}
	}
}

// --- ReconcileMergeRequestedWisps: end-to-end sweep behavior ---

func wispShowJSON(t *testing.T, id, branch string) string {
	t.Helper()
	desc := fmt.Sprintf("Verify and cleanup polecat nux\nIssue: gt-e95\nBranch: %s", branch)
	return fmt.Sprintf(`[{"id":%q,"description":%q}]`, id, desc)
}

func TestReconcileMergeRequestedWisps_MergedClosesTracker(t *testing.T) {
	t.Parallel()
	bd, mock := mockBd(
		func(args []string) (string, error) {
			switch {
			case len(args) > 0 && args[0] == "list":
				return `[{"id":"gt-wisp-1"}]`, nil
			case len(args) > 0 && args[0] == "show":
				return wispShowJSON(t, "gt-wisp-1", "polecat/nux/gt-e95+abc"), nil
			case len(args) > 0 && args[0] == "close":
				return "{}", nil
			}
			return "{}", nil
		},
		func(args []string) error { return nil },
	)
	finder := &fakeMRFinder{byBranch: map[string]*beads.Issue{
		"polecat/nux/gt-e95+abc": mergedMR("gt-mr-1"),
	}}
	workDir := t.TempDir()

	result := ReconcileMergeRequestedWisps(bd, finder, workDir)

	if len(result.Closed) != 1 || result.Closed[0] != "gt-wisp-1" {
		t.Fatalf("Closed = %v, want [gt-wisp-1]", result.Closed)
	}
	if len(result.Preserved) != 0 || len(result.NeedsAudit) != 0 || len(result.Errors) != 0 {
		t.Errorf("unexpected side effects: preserved=%v needsAudit=%v errors=%v", result.Preserved, result.NeedsAudit, result.Errors)
	}
	calls := strings.Join(mock.calls, "\n")
	if !strings.Contains(calls, "close gt-wisp-1") {
		t.Errorf("expected a close call for gt-wisp-1, got calls: %s", calls)
	}
	if !strings.Contains(calls, "gt-mr-1") {
		t.Errorf("expected close reason to cite the verified MR id, got calls: %s", calls)
	}
}

func TestReconcileMergeRequestedWisps_RejectedOrPendingPreservesTracker(t *testing.T) {
	t.Parallel()
	bd, mock := mockBd(
		func(args []string) (string, error) {
			switch {
			case len(args) > 0 && args[0] == "list":
				return `[{"id":"gt-wisp-rej"},{"id":"gt-wisp-pend"}]`, nil
			case len(args) > 0 && args[0] == "show":
				id := args[1]
				branch := map[string]string{"gt-wisp-rej": "b-rej", "gt-wisp-pend": "b-pend"}[id]
				return wispShowJSON(t, id, branch), nil
			}
			return "{}", nil
		},
		func(args []string) error { return nil },
	)
	finder := &fakeMRFinder{byBranch: map[string]*beads.Issue{
		"b-rej":  rejectedMR("gt-mr-rej"),
		"b-pend": openMR("gt-mr-pend"),
	}}
	workDir := t.TempDir()

	result := ReconcileMergeRequestedWisps(bd, finder, workDir)

	if len(result.Closed) != 0 {
		t.Fatalf("Closed = %v, want none (rejected/pending must be preserved)", result.Closed)
	}
	if len(result.Preserved) != 2 {
		t.Fatalf("Preserved = %v, want both wisps preserved", result.Preserved)
	}
	calls := strings.Join(mock.calls, "\n")
	if strings.Contains(calls, "close") {
		t.Errorf("expected no close call for rejected/pending wisps, got calls: %s", calls)
	}
}

func TestReconcileMergeRequestedWisps_NoCorrelatedMRNeedsAuditNotClosed(t *testing.T) {
	t.Parallel()
	// Bug requirement: a tracker with no provable outcome (here: no MR bead
	// found for its branch — the bd-record analogue of a missing branch
	// ref) must be surfaced for audit, never destructively closed.
	bd, mock := mockBd(
		func(args []string) (string, error) {
			switch {
			case len(args) > 0 && args[0] == "list":
				return `[{"id":"gt-wisp-orphan"}]`, nil
			case len(args) > 0 && args[0] == "show":
				return wispShowJSON(t, "gt-wisp-orphan", "polecat/nux/never-tracked"), nil
			}
			return "{}", nil
		},
		func(args []string) error { return nil },
	)
	finder := &fakeMRFinder{byBranch: map[string]*beads.Issue{}} // no MR correlates

	result := ReconcileMergeRequestedWisps(bd, finder, t.TempDir())

	if len(result.Closed) != 0 {
		t.Fatalf("Closed = %v, want none for an uncorrelated wisp", result.Closed)
	}
	if len(result.NeedsAudit) != 1 || result.NeedsAudit[0] != "gt-wisp-orphan" {
		t.Fatalf("NeedsAudit = %v, want [gt-wisp-orphan]", result.NeedsAudit)
	}
	calls := strings.Join(mock.calls, "\n")
	if strings.Contains(calls, "close") {
		t.Errorf("expected no close call for an uncorrelated wisp, got calls: %s", calls)
	}
}

func TestReconcileMergeRequestedWisps_IdempotentOnRerun(t *testing.T) {
	t.Parallel()
	// First sweep: one merge-requested wisp, proven merged, gets closed.
	closed := false
	bd, mock := mockBd(
		func(args []string) (string, error) {
			switch {
			case len(args) > 0 && args[0] == "list":
				if closed {
					// Closed wisps drop out of the --status open query used
					// by every subsequent sweep.
					return `[]`, nil
				}
				return `[{"id":"gt-wisp-1"}]`, nil
			case len(args) > 0 && args[0] == "show":
				return wispShowJSON(t, "gt-wisp-1", "polecat/nux/gt-e95+abc"), nil
			}
			return "{}", nil
		},
		func(args []string) error { return nil },
	)
	finder := &fakeMRFinder{byBranch: map[string]*beads.Issue{
		"polecat/nux/gt-e95+abc": mergedMR("gt-mr-1"),
	}}
	workDir := t.TempDir()

	first := ReconcileMergeRequestedWisps(bd, finder, workDir)
	if len(first.Closed) != 1 {
		t.Fatalf("first sweep Closed = %v, want 1 close", first.Closed)
	}
	closed = true
	mock.calls = nil

	second := ReconcileMergeRequestedWisps(bd, finder, workDir)
	if len(second.Closed) != 0 || second.Scanned != 0 {
		t.Fatalf("second sweep = %+v, want a no-op rerun", second)
	}
	if strings.Contains(strings.Join(mock.calls, "\n"), "close") {
		t.Errorf("rerun should not attempt to close an already-closed wisp")
	}
}

func TestReconcileMergeRequestedWisps_OneWispErrorDoesNotAbortSweep(t *testing.T) {
	t.Parallel()
	// Per-item failure isolation: a bad wisp must not block reconciliation
	// of the rest of the batch (matches DetectZombiePolecats' convention).
	bd, _ := mockBd(
		func(args []string) (string, error) {
			switch {
			case len(args) > 0 && args[0] == "list":
				return `[{"id":"gt-wisp-bad"},{"id":"gt-wisp-good"}]`, nil
			case len(args) > 0 && args[0] == "show":
				id := args[1]
				if id == "gt-wisp-bad" {
					return "", fmt.Errorf("bd show failed")
				}
				return wispShowJSON(t, id, "b-good"), nil
			}
			return "{}", nil
		},
		func(args []string) error { return nil },
	)
	finder := &fakeMRFinder{byBranch: map[string]*beads.Issue{
		"b-good": mergedMR("gt-mr-good"),
	}}

	result := ReconcileMergeRequestedWisps(bd, finder, t.TempDir())

	if len(result.Errors) != 1 {
		t.Fatalf("Errors = %v, want exactly 1 error for the bad wisp", result.Errors)
	}
	if len(result.Closed) != 1 || result.Closed[0] != "gt-wisp-good" {
		t.Fatalf("Closed = %v, want the good wisp still processed", result.Closed)
	}
}

func TestListOpenMergeRequestedWisps_QueriesRigWideNotPerPolecat(t *testing.T) {
	t.Parallel()
	bd, mock := mockBd(
		func(args []string) (string, error) {
			return `[{"id":"gt-wisp-a"},{"id":"gt-wisp-b"}]`, nil
		},
		func(args []string) error { return nil },
	)

	ids, err := listOpenMergeRequestedWisps(bd, t.TempDir())
	if err != nil {
		t.Fatalf("listOpenMergeRequestedWisps: %v", err)
	}
	if len(ids) != 2 || ids[0] != "gt-wisp-a" || ids[1] != "gt-wisp-b" {
		t.Fatalf("ids = %v, want [gt-wisp-a gt-wisp-b]", ids)
	}
	calls := strings.Join(mock.calls, "\n")
	if !strings.Contains(calls, "state:merge-requested") || !strings.Contains(calls, "--status open") {
		t.Errorf("expected rig-wide state:merge-requested/--status open query, got: %s", calls)
	}
	if strings.Contains(calls, "polecat:") {
		t.Errorf("expected no per-polecat label constraint for a rig-wide sweep, got: %s", calls)
	}
}

func TestWispBranchLine_ParsesBranchFromDescription(t *testing.T) {
	t.Parallel()
	bd, _ := mockBd(
		func(args []string) (string, error) {
			return wispShowJSON(t, "gt-wisp-1", "polecat/nux/gt-e95+abc"), nil
		},
		func(args []string) error { return nil },
	)

	branch, err := wispBranchLine(bd, t.TempDir(), "gt-wisp-1")
	if err != nil {
		t.Fatalf("wispBranchLine: %v", err)
	}
	if branch != "polecat/nux/gt-e95+abc" {
		t.Errorf("branch = %q, want polecat/nux/gt-e95+abc", branch)
	}
}

func TestWispBranchLine_EmptyWhenNoBranchLine(t *testing.T) {
	t.Parallel()
	bd, _ := mockBd(
		func(args []string) (string, error) {
			return `[{"id":"gt-wisp-1","description":"Verify and cleanup polecat nux"}]`, nil
		},
		func(args []string) error { return nil },
	)

	branch, err := wispBranchLine(bd, t.TempDir(), "gt-wisp-1")
	if err != nil {
		t.Fatalf("wispBranchLine: %v", err)
	}
	if branch != "" {
		t.Errorf("branch = %q, want empty", branch)
	}
}
