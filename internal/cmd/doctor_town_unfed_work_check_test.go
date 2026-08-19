package cmd

import (
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

func TestEvaluateTownUnfedWork_FlagsDispatchableTypes(t *testing.T) {
	issues := []*beads.Issue{
		{ID: "hq-1", Type: "bug", Priority: 0, Title: "Real defect"},
		{ID: "hq-2", Type: "feature", Priority: 2, Title: "Nice to have"},
	}

	got := evaluateTownUnfedWork(issues)
	if len(got) != 2 {
		t.Fatalf("expected 2 flagged beads, got %d: %v", len(got), got)
	}
	if want := "hq-1 [P0 bug]: Real defect"; got[0] != want {
		t.Fatalf("detail mismatch:\n got:  %s\n want: %s", got[0], want)
	}
}

func TestEvaluateTownUnfedWork_SkipsNonDispatchableTypes(t *testing.T) {
	issues := []*beads.Issue{
		{ID: "hq-1s4w", Type: "directive", Priority: 0, Title: "Standing hard-prohibition policy"},
		{ID: "hq-30yt", Type: "record", Priority: 1, Title: "Coordination record"},
	}

	got := evaluateTownUnfedWork(issues)
	if len(got) != 0 {
		t.Fatalf("expected no flagged beads, got %v", got)
	}
}

func TestEvaluateTownUnfedWork_SkipsEpicsAndConvoys(t *testing.T) {
	issues := []*beads.Issue{
		{ID: "hq-cv-abc", Type: "task", Priority: 1, Title: "Convoy container"},
		{ID: "hq-epic1", Type: "epic", Priority: 1, Title: "Epic container"},
	}

	got := evaluateTownUnfedWork(issues)
	if len(got) != 0 {
		t.Fatalf("expected no flagged beads, got %v", got)
	}
}

func TestEvaluateTownUnfedWork_SkipsWispsAndEphemeral(t *testing.T) {
	issues := []*beads.Issue{
		{ID: "hq-wisp-xyz", Type: "task", Priority: 1, Title: "Wisp-shaped ID"},
		{ID: "hq-3", Type: "task", Priority: 1, Title: "Ephemeral", Ephemeral: true},
		{ID: "hq-4", Type: "wisp", Priority: 1, Title: "Internal wisp type"},
	}

	got := evaluateTownUnfedWork(issues)
	if len(got) != 0 {
		t.Fatalf("expected no flagged beads, got %v", got)
	}
}

func TestEvaluateTownUnfedWork_SkipsProtectedAndInternalLabels(t *testing.T) {
	issues := []*beads.Issue{
		{ID: "hq-role1", Type: "task", Priority: 1, Title: "Role bead mistyped as task", Labels: []string{"gt:role"}},
		{ID: "hq-queue1", Type: "task", Priority: 1, Title: "Queue bead mistyped as task", Labels: []string{"gt:queue"}},
	}

	got := evaluateTownUnfedWork(issues)
	if len(got) != 0 {
		t.Fatalf("expected no flagged beads, got %v", got)
	}
}

func TestEvaluateTownUnfedWork_MixedList(t *testing.T) {
	issues := []*beads.Issue{
		{ID: "hq-1", Type: "bug", Priority: 0, Title: "Real defect"},
		{ID: "hq-1s4w", Type: "directive", Priority: 0, Title: "Standing hard-prohibition policy"},
		{ID: "hq-epic1", Type: "epic", Priority: 0, Title: "Epic container"},
	}

	got := evaluateTownUnfedWork(issues)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 flagged bead, got %d: %v", len(got), got)
	}
	if want := "hq-1 [P0 bug]: Real defect"; got[0] != want {
		t.Fatalf("detail mismatch:\n got:  %s\n want: %s", got[0], want)
	}
}

func TestEvaluateTownUnfedWork_SortedByPriorityThenID(t *testing.T) {
	issues := []*beads.Issue{
		{ID: "hq-zzz", Type: "bug", Priority: 1, Title: "P1 bug"},
		{ID: "hq-aaa", Type: "chore", Priority: 0, Title: "P0 chore"},
		{ID: "hq-bbb", Type: "task", Priority: 0, Title: "P0 task"},
	}

	got := evaluateTownUnfedWork(issues)
	if len(got) != 3 {
		t.Fatalf("expected 3 flagged beads, got %d: %v", len(got), got)
	}
	if got[0][:6] != "hq-aaa" || got[1][:6] != "hq-bbb" || got[2][:6] != "hq-zzz" {
		t.Fatalf("expected P0 beads (hq-aaa, hq-bbb) before P1 bead (hq-zzz), got %v", got)
	}
}
