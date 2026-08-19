package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/polecat"
)

const polecatSessionKeySep = "\x00"

type polecatSessionSet map[string]string

type polecatInventoryItem struct {
	Rig            string
	Name           string
	State          polecat.State
	Issue          string
	CleanupStatus  string
	ActiveMR       string
	Branch         string
	SessionRunning bool
	SessionName    string
	// CleanupProvenance explains when legacy missing metadata was classified
	// from current read-only evidence instead of a persisted cleanup_status.
	CleanupProvenance string
	Disposition       polecat.WorkstateDisposition
}

const legacyCleanupReadOnlyProvenance = "legacy-missing:session-dormant+work-unassigned+worktree+hook+mr"

type polecatActiveWorkEvidence struct {
	BlocksCleanup        bool
	RequiresRestart      bool
	CountsTowardCapacity bool
	Blocker              string
	AssignedIssue        string
}

func newPolecatSessionSet(sessionNames []string) polecatSessionSet {
	sessions := make(polecatSessionSet, len(sessionNames))
	for _, sessionName := range sessionNames {
		rigName, polecatName, ok := parsePolecatSessionName(sessionName)
		if !ok {
			continue
		}
		sessions[polecatSessionKey(rigName, polecatName)] = sessionName
	}
	return sessions
}

func (s polecatSessionSet) lookup(rigName, polecatName string) (string, bool) {
	if s == nil {
		return "", false
	}
	sessionName, ok := s[polecatSessionKey(rigName, polecatName)]
	return sessionName, ok
}

func (s polecatSessionSet) namesForRig(rigName string) []string {
	if len(s) == 0 {
		return nil
	}
	var names []string
	for _, sessionName := range s {
		sessionRig, _, ok := parsePolecatSessionName(sessionName)
		if ok && sessionRig == rigName {
			names = append(names, sessionName)
		}
	}
	sort.Strings(names)
	return names
}

func polecatSessionKey(rigName, polecatName string) string {
	return rigName + polecatSessionKeySep + polecatName
}

// polecatMQIndex batches the merge-request and source-issue lookups that
// check-recovery makes per-polecat (FindMRForBranchAny, bd.Show) into two
// bulk fetches per rig, so gt polecat list / gt scheduler status can populate
// WorkstateInput's MQ fields without an O(N) per-polecat fan-out (gt-h6u4).
type polecatMQIndex struct {
	mrByBranch   map[string]*beads.Issue
	sourceIssues map[string]*beads.Issue
	lookupFailed bool
}

// polecatMQIndexSource is the subset of *beads.Beads that buildPolecatMQIndex
// needs. It lets tests fake the batched MR/source-issue lookups without a
// real bd binary, mirroring issueShower elsewhere in this package.
type polecatMQIndexSource interface {
	ListMergeRequests(opts beads.ListOptions) ([]*beads.Issue, error)
	ShowMultiple(ids []string) (map[string]*beads.Issue, error)
}

// buildPolecatMQIndex fetches all of a rig's merge-request beads once and all
// referenced source issues once (via ShowMultiple), instead of the per-polecat
// FindMRForBranchAny/bd.Show calls check-recovery uses. A fetch failure is
// recorded on the index (not returned as an error) so callers degrade the
// affected polecats to mq-lookup-failed/NEEDS_RECOVERY rather than aborting
// the whole inventory listing.
func buildPolecatMQIndex(bd polecatMQIndexSource, fieldsByName map[string]*beads.AgentFields) polecatMQIndex {
	idx := polecatMQIndex{mrByBranch: map[string]*beads.Issue{}, sourceIssues: map[string]*beads.Issue{}}
	if bd == nil {
		idx.lookupFailed = true
		return idx
	}

	mrs, err := bd.ListMergeRequests(beads.ListOptions{Status: "all", Label: "gt:merge-request"})
	if err != nil {
		idx.lookupFailed = true
	}
	for _, mr := range mrs {
		mrFields := beads.ParseMRFields(mr)
		if mrFields == nil || mrFields.Branch == "" {
			continue
		}
		if existing, ok := idx.mrByBranch[mrFields.Branch]; !ok || mr.CreatedAt > existing.CreatedAt {
			idx.mrByBranch[mrFields.Branch] = mr
		}
	}

	var sourceIDs []string
	seen := make(map[string]bool)
	for _, fields := range fieldsByName {
		sourceID := agentSourceIssueHint("", fields)
		if sourceID != "" && !seen[sourceID] {
			seen[sourceID] = true
			sourceIDs = append(sourceIDs, sourceID)
		}
	}
	if len(sourceIDs) > 0 {
		issues, err := bd.ShowMultiple(sourceIDs)
		if err != nil {
			idx.lookupFailed = true
		}
		idx.sourceIssues = issues
	}
	return idx
}

// applyMQIndexToWorkstateInput populates the merge-queue fields of a
// WorkstateInput from the batched polecatMQIndex instead of check-recovery's
// live per-polecat FindMRForBranchAny/bd.Show calls, so gt polecat list and
// gt scheduler status can reach NEEDS_MQ_SUBMIT (previously structurally
// unreachable — MQCheckRequired was never set) without a per-polecat
// fan-out (gt-h6u4).
//
// HasSubmittableWork is a conservative approximation from persisted/batched
// signals only — it does not run check-recovery's live git-diff check
// (hasSubmittableWorkForRecovery), which would need a live git-check fan-out
// across every polecat and was reviewed and rejected as a perf regression on
// the scheduler's heartbeat path. The approximation only ever biases toward
// "needs a check" (NEEDS_MQ_SUBMIT), never toward falsely claiming reusable.
// It must not fold in AssignedBeadTerminal: a closed source bead says nothing
// about whether the branch still carries pushed-but-unsubmitted work, and
// doing so let stranded work read as SAFE_TO_NUKE once the source bead
// closed (gt-6mhu).
func applyMQIndexToWorkstateInput(input *polecat.WorkstateInput, fields *beads.AgentFields, mq polecatMQIndex) {
	branch := strings.TrimSpace(input.Branch)
	if branch == "" {
		return
	}
	input.MQCheckRequired = true
	if mq.lookupFailed {
		input.MQLookupFailed = true
		return
	}
	if sourceIssue := mq.sourceIssues[agentSourceIssueHint("", fields)]; sourceIssue != nil {
		input.AssignedBeadTerminal = beads.IssueStatus(sourceIssue.Status).IsTerminal()
		if attachment := beads.ParseAttachmentFields(sourceIssue); attachment != nil {
			input.MQNotRequired = attachment.NoMerge || attachment.ReviewOnly ||
				strings.EqualFold(strings.TrimSpace(attachment.MergeStrategy), "local")
		}
	}
	_, input.MRSubmitted = mq.mrByBranch[branch]
	input.HasSubmittableWork = !input.PushFailed && !input.MRSubmitted
}

func buildPolecatInventoryItem(rigName, polecatName string, fields *beads.AgentFields, activeWork *beads.Issue, sessions polecatSessionSet, mq polecatMQIndex) polecatInventoryItem {
	return buildPolecatInventoryItemFromEvidenceWithBD(rigName, polecatName, fields, assessPolecatAssignedIssueWork(activeWork), sessions, mq, nil)
}

func buildPolecatInventoryItemFromEvidence(rigName, polecatName string, fields *beads.AgentFields, activeWorkEvidence polecatActiveWorkEvidence, sessions polecatSessionSet, mq polecatMQIndex) polecatInventoryItem {
	return buildPolecatInventoryItemFromEvidenceWithBD(rigName, polecatName, fields, activeWorkEvidence, sessions, mq, nil)
}

func buildPolecatInventoryItemFromEvidenceWithBD(rigName, polecatName string, fields *beads.AgentFields, activeWorkEvidence polecatActiveWorkEvidence, sessions polecatSessionSet, mq polecatMQIndex, bd issueShower) polecatInventoryItem {
	sessionName, running := sessions.lookup(rigName, polecatName)
	item := polecatInventoryItem{
		Rig:            rigName,
		Name:           polecatName,
		State:          polecat.StateIdle,
		SessionRunning: running,
		SessionName:    sessionName,
	}

	input := polecat.WorkstateInput{State: polecat.StateIdle}
	if fields != nil {
		item.CleanupStatus = strings.TrimSpace(fields.CleanupStatus)
		item.ActiveMR = strings.TrimSpace(fields.ActiveMR)
		item.Branch = strings.TrimSpace(fields.Branch)
		switch beads.AgentState(strings.TrimSpace(fields.AgentState)) {
		case beads.AgentStateDone:
			item.State = polecat.StateDone
		}
		input.CleanupStatus = polecat.CleanupStatus(item.CleanupStatus)
		input.PushFailed = fields.PushFailed
		input.MRFailed = fields.MRFailed
		input.Branch = item.Branch
		input.ActiveMR = item.ActiveMR
	}

	if !activeWorkEvidence.BlocksCleanup && fields != nil {
		activeWorkEvidence = assessPolecatAgentStateWork(beads.AgentState(strings.TrimSpace(fields.AgentState)))
	}

	if activeWorkEvidence.BlocksCleanup {
		item.Issue = activeWorkEvidence.AssignedIssue
		if activeWorkEvidence.RequiresRestart || activeWorkEvidence.CountsTowardCapacity {
			if running {
				item.State = polecat.StateWorking
			} else {
				item.State = polecat.StateStalled
			}
		} else if running && !polecat.CleanupStatus(item.CleanupStatus).IsSafe() {
			item.State = polecat.StateReviewNeeded
		}
		input.ActiveWorkBlocker = activeWorkEvidence.Blocker
		input.ActiveWorkCountsTowardCapacity = activeWorkEvidence.CountsTowardCapacity
	} else if item.State == polecat.StateIdle && running && !polecat.CleanupStatus(item.CleanupStatus).IsSafe() {
		item.State = polecat.StateReviewNeeded
	}

	if fields != nil && !activeWorkEvidence.BlocksCleanup {
		if hookBead := strings.TrimSpace(fields.HookBead); hookBead != "" {
			input.ActiveWorkBlocker = fmt.Sprintf("hook_bead=%s status=unverified", hookBead)
		}
	}
	if item.ActiveMR != "" {
		input.ActiveMRBlocker = "active_mr=" + item.ActiveMR + " status=unknown"
	}

	// Detect partial spawn case: agent_state=spawning with a hook_bead, but the
	// hook is not assigned to this polecat. This indicates a spawn operation that
	// completed its work but didn't establish a durable hook on the assigned bead.
	// When detected, we can ignore unknown cleanup_status and defer to live git/MR
	// evidence (the same logic check-recovery uses for this edge case).
	if fields != nil && !activeWorkEvidence.BlocksCleanup && item.Issue == "" {
		partialSpawnWithoutHook := computePartialSpawnWithoutDurableHook(bd, fields, rigName, polecatName, item.Issue)
		input.PartialSpawnWithoutDurableHook = partialSpawnWithoutHook
	}

	input.State = item.State
	applyMQIndexToWorkstateInput(&input, fields, mq)
	item.Disposition = polecat.DecideWorkstate(input)
	return item
}

// applyLegacyCleanupCompatibility replaces the metadata-only inventory verdict
// with a Manager assessment that gathered live hook, worktree, branch, and MR
// evidence. Only dormant, unassigned entries with an actual legacy agent bead
// are eligible. Missing beads, live sessions, and active work remain fail-closed.
func applyLegacyCleanupCompatibility(item polecatInventoryItem, fields *beads.AgentFields, activeWorkEvidence polecatActiveWorkEvidence, assessed polecat.WorkstateDisposition) polecatInventoryItem {
	if fields == nil || strings.TrimSpace(fields.CleanupStatus) != "" || item.SessionRunning || activeWorkEvidence.BlocksCleanup {
		return item
	}
	if item.State != polecat.StateIdle && item.State != polecat.StateDone {
		return item
	}
	item.Disposition = assessed
	item.CleanupProvenance = legacyCleanupReadOnlyProvenance
	return item
}

var polecatSummaryWorkStatuses = []beads.IssueStatus{
	beads.IssueStatusHooked,
	beads.StatusInProgress,
	beads.StatusOpen,
	beads.StatusBlocked,
	beads.StatusDeferred,
}

var polecatSummaryWorkStatusRank = func() map[string]int {
	ranks := make(map[string]int, len(polecatSummaryWorkStatuses))
	for i, status := range polecatSummaryWorkStatuses {
		ranks[string(status)] = i
	}
	return ranks
}()

func listActivePolecatWorkByName(bd *beads.Beads, rigName string) (map[string]*beads.Issue, error) {
	byName := make(map[string]*beads.Issue)
	issues, err := bd.ListIssueStatuses(polecatSummaryWorkStatuses...)
	if err != nil {
		return nil, err
	}
	for _, issue := range issues {
		evidence := assessPolecatAssignedIssueWork(issue)
		if !evidence.BlocksCleanup {
			continue
		}
		name, ok := polecatNameFromAssignee(rigName, issue.Assignee)
		if !ok {
			continue
		}
		if current := byName[name]; current == nil || polecatSummaryIssueRank(issue) < polecatSummaryIssueRank(current) {
			byName[name] = issue
		}
	}
	return byName, nil
}

func polecatSummaryIssueRank(issue *beads.Issue) int {
	if issue == nil {
		return len(polecatSummaryWorkStatuses)
	}
	if rank, ok := polecatSummaryWorkStatusRank[issue.Status]; ok {
		return rank
	}
	return len(polecatSummaryWorkStatuses)
}

func polecatNameFromAssignee(rigName, assignee string) (string, bool) {
	prefix := rigName + "/polecats/"
	if !strings.HasPrefix(assignee, prefix) {
		return "", false
	}
	name := strings.TrimPrefix(assignee, prefix)
	if name == "" || strings.Contains(name, "/") {
		return "", false
	}
	return name, true
}

func assessPolecatAssignedIssueWork(issue *beads.Issue) polecatActiveWorkEvidence {
	if issue == nil || beads.IsAgentBead(issue) || beads.IsProtectedBead(issue) || beads.IssueStatus(issue.Status).IsTerminal() {
		return polecatActiveWorkEvidence{}
	}
	requiresRestart := polecatSummaryIssueRequiresRestart(beads.IssueStatus(issue.Status))
	return polecatActiveWorkEvidence{
		BlocksCleanup:        true,
		RequiresRestart:      requiresRestart,
		CountsTowardCapacity: requiresRestart,
		Blocker:              fmt.Sprintf("assigned_work=%s status=%s", issue.ID, issue.Status),
		AssignedIssue:        issue.ID,
	}
}

func polecatSummaryIssueRequiresRestart(status beads.IssueStatus) bool {
	switch status {
	case beads.IssueStatusHooked, beads.StatusInProgress, beads.StatusOpen:
		return true
	default:
		return false
	}
}

func assessPolecatAgentStateWork(state beads.AgentState) polecatActiveWorkEvidence {
	if state == "" || state == beads.AgentStateIdle || state == beads.AgentStateDone || state == beads.AgentStateNuked {
		return polecatActiveWorkEvidence{}
	}
	if state.IsActive() {
		return polecatActiveWorkEvidence{
			BlocksCleanup:        true,
			RequiresRestart:      true,
			CountsTowardCapacity: true,
			Blocker:              fmt.Sprintf("agent_state=%s", state),
		}
	}
	if state.ProtectsFromCleanup() || state == beads.AgentStateEscalated {
		return polecatActiveWorkEvidence{
			BlocksCleanup: true,
			Blocker:       fmt.Sprintf("agent_state=%s", state),
		}
	}
	return polecatActiveWorkEvidence{}
}

func polecatActiveWorkLookupError(err error) polecatActiveWorkEvidence {
	if err == nil {
		return polecatActiveWorkEvidence{}
	}
	return polecatActiveWorkEvidence{
		BlocksCleanup: true,
		Blocker:       fmt.Sprintf("assigned_work status=lookup_error: %v", err),
	}
}

func parsePolecatAgentFields(issue *beads.Issue) *beads.AgentFields {
	if issue == nil {
		return nil
	}
	fields := beads.ParseAgentFields(issue.Description)
	fields.AgentState = beads.ResolveAgentState(issue.Description, issue.AgentState)
	return fields
}

// computePartialSpawnWithoutDurableHook mirrors the check-recovery logic for
// detecting when a polecat is in spawning state with a hook_bead that has moved
// to a different assignee. This indicates a partial spawn without a durable hook
// on the assigned bead, which allows us to ignore unknown cleanup_status.
func computePartialSpawnWithoutDurableHook(bd issueShower, fields *beads.AgentFields, rigName, polecatName, currentIssue string) bool {
	if bd == nil || fields == nil || fields.AgentState != "spawning" || fields.HookBead == "" || currentIssue != "" {
		return false
	}
	issue, err := bd.Show(fields.HookBead)
	if err != nil || issue == nil {
		return false
	}
	assignee := fmt.Sprintf("%s/polecats/%s", rigName, polecatName)
	if (issue.Status == beads.StatusHooked && issue.Assignee == assignee) || issue.Assignee == assignee {
		return false
	}
	return true
}
