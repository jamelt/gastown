package cmd

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/refinery"
)

type fakeMQPostMergeManager struct {
	mr              *refinery.MergeRequest
	findErr         error
	postMergeErr    error
	postMergeCalled bool
	postMergeMR     *refinery.MergeRequest
}

func (m *fakeMQPostMergeManager) FindMRForPostMerge(string) (*refinery.MergeRequest, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	return m.mr, nil
}

func (m *fakeMQPostMergeManager) PostMergeMR(mr *refinery.MergeRequest) (*refinery.PostMergeResult, error) {
	m.postMergeCalled = true
	m.postMergeMR = mr
	if m.postMergeErr != nil {
		return nil, m.postMergeErr
	}
	return &refinery.PostMergeResult{MR: m.mr, MRClosed: true, SourceIssueClosed: true, SourceIssueID: m.mr.IssueID}, nil
}

type fakeMQPostMergeGit struct {
	verifyErr     error
	tipVerifyErr  error
	openPR        bool
	openPRHeadSHA string // when set, HasOpenPullRequest also requires ref.HeadSHA to match (simulates the real exact-HeadSHA PR lookup)
	deleteErr     error
	remoteTip     string
	localHead     string
	tipErr        error

	verifiedCommits []string
	deletedBranches []string
	deletedHeads    []string
	localDeleted    []string
	prRefs          []git.PullRequestRef
}

// VerifyPushedCommitReachableFromPushTarget is called twice on the happy
// path: once by verifyMQPostMergeProof against the MR's submitted commit_sha,
// and again by cleanupMQPostMergeBranch against the freshly-read remote tip.
// tipVerifyErr lets tests fail only the second call.
func (g *fakeMQPostMergeGit) VerifyPushedCommitReachableFromPushTarget(_, _, commit string) error {
	g.verifiedCommits = append(g.verifiedCommits, commit)
	if len(g.verifiedCommits) > 1 {
		return g.tipVerifyErr
	}
	return g.verifyErr
}

func (g *fakeMQPostMergeGit) HasOpenPullRequest(ref git.PullRequestRef) bool {
	g.prRefs = append(g.prRefs, ref)
	if g.openPRHeadSHA != "" {
		return g.openPR && ref.HeadSHA == g.openPRHeadSHA
	}
	return g.openPR
}

func (g *fakeMQPostMergeGit) PushRemoteBranchTip(_, _ string) (string, error) {
	return g.remoteTip, g.tipErr
}

func (g *fakeMQPostMergeGit) Rev(string) (string, error) {
	return g.localHead, nil
}

func (g *fakeMQPostMergeGit) DeleteRemoteBranchIfAt(_, branch, expectedHash string) error {
	g.deletedBranches = append(g.deletedBranches, branch)
	g.deletedHeads = append(g.deletedHeads, expectedHash)
	return g.deleteErr
}

func (g *fakeMQPostMergeGit) DeleteBranch(branch string, _ bool) error {
	g.localDeleted = append(g.localDeleted, branch)
	return nil
}

func testMQPostMergeMR() *refinery.MergeRequest {
	return &refinery.MergeRequest{
		ID:           "gt-mr-proof",
		Branch:       "polecat/test/gt-proof",
		Worker:       "polecats/test",
		IssueID:      "gt-proof",
		TargetBranch: "main",
		CommitSHA:    "abc123def456",
	}
}

func TestRunVerifiedMQPostMerge_ProofFailurePreservesRecordsAndBranch(t *testing.T) {
	mgr := &fakeMQPostMergeManager{mr: testMQPostMergeMR()}
	rigGit := &fakeMQPostMergeGit{verifyErr: errors.New("not reachable")}

	_, _, err := runVerifiedMQPostMerge(mgr, t.TempDir(), rigGit, "origin", mgr.mr.ID, false)
	if err == nil || !strings.Contains(err.Error(), "merge proof failed") {
		t.Fatalf("runVerifiedMQPostMerge error = %v, want merge proof failure", err)
	}
	if !strings.Contains(err.Error(), mgr.mr.CommitSHA) {
		t.Fatalf("proof error %q does not mention submitted head %s", err, mgr.mr.CommitSHA)
	}
	if mgr.postMergeCalled {
		t.Fatal("PostMerge called after failed proof")
	}
	if len(rigGit.deletedBranches) != 0 {
		t.Fatalf("remote branch deleted after failed proof: %v", rigGit.deletedBranches)
	}
	if len(rigGit.localDeleted) != 0 {
		t.Fatalf("local branch deleted after failed proof: %v", rigGit.localDeleted)
	}
}

func TestRunVerifiedMQPostMerge_VerifiedHeadClosesAndLeaseDeletes(t *testing.T) {
	mgr := &fakeMQPostMergeManager{mr: testMQPostMergeMR()}
	rigGit := &fakeMQPostMergeGit{remoteTip: mgr.mr.CommitSHA, localHead: mgr.mr.CommitSHA}

	_, cleanup, err := runVerifiedMQPostMerge(mgr, t.TempDir(), rigGit, "origin", mgr.mr.ID, false)
	if err != nil {
		t.Fatalf("runVerifiedMQPostMerge: %v", err)
	}
	if !mgr.postMergeCalled {
		t.Fatal("PostMerge was not called after successful proof")
	}
	if mgr.postMergeMR != mgr.mr {
		t.Fatal("PostMerge did not use the verified MR snapshot")
	}
	wantVerified := []string{mgr.mr.CommitSHA, mgr.mr.CommitSHA}
	if !reflect.DeepEqual(rigGit.verifiedCommits, wantVerified) {
		t.Fatalf("verified commits = %v, want %v", rigGit.verifiedCommits, wantVerified)
	}
	if !cleanup.RemoteDeleted || len(rigGit.deletedBranches) != 1 || rigGit.deletedBranches[0] != mgr.mr.Branch {
		t.Fatalf("remote delete = cleanup=%+v branches=%v", cleanup, rigGit.deletedBranches)
	}
	if len(rigGit.deletedHeads) != 1 || rigGit.deletedHeads[0] != mgr.mr.CommitSHA {
		t.Fatalf("deleted heads = %v, want [%s]", rigGit.deletedHeads, mgr.mr.CommitSHA)
	}
	if !cleanup.LocalDeleted || len(rigGit.localDeleted) != 1 || rigGit.localDeleted[0] != mgr.mr.Branch {
		t.Fatalf("local delete = cleanup=%+v local=%v", cleanup, rigGit.localDeleted)
	}
}

// TestRunVerifiedMQPostMerge_StaleCommitSHAUsesFreshRemoteTip covers gt-twuj:
// a conflict-resolution push after MR submission moves the branch tip without
// updating the MR bead's recorded commit_sha. The delete's compare-and-swap
// must target the branch's current remote tip, not the stale submitted head.
func TestRunVerifiedMQPostMerge_StaleCommitSHAUsesFreshRemoteTip(t *testing.T) {
	mgr := &fakeMQPostMergeManager{mr: testMQPostMergeMR()}
	freshTip := "eb20c1f259fresh"
	rigGit := &fakeMQPostMergeGit{remoteTip: freshTip, localHead: freshTip}

	_, cleanup, err := runVerifiedMQPostMerge(mgr, t.TempDir(), rigGit, "origin", mgr.mr.ID, false)
	if err != nil {
		t.Fatalf("runVerifiedMQPostMerge: %v", err)
	}
	if !cleanup.RemoteDeleted {
		t.Fatalf("cleanup.RemoteDeleted = false, cleanup=%+v", cleanup)
	}
	if len(rigGit.deletedHeads) != 1 || rigGit.deletedHeads[0] != freshTip {
		t.Fatalf("deleted heads = %v, want [%s] (fresh tip, not stale commit_sha %s)", rigGit.deletedHeads, freshTip, mgr.mr.CommitSHA)
	}
	if !cleanup.LocalDeleted || len(rigGit.localDeleted) != 1 || rigGit.localDeleted[0] != mgr.mr.Branch {
		t.Fatalf("local delete = cleanup=%+v local=%v, want deleted at fresh tip %s", cleanup, rigGit.localDeleted, freshTip)
	}
}

// TestRunVerifiedMQPostMerge_FreshTipNotReachableFailsClosed covers the
// reachability re-check added alongside the gt-twuj fix: the freshly-read
// remote tip is a new, unverified value (unlike the already-proven submitted
// commit_sha), so it must be confirmed reachable from the target branch
// before it's used as the delete's compare-and-swap reference.
func TestRunVerifiedMQPostMerge_FreshTipNotReachableFailsClosed(t *testing.T) {
	mgr := &fakeMQPostMergeManager{mr: testMQPostMergeMR()}
	freshTip := "eb20c1f259fresh"
	rigGit := &fakeMQPostMergeGit{remoteTip: freshTip, localHead: freshTip, tipVerifyErr: errors.New("not reachable")}

	_, cleanup, err := runVerifiedMQPostMerge(mgr, t.TempDir(), rigGit, "origin", mgr.mr.ID, false)
	if err == nil || !strings.Contains(err.Error(), "not proven on") {
		t.Fatalf("runVerifiedMQPostMerge error = %v, want fresh-tip reachability failure", err)
	}
	if cleanup.RemoteDeleted || len(rigGit.deletedBranches) != 0 {
		t.Fatalf("remote branch deleted despite unproven fresh tip: cleanup=%+v branches=%v", cleanup, rigGit.deletedBranches)
	}
	if len(rigGit.localDeleted) != 0 {
		t.Fatalf("local branch deleted despite unproven fresh tip: %v", rigGit.localDeleted)
	}
}

func TestRunVerifiedMQPostMerge_SkipBranchDeleteStillRequiresProof(t *testing.T) {
	mgr := &fakeMQPostMergeManager{mr: testMQPostMergeMR()}
	rigGit := &fakeMQPostMergeGit{}

	_, cleanup, err := runVerifiedMQPostMerge(mgr, t.TempDir(), rigGit, "origin", mgr.mr.ID, true)
	if err != nil {
		t.Fatalf("runVerifiedMQPostMerge: %v", err)
	}
	if !mgr.postMergeCalled {
		t.Fatal("PostMerge was not called after successful proof")
	}
	if len(rigGit.verifiedCommits) != 1 || rigGit.verifiedCommits[0] != mgr.mr.CommitSHA {
		t.Fatalf("verified commits = %v, want [%s]", rigGit.verifiedCommits, mgr.mr.CommitSHA)
	}
	if !cleanup.Skipped {
		t.Fatalf("cleanup.Skipped = false, cleanup=%+v", cleanup)
	}
	if len(rigGit.deletedBranches) != 0 || len(rigGit.localDeleted) != 0 {
		t.Fatalf("branch deleted despite skip: remote=%v local=%v", rigGit.deletedBranches, rigGit.localDeleted)
	}
}

func TestRunVerifiedMQPostMerge_OpenPRSkipsRemoteDeleteAfterProof(t *testing.T) {
	mgr := &fakeMQPostMergeManager{mr: testMQPostMergeMR()}
	rigGit := &fakeMQPostMergeGit{openPR: true, remoteTip: mgr.mr.CommitSHA, localHead: mgr.mr.CommitSHA}

	_, cleanup, err := runVerifiedMQPostMerge(mgr, t.TempDir(), rigGit, "origin", mgr.mr.ID, false)
	if err != nil {
		t.Fatalf("runVerifiedMQPostMerge: %v", err)
	}
	if !mgr.postMergeCalled {
		t.Fatal("PostMerge was not called after successful proof")
	}
	if !cleanup.OpenPR {
		t.Fatalf("cleanup.OpenPR = false, cleanup=%+v", cleanup)
	}
	if len(rigGit.deletedBranches) != 0 {
		t.Fatalf("remote branch deleted despite open PR: %v", rigGit.deletedBranches)
	}
	if len(rigGit.localDeleted) != 1 || rigGit.localDeleted[0] != mgr.mr.Branch {
		t.Fatalf("local branch cleanup = %v, want [%s]", rigGit.localDeleted, mgr.mr.Branch)
	}
}

// TestRunVerifiedMQPostMerge_OpenPRLookupUsesFreshRemoteTipNotStaleCommitSHA
// covers gt-zr7e: a conflict-resolution push after MR submission can move the
// branch's remote tip past the recorded mr.CommitSHA. The open-PR lookup must
// use that fresh tip, not the stale commit_sha, or a genuinely open PR at the
// new head is missed and the branch gets deleted out from under it.
func TestRunVerifiedMQPostMerge_OpenPRLookupUsesFreshRemoteTipNotStaleCommitSHA(t *testing.T) {
	mgr := &fakeMQPostMergeManager{mr: testMQPostMergeMR()}
	movedTip := "movedaftertheconflictresolutionpush"
	rigGit := &fakeMQPostMergeGit{
		remoteTip:     movedTip,
		openPR:        true,
		openPRHeadSHA: movedTip,
		localHead:     mgr.mr.CommitSHA,
	}

	_, cleanup, err := runVerifiedMQPostMerge(mgr, t.TempDir(), rigGit, "origin", mgr.mr.ID, false)
	if err != nil {
		t.Fatalf("runVerifiedMQPostMerge: %v", err)
	}
	if !cleanup.OpenPR {
		t.Fatalf("cleanup.OpenPR = false, want true: open-PR lookup must use the fresh remote tip %q, not the stale commit_sha %q, cleanup=%+v", movedTip, mgr.mr.CommitSHA, cleanup)
	}
	if len(rigGit.prRefs) != 1 || rigGit.prRefs[0].HeadSHA != movedTip {
		t.Fatalf("HasOpenPullRequest refs = %+v, want a single call with HeadSHA=%q", rigGit.prRefs, movedTip)
	}
	if len(rigGit.deletedBranches) != 0 {
		t.Fatalf("remote branch deleted despite open PR at moved head: %v", rigGit.deletedBranches)
	}
}

func TestRunVerifiedMQPostMerge_LeaseDeleteFailureReturnsAfterPostMerge(t *testing.T) {
	mgr := &fakeMQPostMergeManager{mr: testMQPostMergeMR()}
	rigGit := &fakeMQPostMergeGit{remoteTip: mgr.mr.CommitSHA, deleteErr: errors.New("stale info")}

	_, _, err := runVerifiedMQPostMerge(mgr, t.TempDir(), rigGit, "origin", mgr.mr.ID, false)
	if err == nil || !strings.Contains(err.Error(), "remote branch delete") {
		t.Fatalf("runVerifiedMQPostMerge error = %v, want remote branch delete failure", err)
	}
	if !mgr.postMergeCalled {
		t.Fatal("PostMerge was not called after successful proof")
	}
	if len(rigGit.deletedBranches) != 1 || rigGit.deletedBranches[0] != mgr.mr.Branch {
		t.Fatalf("remote delete attempts = %v, want [%s]", rigGit.deletedBranches, mgr.mr.Branch)
	}
	if len(rigGit.deletedHeads) != 1 || rigGit.deletedHeads[0] != mgr.mr.CommitSHA {
		t.Fatalf("delete lease heads = %v, want [%s]", rigGit.deletedHeads, mgr.mr.CommitSHA)
	}
	if len(rigGit.localDeleted) != 0 {
		t.Fatalf("local branch deleted after remote lease failure: %v", rigGit.localDeleted)
	}
}

func TestRunVerifiedMQPostMerge_MissingRemoteBranchIsIdempotentAfterProof(t *testing.T) {
	mgr := &fakeMQPostMergeManager{mr: testMQPostMergeMR()}
	rigGit := &fakeMQPostMergeGit{localHead: mgr.mr.CommitSHA}

	_, cleanup, err := runVerifiedMQPostMerge(mgr, t.TempDir(), rigGit, "origin", mgr.mr.ID, false)
	if err != nil {
		t.Fatalf("runVerifiedMQPostMerge: %v", err)
	}
	if !mgr.postMergeCalled {
		t.Fatal("PostMerge was not called after successful proof")
	}
	if !cleanup.AlreadyGone {
		t.Fatalf("cleanup.AlreadyGone = false, cleanup=%+v", cleanup)
	}
	if len(rigGit.deletedBranches) != 0 {
		t.Fatalf("remote branch delete attempted for missing branch: %v", rigGit.deletedBranches)
	}
}

func TestRunVerifiedMQPostMerge_MissingSubmittedHeadFailsClosed(t *testing.T) {
	mr := testMQPostMergeMR()
	mr.CommitSHA = ""
	mgr := &fakeMQPostMergeManager{mr: mr}
	rigGit := &fakeMQPostMergeGit{}

	_, _, err := runVerifiedMQPostMerge(mgr, t.TempDir(), rigGit, "origin", mgr.mr.ID, false)
	if err == nil || !strings.Contains(err.Error(), "missing submitted commit_sha") {
		t.Fatalf("runVerifiedMQPostMerge error = %v, want missing submitted head", err)
	}
	if mgr.postMergeCalled {
		t.Fatal("PostMerge called with missing submitted head")
	}
	if len(rigGit.deletedBranches) != 0 {
		t.Fatalf("branch deleted with missing submitted head: %v", rigGit.deletedBranches)
	}
}

func TestRunVerifiedMQPostMerge_SourceTargetBranchFailsClosed(t *testing.T) {
	mr := testMQPostMergeMR()
	mr.Branch = "main"
	mr.TargetBranch = "main"
	mgr := &fakeMQPostMergeManager{mr: mr}
	rigGit := &fakeMQPostMergeGit{}

	_, _, err := runVerifiedMQPostMerge(mgr, t.TempDir(), rigGit, "origin", mgr.mr.ID, false)
	if err == nil || !strings.Contains(err.Error(), "matches target branch") {
		t.Fatalf("runVerifiedMQPostMerge error = %v, want source/target failure", err)
	}
	if mgr.postMergeCalled {
		t.Fatal("PostMerge called when source branch matched target")
	}
	if len(rigGit.deletedBranches) != 0 {
		t.Fatalf("branch deleted when source matched target: %v", rigGit.deletedBranches)
	}
}
