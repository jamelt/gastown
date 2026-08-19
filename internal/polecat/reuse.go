package polecat

import (
	"errors"
	"fmt"
)

// ErrPolecatNeedsRecovery marks an idle-looking polecat that must not be reset
// or advertised as reusable until its preserved work is recovered or submitted.
var ErrPolecatNeedsRecovery = errors.New("polecat needs recovery before reuse")

// SlotReuseInput is the shared input for deciding whether a polecat slot can be
// advertised as open and destructively reused for new work.
type SlotReuseInput struct {
	State                State
	HookBead             string
	CleanupStatus        CleanupStatus
	IgnoreCleanupStatus  bool
	PushFailed           bool
	MRFailed             bool
	Branch               string
	GitDirty             bool
	GitDirtyReason       string
	StashCount           int
	UnpushedCommits      int
	GitCheckFailed       bool
	GitCheckFailedReason string
	ActiveMR             string
	ActiveMRBlocker      string
	MQCheckRequired      bool
	HasSubmittableWork   bool
	MQNotRequired        bool
	AssignedBeadTerminal bool
	MRSubmitted          bool
	MQLookupFailed       bool
}

// SlotReuseDecision explains whether a polecat can be reused and why not.
type SlotReuseDecision struct {
	Reusable bool
	Reason   string
}

// DecideSlotReuse is the single source of truth for reuse safety. It fails
// closed: unknown cleanup/git state means the slot needs recovery, not reuse.
func DecideSlotReuse(in SlotReuseInput) SlotReuseDecision {
	d := DecideWorkstate(WorkstateInput{
		State:                in.State,
		HookBead:             in.HookBead,
		CleanupStatus:        in.CleanupStatus,
		IgnoreCleanupStatus:  in.IgnoreCleanupStatus,
		PushFailed:           in.PushFailed,
		MRFailed:             in.MRFailed,
		Branch:               in.Branch,
		GitDirty:             in.GitDirty,
		GitDirtyReason:       in.GitDirtyReason,
		StashCount:           in.StashCount,
		UnpushedCommits:      in.UnpushedCommits,
		GitCheckFailed:       in.GitCheckFailed,
		GitCheckFailedReason: in.GitCheckFailedReason,
		ActiveMR:             in.ActiveMR,
		ActiveMRBlocker:      in.ActiveMRBlocker,
		MQCheckRequired:      in.MQCheckRequired,
		HasSubmittableWork:   in.HasSubmittableWork,
		MQNotRequired:        in.MQNotRequired,
		AssignedBeadTerminal: in.AssignedBeadTerminal,
		MRSubmitted:          in.MRSubmitted,
		MQLookupFailed:       in.MQLookupFailed,
	})
	return SlotReuseDecision{Reusable: d.Reusable, Reason: d.Reason}
}

// WorktreeResetSafety is live filesystem evidence about a polecat worktree,
// gathered immediately before a destructive reset. Each *Err field records that
// the corresponding lookup failed, so the decision can fail closed rather than
// treating an unknown as a zero.
type WorktreeResetSafety struct {
	Dirty       bool
	DirtyErr    bool
	StashCount  int
	StashErr    bool
	Unpushed    int
	UnpushedErr bool
}

// DecideWorktreeReset reports whether `git reset --hard` + `git clean -f` may be
// run on a polecat worktree.
//
// This is deliberately INDEPENDENT of DecideWorkstate/DecideSlotReuse. Those
// derive Reusable from bead metadata (cleanup_status, MR history, hook state),
// and gt-nasl showed that a metadata change alone — a rejected MR bead appearing
// for the branch — can flip Reusable to true and thereby authorize destruction of
// a worktree nobody has looked at. Reuse eligibility and permission-to-destroy are
// two different claims; conflating them is what made "automatic nuke on rejection"
// (forbidden by gt-d5c8) reachable without any human step.
//
// So the destructive step gets its own verdict, computed from live git state at
// the moment of the reset, and fails closed: if we cannot prove the worktree holds
// nothing unrecoverable, we refuse. Refusing is cheap — the caller falls back to
// allocating a fresh polecat — while being wrong destroys work.
func DecideWorktreeReset(in WorktreeResetSafety) SlotReuseDecision {
	switch {
	case in.DirtyErr:
		return SlotReuseDecision{Reason: "cannot determine uncommitted state"}
	case in.Dirty:
		return SlotReuseDecision{Reason: "uncommitted changes present"}
	case in.StashErr:
		return SlotReuseDecision{Reason: "cannot determine stash state"}
	case in.StashCount > 0:
		return SlotReuseDecision{Reason: fmt.Sprintf("%d stash entr(y|ies) present", in.StashCount)}
	case in.UnpushedErr:
		return SlotReuseDecision{Reason: "cannot determine unpushed commits"}
	case in.Unpushed > 0:
		return SlotReuseDecision{Reason: fmt.Sprintf("%d unpushed commit(s)", in.Unpushed)}
	}
	return SlotReuseDecision{Reusable: true}
}
