package polecat

import "testing"

// gt-nz5x: gt done sets agent_state=done on completion, so a polecat that
// finishes work is StateDone and never returns to StateIdle. Restricting reuse
// candidates to StateIdle meant no completed polecat was ever reused — every
// dispatch allocated a fresh worktree until the rig hit its directory cap and
// dispatch stopped entirely, while the fleet reported reusable polecats nobody
// could use.
func TestIsReuseCandidateState(t *testing.T) {
	for _, tc := range []struct {
		state State
		want  bool
		why   string
	}{
		{StateIdle, true, "never assigned — the original candidate"},
		{StateDone, true, "finished work; gt done leaves polecats here permanently"},
		{StateWorking, false, "actively holds work"},
		{StateStalled, false, "interrupted mid-work; needs recovery, not reuse"},
		{StateReviewNeeded, false, "live session with unclear cleanup"},
		{StateStuck, true, "hq-f2n8c: gt done --status DEFERRED/ESCALATED lands here; vet it on predicates, not on the label"},
	} {
		if got := isReuseCandidateState(tc.state); got != tc.want {
			t.Errorf("isReuseCandidateState(%s) = %v, want %v (%s)", tc.state, got, tc.want, tc.why)
		}
	}
}

// The candidate filter must stay consistent with DecideWorkstate, which lets
// exactly these three states fall through to the real reuse predicates. If one
// side is changed without the other, polecats become either unreusable (the
// gt-nz5x outage) or reusable without being vetted.
func TestReuseCandidateStatesMatchWorkstateFallthrough(t *testing.T) {
	for _, s := range []State{StateIdle, StateDone, StateStuck, StateWorking, StateStalled, StateReviewNeeded} {
		d := DecideWorkstate(WorkstateInput{State: s, CleanupStatus: CleanupClean})
		// "not-idle" is the short-circuit DecideWorkstate returns for states it
		// refuses to evaluate against the real predicates.
		evaluated := d.Reason != "not-idle"
		if evaluated != isReuseCandidateState(s) {
			t.Errorf("state %s: DecideWorkstate evaluates=%v but isReuseCandidateState=%v — the two must agree",
				s, evaluated, isReuseCandidateState(s))
		}
	}
}

// hq-f2n8c / hq-vv0q: an unknown or missing cleanup_status is an absence of
// evidence, not evidence of risk, and it is unrecoverable on its own — the only
// route to a real value is a repair path the predicate itself refuses to run.
// When the git check succeeded, the git predicates are the real evidence and
// must decide. When it failed, there is no evidence and we must stay closed.
func TestUnknownCleanupStatusDefersToGitEvidence(t *testing.T) {
	for _, tc := range []struct {
		name        string
		in          WorkstateInput
		wantReuse   bool
		wantBlocker string
	}{
		{
			name:      "unknown cleanup + clean git = reusable",
			in:        WorkstateInput{State: StateDone, CleanupStatus: CleanupUnknown},
			wantReuse: true,
		},
		{
			name:      "missing cleanup + clean git = reusable",
			in:        WorkstateInput{State: StateDone, CleanupStatus: ""},
			wantReuse: true,
		},
		{
			name:        "unknown cleanup + dirty git = blocked by git, not by cleanup",
			in:          WorkstateInput{State: StateDone, CleanupStatus: CleanupUnknown, GitDirty: true},
			wantReuse:   false,
			wantBlocker: "git_state=has_uncommitted",
		},
		{
			name:        "unknown cleanup + stash = blocked",
			in:          WorkstateInput{State: StateDone, CleanupStatus: CleanupUnknown, StashCount: 1},
			wantReuse:   false,
			wantBlocker: "git_state=has_stash stash_count=1",
		},
		{
			name:        "unknown cleanup + unpushed = blocked",
			in:          WorkstateInput{State: StateDone, CleanupStatus: CleanupUnknown, UnpushedCommits: 2},
			wantReuse:   false,
			wantBlocker: "git_state=has_unpushed unpushed_commits=2",
		},
		{
			name:        "unknown cleanup + git check failed = stays closed, no evidence",
			in:          WorkstateInput{State: StateDone, CleanupStatus: CleanupUnknown, GitCheckFailed: true},
			wantReuse:   false,
			wantBlocker: "cleanup_status=unknown",
		},
		{
			name:        "a REAL unsafe cleanup status still blocks on its own",
			in:          WorkstateInput{State: StateDone, CleanupStatus: CleanupUncommitted},
			wantReuse:   false,
			wantBlocker: "cleanup_status=has_uncommitted",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := DecideWorkstate(tc.in)
			if got := len(d.Blockers) == 0; got != tc.wantReuse {
				t.Errorf("reusable = %v, want %v (blockers: %v, reason: %q)",
					got, tc.wantReuse, d.Blockers, d.Reason)
			}
			if tc.wantBlocker != "" {
				found := false
				for _, b := range d.Blockers {
					if b == tc.wantBlocker {
						found = true
					}
				}
				if !found {
					t.Errorf("want blocker %q, got %v", tc.wantBlocker, d.Blockers)
				}
			}
		})
	}
}
