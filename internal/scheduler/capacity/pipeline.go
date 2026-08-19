package capacity

import "strings"

// PendingBead represents a bead that is scheduled and ready for dispatch evaluation.
type PendingBead struct {
	ID              string // Context bead ID (sling context)
	WorkBeadID      string // The actual work bead ID
	Title           string
	TargetRig       string
	Description     string
	Labels          []string
	Context         *SlingContextFields // Parsed sling params from context bead
	ContextWorkDir  string              // Work dir for the DB where the context was discovered.
	ContextBeadsDir string              // Resolved .beads dir where the context was discovered.
}

// SlingContextFields holds scheduling parameters stored on a sling context bead.
// JSON-serialized as the context bead's description.
type SlingContextFields struct {
	Version          int    `json:"version"`
	WorkBeadID       string `json:"work_bead_id"`
	TargetRig        string `json:"target_rig"`
	EnqueuedBy       string `json:"enqueued_by,omitempty"`
	TargetAgent      string `json:"target_agent,omitempty"`
	Formula          string `json:"formula,omitempty"`
	Args             string `json:"args,omitempty"`
	Vars             string `json:"vars,omitempty"`
	EnqueuedAt       string `json:"enqueued_at"`
	Merge            string `json:"merge,omitempty"`
	Convoy           string `json:"convoy,omitempty"`
	BaseBranch       string `json:"base_branch,omitempty"`
	ResumeBranch     string `json:"resume_branch,omitempty"`
	BaseRef          string `json:"base_ref,omitempty"`
	PublishRemote    string `json:"publish_remote,omitempty"`
	PublishRef       string `json:"publish_ref,omitempty"`
	PRTargetRef      string `json:"pr_target_ref,omitempty"`
	NoMerge          bool   `json:"no_merge,omitempty"`
	ReviewOnly       bool   `json:"review_only,omitempty"`
	Account          string `json:"account,omitempty"`
	Agent            string `json:"agent,omitempty"`
	HookRawBead      bool   `json:"hook_raw_bead,omitempty"`
	Owned            bool   `json:"owned,omitempty"`
	Mode             string `json:"mode,omitempty"`
	DispatchFailures int    `json:"dispatch_failures,omitempty"`
	LastFailure      string `json:"last_failure,omitempty"`
}

// LabelSlingContext is the label used to identify sling context beads.
const LabelSlingContext = "gt:sling-context"

// LabelSchedulerCleared marks a work bead whose sling context was explicitly
// removed via `gt scheduler clear`. The feeder must never re-enqueue a bead
// carrying this label — only an explicit `gt sling` (which creates a fresh
// sling context directly, not through the feeder) may lift it. Without this
// marker, closing a sling context is indistinguishable from "never
// scheduled": the next feed cycle sees the bead as ready and unscheduled and
// recreates the context it was just cleared from (gt-5ti).
const LabelSchedulerCleared = "gt:scheduler-cleared"

// Labels that mark inter-agent messaging beads. These are never polecat work
// and must not be dispatched to rig polecats.
const (
	LabelMessage      = "gt:message"
	LabelHandoff      = "gt:handoff"
	LabelMergeRequest = "gt:merge-request"
)

// IsMessagingBead reports whether the bead is an inter-agent communication
// artifact rather than dispatchable work. Used as a defensive filter in the
// dispatch pipeline: a bead carrying any of these labels must never be handed
// to a polecat (gt-el4 / gastownhall/gastown#3800).
func IsMessagingBead(labels []string) bool {
	for _, l := range labels {
		switch l {
		case LabelMessage, LabelHandoff, LabelMergeRequest:
			return true
		}
	}
	return false
}

// FilterMessagingBeads removes messaging-labeled beads from the candidate slice.
// Returns the filtered slice plus the count of removed beads. Callers should
// log the skipped beads at debug level so the gap is observable.
func FilterMessagingBeads(beads []PendingBead) ([]PendingBead, int) {
	var result []PendingBead
	removed := 0
	for _, b := range beads {
		if IsMessagingBead(b.Labels) {
			removed++
			continue
		}
		result = append(result, b)
	}
	return result, removed
}

// DispatchPlan is the output of PlanDispatch — what to dispatch and why.
type DispatchPlan struct {
	ToDispatch []PendingBead
	Skipped    int
	Reason     string // "capacity" | "batch" | "ready" | "none"
}

// FailureAction indicates what to do after a dispatch failure.
type FailureAction int

const (
	// FailureRetry means the bead should be retried on the next cycle.
	FailureRetry FailureAction = iota
	// FailureQuarantine means the bead should be marked as permanently failed.
	FailureQuarantine
)

// ReadinessFilter is a function that filters pending beads to those ready for dispatch.
type ReadinessFilter func(pending []PendingBead) []PendingBead

// FailurePolicy is a function that determines what to do after N failures.
type FailurePolicy func(failures int) FailureAction

// AllReady is a ReadinessFilter that passes all beads through (no filtering).
func AllReady(pending []PendingBead) []PendingBead {
	return pending
}

// BlockerAware returns a ReadinessFilter that only passes beads whose WorkBeadID
// appears in the readyIDs set (i.e., beads whose work bead has no unresolved blockers).
func BlockerAware(readyIDs map[string]bool) ReadinessFilter {
	return func(pending []PendingBead) []PendingBead {
		var result []PendingBead
		for _, b := range pending {
			if readyIDs[b.WorkBeadID] {
				result = append(result, b)
			}
		}
		return result
	}
}

// PlanDispatch computes which beads to dispatch given capacity constraints.
// availableCapacity: free slots (positive = that many slots, <= 0 = no capacity).
// batchSize: max beads per cycle.
// ready: beads that passed readiness filtering.
//
// Messaging-labeled beads (gt:message / gt:handoff / gt:merge-request) are
// filtered out defensively before any capacity math runs. They are inter-agent
// communication artifacts and never dispatchable work; if any survived earlier
// filtering they must not reach a polecat (gt-el4).
func PlanDispatch(availableCapacity, batchSize int, ready []PendingBead) DispatchPlan {
	ready, msgSkipped := FilterMessagingBeads(ready)

	if len(ready) == 0 {
		if msgSkipped > 0 {
			return DispatchPlan{Skipped: msgSkipped, Reason: "messaging-filtered"}
		}
		return DispatchPlan{Reason: "none"}
	}

	// Exact polecat reservations reuse an already-running worker and therefore
	// do not consume a new capacity slot. Select them independently of free spawn
	// capacity while retaining the existing FIFO/capacity behavior for rig-only
	// contexts. This prevents a reserved recovery worker from waiting forever on
	// capacity that it already occupies.
	hasReservation := false
	for _, b := range ready {
		if b.Context != nil && b.Context.TargetAgent != "" {
			hasReservation = true
			break
		}
	}
	if hasReservation {
		remainingSlots := availableCapacity
		if remainingSlots < 0 {
			remainingSlots = 0
		}
		toDispatch := make([]PendingBead, 0, batchSize)
		for _, b := range ready {
			if len(toDispatch) >= batchSize {
				break
			}
			reserved := b.Context != nil && b.Context.TargetAgent != ""
			if !reserved && remainingSlots == 0 {
				continue
			}
			toDispatch = append(toDispatch, b)
			if !reserved {
				remainingSlots--
			}
		}
		if len(toDispatch) == 0 {
			return DispatchPlan{Skipped: len(ready) + msgSkipped, Reason: "capacity"}
		}
		return DispatchPlan{
			ToDispatch: toDispatch,
			Skipped:    len(ready) - len(toDispatch) + msgSkipped,
			Reason:     "reservation",
		}
	}

	if availableCapacity <= 0 {
		return DispatchPlan{
			Skipped: len(ready) + msgSkipped,
			Reason:  "capacity",
		}
	}

	// Dispatch up to the smallest of capacity, batchSize, and readyBeads count
	toDispatch := batchSize
	if availableCapacity < toDispatch {
		toDispatch = availableCapacity
	}
	if len(ready) < toDispatch {
		toDispatch = len(ready)
	}

	reason := "batch"
	if availableCapacity < batchSize && availableCapacity < len(ready) {
		reason = "capacity"
	}
	if len(ready) < batchSize && len(ready) < availableCapacity {
		reason = "ready"
	}

	skipped := len(ready) - toDispatch + msgSkipped
	if msgSkipped > 0 {
		reason = reason + "+messaging-filtered"
	}

	return DispatchPlan{
		ToDispatch: ready[:toDispatch],
		Skipped:    skipped,
		Reason:     reason,
	}
}

// NoRetryPolicy returns a FailurePolicy that always quarantines on first failure.
func NoRetryPolicy() FailurePolicy {
	return func(failures int) FailureAction {
		return FailureQuarantine
	}
}

// CircuitBreakerPolicy returns a FailurePolicy that retries up to maxFailures
// times, then quarantines.
func CircuitBreakerPolicy(maxFailures int) FailurePolicy {
	return func(failures int) FailureAction {
		if failures >= maxFailures {
			return FailureQuarantine
		}
		return FailureRetry
	}
}

// FilterCircuitBroken removes beads that have exceeded the maximum dispatch
// failures threshold. Returns the filtered list and the count of removed beads.
func FilterCircuitBroken(beads []PendingBead, maxFailures int) ([]PendingBead, int) {
	var result []PendingBead
	removed := 0
	for _, b := range beads {
		if b.Context != nil && b.Context.DispatchFailures >= maxFailures {
			removed++
			continue
		}
		result = append(result, b)
	}
	return result, removed
}

// DispatchParams captures what the scheduler needs to tell the dispatcher.
// Mirrors the relevant fields from cmd.SlingParams but is scheduler-owned.
type DispatchParams struct {
	BeadID        string
	FormulaName   string
	RigName       string
	TargetAgent   string
	Args          string
	Vars          []string
	Merge         string
	BaseBranch    string
	ResumeBranch  string
	BaseRef       string
	PublishRemote string
	PublishRef    string
	PRTargetRef   string
	Account       string
	Agent         string
	Mode          string
	NoMerge       bool
	ReviewOnly    bool
	HookRawBead   bool
	EnqueuedBy    string
}

// ReconstructFromContext builds DispatchParams from sling context fields.
func ReconstructFromContext(ctx *SlingContextFields) DispatchParams {
	p := DispatchParams{
		BeadID:        ctx.WorkBeadID,
		RigName:       ctx.TargetRig,
		TargetAgent:   ctx.TargetAgent,
		FormulaName:   ctx.Formula,
		Args:          ctx.Args,
		Merge:         ctx.Merge,
		BaseBranch:    ctx.BaseBranch,
		ResumeBranch:  ctx.ResumeBranch,
		BaseRef:       ctx.BaseRef,
		PublishRemote: ctx.PublishRemote,
		PublishRef:    ctx.PublishRef,
		PRTargetRef:   ctx.PRTargetRef,
		Account:       ctx.Account,
		Agent:         ctx.Agent,
		Mode:          ctx.Mode,
		NoMerge:       ctx.NoMerge,
		ReviewOnly:    ctx.ReviewOnly,
		HookRawBead:   ctx.HookRawBead,
		EnqueuedBy:    ctx.EnqueuedBy,
	}
	if ctx.Vars != "" {
		p.Vars = splitVars(ctx.Vars)
	}
	return p
}

// splitVars splits a newline-separated vars string into individual key=value pairs.
func splitVars(vars string) []string {
	if vars == "" {
		return nil
	}
	var result []string
	for _, line := range strings.Split(vars, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}
