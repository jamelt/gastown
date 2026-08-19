package cmd

import (
	"os"
	"path/filepath"
	"runtime"
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

	got := pickBestAgentBead(candidates)
	if got == nil || got.ID != "rig-wisp" {
		t.Fatalf("pickBestAgentBead picked %v, want rig-wisp", got)
	}
}

func TestPickBestAgentBeadSkipsClosed(t *testing.T) {
	candidates := []agentBeadCandidate{
		candidate("closed-rig-wisp", agentSourceRigWisps, "closed"),
		candidate("open-rig-issue", agentSourceRigIssues, "open"),
	}

	got := pickBestAgentBead(candidates)
	if got == nil || got.ID != "open-rig-issue" {
		t.Fatalf("pickBestAgentBead picked %v, want open-rig-issue", got)
	}
}

// TestPickBestAgentBeadCanonicalizesLegacyDuplicate reproduces the trader
// rig incident (gt-xjd): a legacy bead named "<rig>-<role>" from before the
// prefix-rig-role convention, and the canonical "<prefix>-<rig>-<role>" bead,
// both survive registration and match the same role/rig at the same source
// rank. Resolution must deterministically prefer the canonical-form ID
// instead of blocking await-signal with an ambiguity error.
func TestPickBestAgentBeadCanonicalizesLegacyDuplicate(t *testing.T) {
	candidates := []agentBeadCandidate{
		candidate("trader-refinery", agentSourceRigIssues, "open"),    // legacy, pre-prefix
		candidate("tr-trader-refinery", agentSourceRigIssues, "open"), // canonical
	}

	got := pickBestAgentBead(candidates)
	if got == nil || got.ID != "tr-trader-refinery" {
		t.Fatalf("pickBestAgentBead picked %v, want canonical tr-trader-refinery", got)
	}

	// Order independence: the canonicalization must not depend on input order.
	reversed := []agentBeadCandidate{candidates[1], candidates[0]}
	got = pickBestAgentBead(reversed)
	if got == nil || got.ID != "tr-trader-refinery" {
		t.Fatalf("pickBestAgentBead (reversed input) picked %v, want canonical tr-trader-refinery", got)
	}
}

// TestPickBestAgentBeadDeterministicWhenBothCanonical covers same-rank
// duplicates that both conform to the ID convention (e.g. a genuine
// double-registration race). Resolution still must not error — it falls
// back to lexicographic order so repeated calls agree.
func TestPickBestAgentBeadDeterministicWhenBothCanonical(t *testing.T) {
	candidates := []agentBeadCandidate{
		candidate("rig-wisp-b", agentSourceRigWisps, "open"),
		candidate("rig-wisp-a", agentSourceRigWisps, "open"),
	}

	got := pickBestAgentBead(candidates)
	if got == nil || got.ID != "rig-wisp-a" {
		t.Fatalf("pickBestAgentBead picked %v, want lexicographically first rig-wisp-a", got)
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

// TestHealRigSingletons_ReopensRigLocalCandidateInPlace covers gt-n99t: a
// Witness/Refinery singleton's rig-local record (see beads.Beads.ForLocalBeads)
// can be transiently closed (e.g. by the reaper) while a same-ID town-sourced
// bead stays open. healRigSingletons must reopen the rig-local record in
// place (mutating the slice element's Status), not return a detached copy —
// pickBestAgentBead reads straight from this same slice afterward.
func TestHealRigSingletons_ReopensRigLocalCandidateInPlace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mock bd script uses POSIX shell")
	}

	rigBeadsDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "bd.log")
	installAgentsResolveMockBD(t, logPath)

	matches := []agentBeadCandidate{
		candidate("gt-gastown-refinery", agentSourceTownIssues, "open"),
		{
			ID:       "gt-gastown-refinery",
			Source:   agentSourceRigIssues,
			BeadsDir: rigBeadsDir,
			Status:   "closed",
			Issue:    &beads.Issue{ID: "gt-gastown-refinery", Status: "closed"},
		},
	}

	healRigSingletons(matches, "refinery")

	if matches[1].Status != "open" {
		t.Fatalf("matches[1].Status = %q, want \"open\" after healing", matches[1].Status)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read bd log: %v", err)
	}
	logStr := string(logData)
	if !strings.Contains(logStr, "BEADS_DIR="+rigBeadsDir) {
		t.Fatalf("expected a bd invocation against rig BEADS_DIR=%s; log:\n%s", rigBeadsDir, logStr)
	}
	if !strings.Contains(logStr, "update gt-gastown-refinery --status=open") {
		t.Fatalf("expected an 'update ... --status=open' call; log:\n%s", logStr)
	}
}

// TestHealRigSingletons_ThenPickBestPrefersReopenedRigCandidate is the
// regression test for the bug a post-implementation review caught: calling
// the healer AFTER pickBestAgentBead reads a corrupted slice, because
// pickBestAgentBead's closed-candidate filter (`open := candidates[:0]`)
// overwrites the input slice's backing array in place. Healing must run
// BEFORE pickBestAgentBead so ranking sees the reopened status and picks the
// rig-local record over the town duplicate (gt-n99t).
func TestHealRigSingletons_ThenPickBestPrefersReopenedRigCandidate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mock bd script uses POSIX shell")
	}

	rigBeadsDir := t.TempDir()
	installAgentsResolveMockBD(t, filepath.Join(t.TempDir(), "bd.log"))

	matches := []agentBeadCandidate{
		{
			ID:       "gt-gastown-refinery",
			Source:   agentSourceRigIssues,
			BeadsDir: rigBeadsDir,
			Status:   "closed",
			Issue:    &beads.Issue{ID: "gt-gastown-refinery", Status: "closed"},
		},
		candidate("gt-gastown-refinery", agentSourceTownIssues, "open"),
	}

	healRigSingletons(matches, "refinery")
	got := pickBestAgentBead(matches)

	if got == nil {
		t.Fatal("pickBestAgentBead returned nil after healing")
	}
	if got.Source != agentSourceRigIssues {
		t.Fatalf("pickBestAgentBead picked source %v, want agentSourceRigIssues (the reopened rig-local record)", got.Source)
	}
}

// TestHealRigSingletons_IgnoresNonSingletonRoles verifies crew/polecat beads
// (legitimately closed forever once nuked/retired) are never auto-reopened —
// only Witness/Refinery are rig-local singletons.
func TestHealRigSingletons_IgnoresNonSingletonRoles(t *testing.T) {
	matches := []agentBeadCandidate{
		{
			ID:       "gt-gastown-polecat-shiny",
			Source:   agentSourceRigIssues,
			BeadsDir: t.TempDir(),
			Status:   "closed",
			Issue:    &beads.Issue{ID: "gt-gastown-polecat-shiny", Status: "closed"},
		},
	}
	healRigSingletons(matches, "polecat")
	if matches[0].Status != "closed" {
		t.Fatalf("matches[0].Status = %q, want unchanged \"closed\" for a non-singleton role", matches[0].Status)
	}
}

// TestHealRigSingletons_NoClosedRigCandidateIsNoop verifies the healer
// leaves an already-open town candidate untouched when there's nothing to
// reopen — resolve still falls through to its existing town-source error in
// that case.
func TestHealRigSingletons_NoClosedRigCandidateIsNoop(t *testing.T) {
	matches := []agentBeadCandidate{
		candidate("gt-gastown-refinery", agentSourceTownIssues, "open"),
	}
	healRigSingletons(matches, "refinery")
	if matches[0].Status != "open" {
		t.Fatalf("matches[0].Status = %q, want unchanged \"open\"", matches[0].Status)
	}
}

// installAgentsResolveMockBD installs a fake `bd` on PATH that logs the
// BEADS_DIR env var and full argument list per invocation to logPath and
// exits 0, so beads.Beads.Update() succeeds without touching a real Dolt
// server.
func installAgentsResolveMockBD(t *testing.T, logPath string) {
	t.Helper()
	binDir := t.TempDir()
	script := `#!/bin/sh
printf 'BEADS_DIR=%s ARGS=%s\n' "$BEADS_DIR" "$*" >> "` + logPath + `"
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake bd: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
