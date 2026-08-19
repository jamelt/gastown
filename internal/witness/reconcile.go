package witness

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
)

// mrFinder looks up the merge-request bead correlated with a branch. It is
// satisfied by *beads.Beads in production; tests supply a fake so decision
// logic can be exercised without a real Dolt-backed store.
type mrFinder interface {
	FindMRForBranchAny(branch string) (*beads.Issue, error)
}

// ReconcileResult summarizes one merge-requested-wisp reconciliation sweep.
type ReconcileResult struct {
	Scanned    int
	Closed     []string
	Preserved  []string
	NeedsAudit []string
	Errors     []error
}

// ReconcileMergeRequestedWisps closes witness cleanup wisps whose merge
// request has been definitively merged, per the correlated MR bead's own
// terminal outcome — never inferred from the source work bead's status
// (gt-e95). Wisps for still-open, rejected, conflicted, or superseded MRs
// are preserved untouched. A wisp with no correlated MR bead (e.g. a
// missing/never-created MR record) is surfaced for audit rather than
// closed. Safe to call repeatedly: it only considers currently-open
// state:merge-requested wisps, so a closed wisp never reappears in a later
// sweep, and one wisp's error never aborts the rest of the sweep.
func ReconcileMergeRequestedWisps(bd *BdCli, mrBeads mrFinder, workDir string) *ReconcileResult {
	result := &ReconcileResult{}

	wispIDs, err := listOpenMergeRequestedWisps(bd, workDir)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("listing merge-requested wisps: %w", err))
		return result
	}
	result.Scanned = len(wispIDs)

	for _, wispID := range wispIDs {
		branch, err := wispBranchLine(bd, workDir, wispID)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("reading wisp %s: %w", wispID, err))
			continue
		}
		if branch == "" {
			result.NeedsAudit = append(result.NeedsAudit, wispID)
			continue
		}

		mr, err := mrBeads.FindMRForBranchAny(branch)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("looking up MR for wisp %s (branch %s): %w", wispID, branch, err))
			continue
		}

		switch reconcileWispAction(mr) {
		case reconcileClose:
			reason := fmt.Sprintf("merged: verified via refinery MR %s (gt-e95 reconciliation)", mr.ID)
			if _, err := bd.Exec(workDir, "close", wispID, "--reason="+reason); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("closing wisp %s: %w", wispID, err))
				continue
			}
			result.Closed = append(result.Closed, wispID)
		case reconcileAudit:
			result.NeedsAudit = append(result.NeedsAudit, wispID)
		default:
			result.Preserved = append(result.Preserved, wispID)
		}
	}
	return result
}

type reconcileAction int

const (
	reconcilePreserve reconcileAction = iota
	reconcileClose
	reconcileAudit
)

// reconcileWispAction decides what to do with a merge-requested wisp given
// its correlated MR bead (nil if none was found). Pure decision logic, no
// I/O, so regression tests exercise it directly against fixture MRs.
func reconcileWispAction(mr *beads.Issue) reconcileAction {
	if mr == nil {
		return reconcileAudit
	}
	if beads.IssueStatus(mr.Status) != beads.StatusClosed {
		return reconcilePreserve
	}
	fields := beads.ParseMRFields(mr)
	if fields != nil && fields.CloseReason == "merged" {
		return reconcileClose
	}
	return reconcilePreserve
}

// listOpenMergeRequestedWisps bulk-lists open cleanup wisps across the rig
// in state:merge-requested, mirroring findCleanupWisp's query (handlers.go)
// without the per-polecat label constraint.
func listOpenMergeRequestedWisps(bd *BdCli, workDir string) ([]string, error) {
	output, err := bd.Exec(workDir, "list",
		"--label", "state:merge-requested",
		"--status", "open",
		"--json",
	)
	if err != nil {
		return nil, err
	}
	if output == "" || output == "[]" || output == "null" {
		return nil, nil
	}
	var items []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(output), &items); err != nil {
		return nil, fmt.Errorf("parsing wisp list: %w", err)
	}
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	return ids, nil
}

// wispBranchLine fetches a wisp's full description via bd show and extracts
// the "Branch: <name>" line written by createCleanupWisp. Returns "" if the
// wisp has no recorded branch.
func wispBranchLine(bd *BdCli, workDir, wispID string) (string, error) {
	output, err := bd.Exec(workDir, "show", wispID, "--json")
	if err != nil {
		return "", err
	}
	var items []struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(output), &items); err != nil {
		return "", fmt.Errorf("parsing wisp %s: %w", wispID, err)
	}
	if len(items) == 0 {
		return "", nil
	}
	for _, line := range strings.Split(items[0].Description, "\n") {
		if branch, ok := strings.CutPrefix(strings.TrimSpace(line), "Branch: "); ok {
			return strings.TrimSpace(branch), nil
		}
	}
	return "", nil
}
