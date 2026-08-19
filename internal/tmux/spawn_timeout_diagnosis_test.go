package tmux

import (
	"errors"
	"strings"
	"testing"
)

// TestTimeoutDiagnosis_DistinguishesNeverCapturableFromSlowPrompt guards the
// gt-zpfn fix: "timeout waiting for runtime prompt" used to be a bare string
// with no way to tell whether the tmux pane never came up at all (consistent
// with resource pressure preventing the spawn) from a pane that came up but
// never printed the ready prompt (agent process alive but stuck/slow).
func TestTimeoutDiagnosis_DistinguishesNeverCapturableFromSlowPrompt(t *testing.T) {
	t.Run("pane never capturable", func(t *testing.T) {
		got := timeoutDiagnosis("gastown-toast", nil, errors.New("can't find pane"), 5, 5)
		if !strings.Contains(got, "never capturable") {
			t.Fatalf("expected diagnosis to call out the pane as never capturable, got: %q", got)
		}
		if !strings.Contains(got, "resource pressure") {
			t.Fatalf("expected diagnosis to hint at resource pressure as a cause, got: %q", got)
		}
	})

	t.Run("pane capturable but no ready prompt", func(t *testing.T) {
		lines := []string{"Loading context...", "Priming 400 files", "still working"}
		got := timeoutDiagnosis("gastown-toast", lines, nil, 5, 0)
		if !strings.Contains(got, "still working") {
			t.Fatalf("expected diagnosis to include the pane's last observed output, got: %q", got)
		}
		if strings.Contains(got, "never capturable") {
			t.Fatalf("must not claim the pane was never capturable when captures succeeded, got: %q", got)
		}
	})

	t.Run("no output at all", func(t *testing.T) {
		got := timeoutDiagnosis("gastown-toast", nil, nil, 5, 0)
		if !strings.Contains(got, "no output") {
			t.Fatalf("expected diagnosis to call out empty output, got: %q", got)
		}
	})
}
