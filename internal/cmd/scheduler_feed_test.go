package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

func TestFeedSkipReason(t *testing.T) {
	tests := []struct {
		name  string
		issue *beads.Issue
		want  string
		skip  bool
	}{
		{
			name:  "ordinary ready task is fed",
			issue: &beads.Issue{ID: "gt-1", Title: "Fix the widget", Status: "open"},
			skip:  false,
		},
		{
			name:  "deferred status is skipped",
			issue: &beads.Issue{ID: "gt-2", Title: "Some task", Status: "deferred"},
			want:  "deferred",
			skip:  true,
		},
		{
			name:  "deferred description keyword is skipped",
			issue: &beads.Issue{ID: "gt-3", Title: "Some task", Status: "open", Description: "Deferred to post-launch."},
			want:  "deferred",
			skip:  true,
		},
		{
			name:  "epic type is skipped",
			issue: &beads.Issue{ID: "gt-4", Title: "Big epic", Status: "open", Type: "epic"},
			want:  "epic/convoy container, not dispatchable work",
			skip:  true,
		},
		{
			name:  "gt:convoy label is skipped",
			issue: &beads.Issue{ID: "gt-5", Title: "A convoy", Status: "open", Labels: []string{"gt:convoy"}},
			want:  "epic/convoy container, not dispatchable work",
			skip:  true,
		},
		{
			name:  "human decision title is skipped",
			issue: &beads.Issue{ID: "gt-6", Title: "Human decision for opencode feature", Status: "open"},
			want:  "requires human decision",
			skip:  true,
		},
		{
			name:  "human decision description phrase is skipped",
			issue: &beads.Issue{ID: "gt-7", Title: "Route selection", Status: "open", Description: "This requires human approval before proceeding."},
			want:  "requires human decision",
			skip:  true,
		},
		{
			name:  "gt:needs-human label is skipped",
			issue: &beads.Issue{ID: "gt-8", Title: "Some task", Status: "open", Labels: []string{"gt:needs-human"}},
			want:  "requires human decision",
			skip:  true,
		},
		{
			name:  "mismatched platform label is skipped",
			issue: &beads.Issue{ID: "gt-9", Title: "macOS gate capture", Status: "open", Labels: []string{"gt:platform:darwin"}},
			skip:  true, // exact reason string depends on runtime.GOOS, checked separately below
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reason, skip := feedSkipReason(tc.issue)
			if skip != tc.skip {
				t.Fatalf("feedSkipReason(%+v) skip = %v, want %v (reason=%q)", tc.issue, skip, tc.skip, reason)
			}
			if tc.want != "" && reason != tc.want {
				t.Fatalf("feedSkipReason(%+v) reason = %q, want %q", tc.issue, reason, tc.want)
			}
		})
	}
}

func TestPlatformIncompatibleNoOpWithoutLabel(t *testing.T) {
	issue := &beads.Issue{ID: "gt-1", Title: "Ordinary task", Status: "open"}
	if _, incompatible := platformIncompatible(issue); incompatible {
		t.Fatal("expected no platform incompatibility without a gt:platform label")
	}
}

func TestPlatformIncompatibleMatchingHostIsFed(t *testing.T) {
	// A label matching the current runtime is not a skip reason.
	issue := &beads.Issue{ID: "gt-1", Title: "Ordinary task", Status: "open", Labels: []string{"gt:platform:" + runtime.GOOS}}
	if _, incompatible := platformIncompatible(issue); incompatible {
		t.Fatal("expected no platform incompatibility when label matches host GOOS")
	}
}

func TestIsEpicOrConvoyIssue(t *testing.T) {
	if !isEpicOrConvoyIssue(&beads.Issue{Type: "epic"}) {
		t.Error("expected type=epic to be detected")
	}
	if !isEpicOrConvoyIssue(&beads.Issue{Type: "convoy"}) {
		t.Error("expected type=convoy to be detected")
	}
	if !isEpicOrConvoyIssue(&beads.Issue{Labels: []string{"gt:epic"}}) {
		t.Error("expected gt:epic label to be detected")
	}
	if isEpicOrConvoyIssue(&beads.Issue{Type: "task"}) {
		t.Error("expected ordinary task to not be detected as epic/convoy")
	}
}

// setupDirectDispatchTown creates a minimal town with no scheduler config,
// which means direct-dispatch mode (shouldDeferDispatch returns false).
func setupDirectDispatchTown(t *testing.T) string {
	t.Helper()
	townRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(townRoot, "mayor"), 0755); err != nil {
		t.Fatalf("mkdir mayor: %v", err)
	}
	return townRoot
}

func TestRunSchedulerFeedNoOpInDirectDispatchMode(t *testing.T) {
	townRoot := setupDirectDispatchTown(t)
	oldCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(townRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldCWD) })

	result, err := runSchedulerFeed(townRoot, defaultFeederMaxPerRig, false)
	if err != nil {
		t.Fatalf("runSchedulerFeed: %v", err)
	}
	if result.Deferred {
		t.Fatalf("result.Deferred = true, want false (direct-dispatch mode has no queue to feed)")
	}
	if result.Fed != 0 || len(result.Decisions) != 0 {
		t.Fatalf("result = %+v, want zero-value fed/decisions in direct-dispatch mode", result)
	}
}
