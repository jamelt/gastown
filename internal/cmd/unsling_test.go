package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestUnslingDeadSessionTargetDoesNotRequireLivePane is a regression test for
// gt-dgrq: gt unsling <bead> <target> used to fail resolving a target agent
// whose tmux session was already dead, because it round-tripped through
// resolveTargetAgent (which needs a live pane for pane/hookRoot values
// unsling never uses). Before the fix, this failed unconditionally with
// "resolving target agent: getting pane for ...: exit status 1" — before any
// town or beads lookup ran. After the fix, target-agent resolution no longer
// touches tmux, so any failure here comes from a later, unrelated step —
// never a pane error.
func TestUnslingDeadSessionTargetDoesNotRequireLivePane(t *testing.T) {
	t.Chdir(t.TempDir())

	err := runUnslingWith(&cobra.Command{}, []string{"testrig/polecats/ghost"}, true, false)
	if err != nil && strings.Contains(err.Error(), "getting pane for") {
		t.Fatalf("unsling still requires a live tmux pane to resolve target agent: %v", err)
	}
}
