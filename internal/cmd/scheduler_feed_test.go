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
			name:  "hq-cv- ID prefix is skipped even without type/label",
			issue: &beads.Issue{ID: "hq-cv-abc123", Title: "A convoy without a synced label", Status: "open"},
			want:  "epic/convoy container, not dispatchable work",
			skip:  true,
		},
		{
			name:  "human decision title is skipped",
			issue: &beads.Issue{ID: "gt-6", Title: "Human decision for opencode feature", Status: "open"},
			want:  "title indicates human decision required",
			skip:  true,
		},
		{
			name:  "human decision description phrase is skipped",
			issue: &beads.Issue{ID: "gt-7", Title: "Route selection", Status: "open", Description: "This requires human approval before proceeding."},
			want:  "description indicates human decision required",
			skip:  true,
		},
		{
			name:  "gt:needs-human label is skipped",
			issue: &beads.Issue{ID: "gt-8", Title: "Some task", Status: "open", Labels: []string{"gt:needs-human"}},
			want:  "gt:needs-human label",
			skip:  true,
		},
		{
			name:  "mismatched platform label is skipped",
			issue: &beads.Issue{ID: "gt-9", Title: "macOS gate capture", Status: "open", Labels: []string{"gt:platform:darwin"}},
			skip:  true, // exact reason string depends on runtime.GOOS, checked separately below
		},
		{
			name:  "blocked status is skipped even without a dependency edge",
			issue: &beads.Issue{ID: "gt-10", Title: "Some task", Status: "blocked"},
			want:  "status: blocked",
			skip:  true,
		},
		// Regression fixtures harvested from the live trader queue 2026-08-19
		// by the mayor (mail hq-wisp-m1p25, bead comments on gt-j3xq). These
		// are the concrete counterexamples that motivated gating on labels
		// instead of title/description keywords. Labels here simulate the
		// batch bd-show fetch runSchedulerFeed does before calling this
		// function — bd ready --json itself never populates Labels.
		{
			name: "trader-obs-d9: credentials bead with innocuous title must NOT be fed",
			issue: &beads.Issue{
				ID:          "trader-obs-d9",
				Title:       "Put deploy-only pager secrets in the normal recoverable secret store",
				Description: "Back up the ops webhook secret and watchdog ping URL wherever other deploy credentials are custodied.",
				Status:      "open",
				Labels:      []string{"area:observability", "area:security", "human", "program:observability", "source:D9"},
			},
			skip: true, // reason is one of the matched labels; map iteration order isn't fixed, so not asserted exactly
		},
		{
			name: "trader-lt8e: alarming title but genuinely safe UI bead MUST be fed",
			issue: &beads.Issue{
				ID:          "trader-lt8e",
				Title:       "Repair legacy production acknowledgement migration cleanup",
				Description: "Clean up the stale trader.production-confirm.acknowledged localStorage key in useEnvironmentStore. Verify with a vitest run.",
				Status:      "open",
				Labels:      []string{"area:testing", "area:ui"},
			},
			skip: false,
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

// TestEffectiveIssueForFeedAppliesBatchLabels guards the exact wiring bug
// the mayor caught in ab117c05 (mail hq-wisp-m1p25): bd ready --json never
// populates Labels, so feedSkipReason's hard-prohibition checks are a
// silent no-op unless the batch bd-show fetch is actually overlaid before
// the eligibility check runs.
func TestEffectiveIssueForFeedAppliesBatchLabels(t *testing.T) {
	readyIssue := &beads.Issue{
		ID:     "trader-obs-d9",
		Title:  "Put deploy-only pager secrets in the normal recoverable secret store",
		Status: "open",
		// Labels intentionally empty: this is what bd ready --json actually
		// returns for every issue, credentials beads included.
	}

	// Without the overlay, the label the whole check depends on is invisible.
	if reason, skip := feedSkipReason(readyIssue); skip {
		t.Fatalf("expected the un-overlaid ready issue (no labels, as bd ready returns) to be fed, got skip=true reason=%q", reason)
	}

	labelsByID := map[string]beadStatusInfo{
		"trader-obs-d9": {Labels: []string{"area:observability", "area:security", "human", "program:observability", "source:D9"}},
	}
	effective := effectiveIssueForFeed(readyIssue, labelsByID)
	if len(effective.Labels) == 0 {
		t.Fatal("effectiveIssueForFeed did not apply the batch-fetched labels")
	}
	if reason, skip := feedSkipReason(effective); !skip {
		t.Fatalf("expected the label-overlaid credentials bead to be skipped, got fed (reason=%q)", reason)
	}

	// A bead with no batch-fetch entry (e.g. the bd show call failed) must
	// fall back to whatever bd ready returned rather than panicking or
	// wiping out data that was present.
	fallback := effectiveIssueForFeed(readyIssue, nil)
	if fallback.ID != readyIssue.ID {
		t.Fatalf("effectiveIssueForFeed with no batch entry = %+v, want unchanged issue", fallback)
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
	if !isEpicOrConvoyIssue(&beads.Issue{ID: "hq-cv-xyz"}) {
		t.Error("expected hq-cv- ID prefix to be detected")
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
