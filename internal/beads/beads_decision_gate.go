package beads

import (
	"fmt"
	"strconv"
	"strings"
)

// Decision-bead lifecycle guard (gt-v0kz).
//
// A decision bead is not a work item. It exists to hold a pending human
// ruling, and its terminal state is that ruling — not an agent deciding it
// has nothing left to do.
//
// Observed twice out of two council runs: a council posted COUNCIL_LENS v2,
// COUNCIL_CONVERGENCE v1 and DECISION_CARD v2 to gt-a7o5 and gt-vfoq, and the
// polecat running it then closed each bead with "Completed with no code
// changes (already fixed or already merged)". That is the correct verdict for
// a code task and the wrong one here. Closing drops the bead out of the
// human-gate queue, so the card — the output of the most expensive operation
// in the system, four Opus lenses plus a chair — became invisible to the only
// person able to act on it. In one of those cases the buried card carried a
// durability hard gate reporting a live data-loss defect.
//
// The guard refuses to close a bead that has a DECISION_CARD v2 with no
// corresponding HUMAN_DECISION v1. It deliberately applies even under
// --force: burying a pending human decision is precisely the harm force would
// otherwise cause, and no agent has standing to waive the overseer's ruling.
// Once the human has ruled, closure proceeds normally.

const (
	decisionCardMarker  = "DECISION_CARD v2"
	humanDecisionMarker = "HUMAN_DECISION v1"
)

// ErrDecisionAwaitingHuman reports an attempt to close a decision bead whose
// card has not yet been ruled on.
type ErrDecisionAwaitingHuman struct {
	IssueID     string
	CardVersion int
}

func (e *ErrDecisionAwaitingHuman) Error() string {
	return fmt.Sprintf(
		"refusing to close %s: it holds a %s (version %d) with no %s.\n"+
			"A decision bead is not a work item — it holds a pending human ruling, and closing it "+
			"removes the card from the human-gate queue where the overseer would see it.\n"+
			"Closure is allowed once the human responds and a %s is recorded (the dashboard writes "+
			"one, or record the ruling directly). If this bead is genuinely not a decision, remove "+
			"its %s comment rather than closing over it.",
		e.IssueID, decisionCardMarker, e.CardVersion, humanDecisionMarker,
		humanDecisionMarker, decisionCardMarker)
}

// markerOf reports which decision-protocol marker a comment body opens with,
// or empty for ordinary chatter. Matching is anchored to the start so a
// comment merely *discussing* a marker — as this guard's own bead notes do —
// is never mistaken for the record itself.
func markerOf(text string) string {
	trimmed := strings.TrimSpace(text)
	for _, marker := range []string{decisionCardMarker, humanDecisionMarker} {
		if trimmed == marker ||
			strings.HasPrefix(trimmed, marker+"\n") ||
			strings.HasPrefix(trimmed, marker+" ") {
			return marker
		}
	}
	return ""
}

// cardVersionOf reads decision_card_version from a marker body, defaulting to
// 0 when absent so a card and ruling that both omit it still pair up.
func cardVersionOf(text string) int {
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok || strings.TrimSpace(key) != "decision_card_version" {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			return n
		}
	}
	return 0
}

// decisionAwaitingHuman reports whether these comments show a decision card
// that has not been ruled on. A ruling for the same card version clears it; a
// ruling for an older version does not, since a newer card supersedes it and
// needs its own answer.
func decisionAwaitingHuman(comments []Comment) (bool, int) {
	latestCard := -1
	ruled := map[int]bool{}
	for _, c := range comments {
		switch markerOf(c.Text) {
		case decisionCardMarker:
			latestCard = cardVersionOf(c.Text)
		case humanDecisionMarker:
			ruled[cardVersionOf(c.Text)] = true
		}
	}
	if latestCard < 0 {
		return false, 0
	}
	return !ruled[latestCard], latestCard
}

// guardDecisionBeads returns an error if any id holds an unruled decision
// card. Comment lookup failures are never treated as permission to close:
// this guard exists precisely because closure silently destroyed value, so it
// fails closed and says why.
func (b *Beads) guardDecisionBeads(ids ...string) error {
	for _, id := range ids {
		comments, err := b.Comments(id)
		if err != nil {
			return fmt.Errorf(
				"refusing to close %s: could not read its comments to check for a pending "+
					"human decision (%w). Retry, or verify the bead holds no unruled %s",
				id, err, decisionCardMarker)
		}
		if awaiting, version := decisionAwaitingHuman(comments); awaiting {
			return &ErrDecisionAwaitingHuman{IssueID: id, CardVersion: version}
		}
	}
	return nil
}
