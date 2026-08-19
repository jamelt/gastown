package refinery

import (
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/polecat"
)

func TestValidateTerminalMRCloseSnapshotRejectsDrift(t *testing.T) {
	expected := &MergeRequest{
		ID:           "gt-mr-proof",
		Branch:       "polecat/test/proof",
		IssueID:      "gt-proof",
		TargetBranch: "main",
		CommitSHA:    "abc123",
	}
	fields := &beads.MRFields{
		Branch:      "polecat/test/proof",
		SourceIssue: "gt-proof",
		Target:      "main",
		CommitSHA:   "def456",
	}

	err := validateTerminalMRCloseSnapshot(expected.ID, fields, expected)
	if err == nil || !strings.Contains(err.Error(), "changed after merge proof") {
		t.Fatalf("validateTerminalMRCloseSnapshot error = %v, want drift failure", err)
	}
}

func TestValidateTerminalMRCloseSnapshotAllowsMatchingSnapshot(t *testing.T) {
	expected := &MergeRequest{
		ID:           "gt-mr-proof",
		Branch:       "polecat/test/proof",
		IssueID:      "gt-proof",
		TargetBranch: "main",
		CommitSHA:    "abc123",
	}
	fields := &beads.MRFields{
		Branch:      "polecat/test/proof",
		SourceIssue: "gt-proof",
		Target:      "main",
		CommitSHA:   "abc123",
	}

	if err := validateTerminalMRCloseSnapshot(expected.ID, fields, expected); err != nil {
		t.Fatalf("validateTerminalMRCloseSnapshot: %v", err)
	}
}

// TestEngineerCloseMRWithReasonMergedInvalidatesCleanupStatus is part of
// gt-h6u4: a merged MR must invalidate the source polecat's persisted
// cleanup_status immediately, instead of leaving a possibly-stale "clean"
// claim for gt polecat list / gt scheduler status to trust until the next
// witness patrol sweep.
func TestEngineerCloseMRWithReasonMergedInvalidatesCleanupStatus(t *testing.T) {
	e, b, mrIssue, agentIssue, _ := setupEngineerTerminalCloseTest(t, "gt-wisp-old")
	if err := b.UpdateAgentCleanupStatus(agentIssue.ID, string(polecat.CleanupClean)); err != nil {
		t.Fatalf("seed cleanup_status: %v", err)
	}

	if err := e.closeMRWithReason(&MRInfo{ID: mrIssue.ID, AgentBead: agentIssue.ID}, string(CloseReasonMerged)); err != nil {
		t.Fatalf("closeMRWithReason: %v", err)
	}

	assertAgentActiveMR(t, b, agentIssue.ID, "")
	assertAgentCleanupStatus(t, b, agentIssue.ID, string(polecat.CleanupUnknown))
}

// TestEngineerCloseMRWithReasonRejectedLeavesCleanupStatusAlone confirms
// gt-h6u4 scoped the eager-invalidation write to the merged path only: the
// MR-rejection case is gt-nasl's territory (it proposes its own terminal
// cleanup_status value at this same call site), so gt-h6u4 must not preempt
// it here.
func TestEngineerCloseMRWithReasonRejectedLeavesCleanupStatusAlone(t *testing.T) {
	e, b, mrIssue, agentIssue, _ := setupEngineerTerminalCloseTest(t, "gt-wisp-old")
	if err := b.UpdateAgentCleanupStatus(agentIssue.ID, string(polecat.CleanupClean)); err != nil {
		t.Fatalf("seed cleanup_status: %v", err)
	}

	if err := e.closeMRWithReason(&MRInfo{ID: mrIssue.ID, AgentBead: agentIssue.ID}, "rejected: gate failure"); err != nil {
		t.Fatalf("closeMRWithReason: %v", err)
	}

	assertAgentActiveMR(t, b, agentIssue.ID, "")
	assertAgentCleanupStatus(t, b, agentIssue.ID, string(polecat.CleanupClean))
}

func assertAgentCleanupStatus(t *testing.T, b *beads.Beads, agentID string, want string) {
	t.Helper()
	issue, err := b.Show(agentID)
	if err != nil {
		t.Fatalf("show agent %s: %v", agentID, err)
	}
	fields := beads.ParseAgentFields(issue.Description)
	if fields.CleanupStatus != want {
		t.Fatalf("agent cleanup_status = %q, want %q", fields.CleanupStatus, want)
	}
}
