package refinery

import (
	"testing"
)

// TestSubmittedCommitBasedOn is the refinery-side defense for gt-fi6e: the
// pre-verification fast-path must only be honored when the submitted commit is
// actually based on the attested base. A branch that is behind the recorded
// base (never rebased) must be rejected so it falls back to full gates.
func TestSubmittedCommitBasedOn(t *testing.T) {
	workDir, g, _ := testGitRepo(t)
	e := newTestEngineer(t, workDir, g)

	// Feature branch cut from the current main tip (A).
	createFeatureBranch(t, workDir, "feat/x", "x.txt", "hello")
	featTip := run(t, workDir, "git", "rev-parse", "feat/x")
	baseA := run(t, workDir, "git", "rev-parse", "main")

	// Genuinely based on the recorded base A → verified.
	based, err := e.submittedCommitBasedOn(&MRInfo{Branch: "feat/x", CommitSHA: featTip}, baseA)
	if err != nil {
		t.Fatalf("unexpected error for based-on-base case: %v", err)
	}
	if !based {
		t.Fatalf("branch based on recorded base A must verify as based-on")
	}

	// Advance main to B; the feature branch does not contain B. Recording B as the
	// pre-verified base (what the buggy write path did for an unrebased branch)
	// must be rejected.
	writeFile(t, workDir, "y.txt", "world")
	run(t, workDir, "git", "add", ".")
	run(t, workDir, "git", "commit", "-m", "advance main")
	baseB := run(t, workDir, "git", "rev-parse", "main")

	based, err = e.submittedCommitBasedOn(&MRInfo{Branch: "feat/x", CommitSHA: featTip}, baseB)
	if err != nil {
		t.Fatalf("unexpected error for behind-base case: %v", err)
	}
	if based {
		t.Fatalf("branch behind recorded base B must NOT be reported as based on it (gt-fi6e)")
	}
}
