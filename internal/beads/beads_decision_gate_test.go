package beads

import (
	"strings"
	"testing"
)

// Reproduces the exact gt-a7o5 / gt-vfoq sequence: a council posts its
// artifacts, then the polecat running it closes the bead as "no code
// changes". Both real occurrences buried a decision card the overseer had not
// yet ruled on.
func TestDecisionAwaitingHuman_CouncilPostedButNotRuled(t *testing.T) {
	comments := []Comment{
		{Author: "gastown/polecats/brahmin", Text: "COUNCIL_LENS v2 durability-blast-radius\nHARD GATE"},
		{Author: "gastown/polecats/brahmin", Text: "COUNCIL_CONVERGENCE v1\nconverged"},
		{Author: "gastown/polecats/brahmin", Text: "DECISION_CARD v2\ndecision_card_version: 2\nRecommendation: B"},
	}
	awaiting, version := decisionAwaitingHuman(comments)
	if !awaiting {
		t.Fatal("a posted card with no human ruling must block closure")
	}
	if version != 2 {
		t.Fatalf("card version = %d, want 2", version)
	}
}

func TestDecisionAwaitingHuman_ClearedByRuling(t *testing.T) {
	comments := []Comment{
		{Text: "DECISION_CARD v2\ndecision_card_version: 2\n"},
		{Text: "HUMAN_DECISION v1\ndecision_card_version: 2\naction: ACCEPT\napprover: Jamel Toms\n"},
	}
	if awaiting, _ := decisionAwaitingHuman(comments); awaiting {
		t.Fatal("a ruled card must not block closure")
	}
}

// A newer card supersedes an older ruling and needs its own answer —
// otherwise a stale ACCEPT would silently authorize closing a revised
// decision the human never saw.
func TestDecisionAwaitingHuman_StaleRulingDoesNotClearNewerCard(t *testing.T) {
	comments := []Comment{
		{Text: "DECISION_CARD v2\ndecision_card_version: 1\n"},
		{Text: "HUMAN_DECISION v1\ndecision_card_version: 1\naction: ACCEPT\n"},
		{Text: "DECISION_CARD v2\ndecision_card_version: 2\n"},
	}
	awaiting, version := decisionAwaitingHuman(comments)
	if !awaiting {
		t.Fatal("a revised card must require its own ruling")
	}
	if version != 2 {
		t.Fatalf("version = %d, want the latest card (2)", version)
	}
}

// Ordinary beads must close normally. The guard is narrow by construction:
// no card, no interference.
func TestDecisionAwaitingHuman_IgnoresOrdinaryBeads(t *testing.T) {
	for _, comments := range [][]Comment{
		nil,
		{{Text: "MR created: gt-wisp-q19"}},
		{{Text: "fixed the thing"}, {Text: "rebased cleanly"}},
	} {
		if awaiting, _ := decisionAwaitingHuman(comments); awaiting {
			t.Fatalf("non-decision bead wrongly blocked: %+v", comments)
		}
	}
}

// A comment that merely mentions the marker mid-text is not the record. This
// bead's own notes discuss DECISION_CARD v2 in prose; matching loosely would
// lock beads that hold no card at all.
func TestMarkerOf_AnchoredToStart(t *testing.T) {
	if got := markerOf("the council posts a DECISION_CARD v2 when it converges"); got != "" {
		t.Fatalf("prose mention matched as marker: %q", got)
	}
	if got := markerOf("DECISION_CARD v2\nbody"); got != decisionCardMarker {
		t.Fatalf("real marker not matched: %q", got)
	}
	if got := markerOf("  HUMAN_DECISION v1  \naction: ACCEPT"); got != humanDecisionMarker {
		t.Fatalf("leading whitespace broke matching: %q", got)
	}
}

// A card and ruling that both omit the version still pair, so the guard does
// not lock beads produced before versions were emitted.
func TestCardVersionDefaultsPairUp(t *testing.T) {
	comments := []Comment{
		{Text: "DECISION_CARD v2\nRecommendation: B\n"},
		{Text: "HUMAN_DECISION v1\naction: ACCEPT\n"},
	}
	if awaiting, _ := decisionAwaitingHuman(comments); awaiting {
		t.Fatal("unversioned card and ruling must pair up")
	}
}

func TestErrDecisionAwaitingHumanExplainsRemedy(t *testing.T) {
	err := &ErrDecisionAwaitingHuman{IssueID: "gt-a7o5", CardVersion: 2}
	msg := err.Error()
	for _, want := range []string{"gt-a7o5", decisionCardMarker, humanDecisionMarker} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message omits %q: %s", want, msg)
		}
	}
	// The refusal must say what would allow closure, not merely refuse.
	if !strings.Contains(msg, "once the human responds") {
		t.Errorf("error does not state the remedy: %s", msg)
	}
}
