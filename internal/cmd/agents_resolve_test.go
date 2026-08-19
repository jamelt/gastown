package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

func TestResolveAgentsPrimaryBeadsDirUsesExplicitRigFromTownAndRigContexts(t *testing.T) {
	townRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(townRoot, "mayor"),
		filepath.Join(townRoot, ".beads"),
		filepath.Join(townRoot, "gastown", ".beads"),
		filepath.Join(townRoot, "gastown", "mayor", "rig"),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(townRoot, "mayor", "town.json"), []byte(`{"name":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(townRoot, ".beads", "routes.jsonl"), []byte(`{"prefix":"gt-","path":"gastown"}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rigBeads := filepath.Join(townRoot, "gastown", ".beads")
	for _, cwd := range []string{townRoot, filepath.Join(townRoot, "gastown", "mayor", "rig")} {
		got, err := resolveAgentsPrimaryBeadsDir(cwd, filepath.Join(townRoot, ".beads"), "gastown")
		if err != nil {
			t.Fatalf("cwd %s: %v", cwd, err)
		}
		if filepath.Clean(got) != filepath.Clean(rigBeads) {
			t.Fatalf("cwd %s resolved %s, want %s", cwd, got, rigBeads)
		}
	}
}

func TestAgentBeadMatchesDescriptionAndIDFallback(t *testing.T) {
	tests := []struct {
		name  string
		issue *beads.Issue
		role  string
		rig   string
		want  bool
	}{
		{
			name: "description matches legacy random wisp ID",
			issue: &beads.Issue{
				ID:          "au-wisp-0ti",
				Description: "Agent\n\nrole_type: refinery\nrig: alleago_ui",
			},
			role: "refinery",
			rig:  "alleago_ui",
			want: true,
		},
		{
			name: "canonical ID fallback matches sparse wisp metadata",
			issue: &beads.Issue{
				ID: "gt-gastown-witness",
			},
			role: "witness",
			rig:  "gastown",
			want: true,
		},
		{
			name: "collapsed prefix-rig ID fallback matches sparse metadata",
			issue: &beads.Issue{
				ID: "cp-refinery",
			},
			role: "refinery",
			rig:  "cp",
			want: true,
		},
		{
			name: "role mismatch",
			issue: &beads.Issue{
				ID:          "gt-gastown-witness",
				Description: "Agent\n\nrole_type: witness\nrig: gastown",
			},
			role: "refinery",
			rig:  "gastown",
			want: false,
		},
		{
			name: "rig mismatch",
			issue: &beads.Issue{
				ID:          "gt-gastown-refinery",
				Description: "Agent\n\nrole_type: refinery\nrig: gastown",
			},
			role: "refinery",
			rig:  "other",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := agentBeadMatches(tt.issue, tt.role, tt.rig)
			if got != tt.want {
				t.Fatalf("agentBeadMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPickBestAgentBead(t *testing.T) {
	candidates := []agentBeadCandidate{
		candidate("town-issue", agentSourceTownIssues, "open"),
		candidate("rig-issue", agentSourceRigIssues, "open"),
		candidate("town-wisp", agentSourceTownWisps, "open"),
		candidate("rig-wisp", agentSourceRigWisps, "open"),
	}

	got, err := pickBestAgentBead(candidates)
	if err != nil {
		t.Fatalf("pickBestAgentBead returned error: %v", err)
	}
	if got == nil || got.ID != "rig-wisp" {
		t.Fatalf("pickBestAgentBead picked %v, want rig-wisp", got)
	}
}

func TestPickBestAgentBeadSkipsClosed(t *testing.T) {
	candidates := []agentBeadCandidate{
		candidate("closed-rig-wisp", agentSourceRigWisps, "closed"),
		candidate("open-rig-issue", agentSourceRigIssues, "open"),
	}

	got, err := pickBestAgentBead(candidates)
	if err != nil {
		t.Fatalf("pickBestAgentBead returned error: %v", err)
	}
	if got == nil || got.ID != "open-rig-issue" {
		t.Fatalf("pickBestAgentBead picked %v, want open-rig-issue", got)
	}
}

func TestPickBestAgentBeadRejectsSameRankDuplicates(t *testing.T) {
	candidates := []agentBeadCandidate{
		candidate("rig-wisp-a", agentSourceRigWisps, "open"),
		candidate("rig-wisp-b", agentSourceRigWisps, "open"),
		candidate("rig-issue", agentSourceRigIssues, "open"),
	}

	got, err := pickBestAgentBead(candidates)
	if err == nil {
		t.Fatalf("pickBestAgentBead picked %v, want duplicate error", got)
	}
	if !strings.Contains(err.Error(), "multiple matching agent beads") {
		t.Fatalf("error = %q, want duplicate diagnostic", err)
	}
}

func candidate(id string, source agentBeadSource, status string) agentBeadCandidate {
	return agentBeadCandidate{
		ID:     id,
		Source: source,
		Status: status,
		Issue:  &beads.Issue{ID: id, Status: status},
	}
}
