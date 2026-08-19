package cmd

import (
	"errors"
	"fmt"
	"testing"
)

// gt-c2lp: a respawn-limit block must be distinguishable from a transient
// dispatch error. It is latched state that persists until an operator runs
// `gt sling respawn-reset`, so it warrants escalation; ordinary failures do not.
func TestIsRespawnLimitError(t *testing.T) {
	// The real message produced by internal/cmd/polecat_spawn.go.
	real := fmt.Errorf("sling failed: failed to spawn polecat: respawn limit reached for gt-mqcr (3 attempts). "+
		"This bead keeps failing — investigate before re-dispatching.\nReset: gt sling respawn-reset %s", "gt-mqcr")

	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"real respawn-limit error", real, true},
		{"wrapped respawn-limit error", fmt.Errorf("dispatching: %w", real), true},
		{"directory cap is NOT a respawn limit", errors.New("rig gastown has 30 polecat directories (max 30)"), false},
		{"transient spawn failure", errors.New("timeout waiting for runtime prompt"), false},
		{"cross-rig prefix refusal", errors.New("cross-rig prefix dispatch refused"), false},
		{"nil", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRespawnLimitError(tc.err); got != tc.want {
				t.Fatalf("isRespawnLimitError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
