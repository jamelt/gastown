package polecat

import "testing"

// gt-nasl: the destructive reset must be gated on live git evidence, independent
// of the metadata-derived Reusable verdict. These cases pin the fail-closed
// behaviour — an unknown must never be read as "safe".
func TestDecideWorktreeReset(t *testing.T) {
	tests := []struct {
		name    string
		in      WorktreeResetSafety
		wantOK  bool
		wantWhy string
	}{
		{"clean worktree is resettable", WorktreeResetSafety{}, true, ""},
		{"uncommitted changes block", WorktreeResetSafety{Dirty: true}, false, "uncommitted changes present"},
		{"stashes block", WorktreeResetSafety{StashCount: 2}, false, "2 stash entr(y|ies) present"},
		{"unpushed commits block", WorktreeResetSafety{Unpushed: 3}, false, "3 unpushed commit(s)"},

		// Fail-closed: a failed lookup is not evidence of safety.
		{"dirty lookup failure blocks", WorktreeResetSafety{DirtyErr: true}, false, "cannot determine uncommitted state"},
		{"stash lookup failure blocks", WorktreeResetSafety{StashErr: true}, false, "cannot determine stash state"},
		{"unpushed lookup failure blocks", WorktreeResetSafety{UnpushedErr: true}, false, "cannot determine unpushed commits"},

		// Precedence: the first unsafe signal is reported, and a later clean
		// reading never overrides an earlier failure.
		{"dirty takes precedence over clean stash/unpushed",
			WorktreeResetSafety{Dirty: true, StashCount: 0, Unpushed: 0}, false, "uncommitted changes present"},
		{"lookup failure is not masked by clean readings",
			WorktreeResetSafety{DirtyErr: true, StashCount: 0, Unpushed: 0}, false, "cannot determine uncommitted state"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideWorktreeReset(tt.in)
			if got.Reusable != tt.wantOK {
				t.Fatalf("Reusable = %v, want %v (reason %q)", got.Reusable, tt.wantOK, got.Reason)
			}
			if !tt.wantOK && got.Reason != tt.wantWhy {
				t.Fatalf("Reason = %q, want %q", got.Reason, tt.wantWhy)
			}
		})
	}
}

// The regression this fixes: metadata alone must not authorize destruction.
// DecideSlotReuse can legitimately return Reusable=true from bead state while the
// worktree still holds unrecoverable work; the reset gate must still refuse.
func TestWorktreeResetIndependentOfSlotReuse(t *testing.T) {
	slot := DecideSlotReuse(SlotReuseInput{State: StateIdle, CleanupStatus: CleanupClean})
	if !slot.Reusable {
		t.Fatalf("precondition: expected a clean idle slot to be reusable, got %q", slot.Reason)
	}
	// Same polecat, but the worktree has unpushed work on disk.
	if d := DecideWorktreeReset(WorktreeResetSafety{Unpushed: 1}); d.Reusable {
		t.Fatal("reset gate approved destruction of a worktree with unpushed commits despite slot being reusable")
	}
}
