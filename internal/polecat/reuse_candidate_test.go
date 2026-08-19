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
	} {
		if got := isReuseCandidateState(tc.state); got != tc.want {
			t.Errorf("isReuseCandidateState(%s) = %v, want %v (%s)", tc.state, got, tc.want, tc.why)
		}
	}
}

// The candidate filter must stay consistent with DecideWorkstate, which lets
// exactly these two states fall through to the real reuse predicates. If one
// side is changed without the other, polecats become either unreusable (the
// gt-nz5x outage) or reusable without being vetted.
func TestReuseCandidateStatesMatchWorkstateFallthrough(t *testing.T) {
	for _, s := range []State{StateIdle, StateDone, StateWorking, StateStalled, StateReviewNeeded} {
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
