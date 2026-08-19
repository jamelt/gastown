package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/polecat"
	"github.com/steveyegge/gastown/internal/rig"
	"github.com/steveyegge/gastown/internal/style"
)

// polecatTarget represents a polecat to operate on.
type polecatTarget struct {
	rigName     string
	polecatName string
	mgr         *polecat.Manager
	r           *rig.Rig
}

// resolvePolecatTargets builds a list of polecats from command args.
// If useAll is true, the first arg is treated as a rig name and all polecats in it are returned.
// Otherwise, args are parsed as rig/polecat addresses.
func resolvePolecatTargets(args []string, useAll bool) ([]polecatTarget, error) {
	var targets []polecatTarget

	if useAll {
		// --all flag: first arg is just the rig name
		rigName := args[0]
		// Check if it looks like rig/polecat format
		if _, _, err := parseAddress(rigName); err == nil {
			return nil, fmt.Errorf("with --all, provide just the rig name (e.g., 'gt polecat <cmd> %s --all')", strings.Split(rigName, "/")[0])
		}

		mgr, r, err := getPolecatManager(rigName)
		if err != nil {
			return nil, err
		}

		polecats, err := mgr.List()
		if err != nil {
			return nil, fmt.Errorf("listing polecats: %w", err)
		}

		for _, p := range polecats {
			targets = append(targets, polecatTarget{
				rigName:     rigName,
				polecatName: p.Name,
				mgr:         mgr,
				r:           r,
			})
		}
	} else {
		// Multiple rig/polecat arguments - require explicit rig/polecat format
		for _, arg := range args {
			// Validate format: must contain "/" to avoid misinterpreting rig names as polecat names
			if !strings.Contains(arg, "/") {
				return nil, fmt.Errorf("invalid address '%s': must be in 'rig/polecat' format (e.g., 'gastown/Toast')", arg)
			}

			rigName, polecatName, err := parseAddress(arg)
			if err != nil {
				return nil, fmt.Errorf("invalid address '%s': %w", arg, err)
			}

			mgr, r, err := getPolecatManager(rigName)
			if err != nil {
				return nil, err
			}

			targets = append(targets, polecatTarget{
				rigName:     rigName,
				polecatName: polecatName,
				mgr:         mgr,
				r:           r,
			})
		}
	}

	return targets, nil
}

// SafetyCheckResult holds the result of safety checks for a polecat.
type SafetyCheckResult struct {
	Polecat       string
	Blocked       bool
	Reasons       []string
	CleanupStatus polecat.CleanupStatus
	HookBead      string
	HookStale     bool // true if hooked bead is closed
	ActiveMR      string
	OpenMR        string
	GitState      *GitState
<<<<<<< HEAD
	Verdict       string // mirrors RecoveryStatus.Verdict, e.g. NEEDS_MQ_SUBMIT
	MQStatus      string
}

// checkPolecatSafety performs safety checks before destructive operations.
// It shares the exact fact-gathering and policy classifier
// (computePolecatRecoveryStatus -> polecat.DecideWorkstate) that
// `gt polecat check-recovery`, `list`, and reuse selection use, so nuke can
// no longer disagree with what check-recovery reports as unsafe (gt-it1).
// The predicate check is state-agnostic (see recoveryStatusOptions.TreatStateAsIdle):
// nuking a stuck StateWorking/StateStalled polecat with no work at risk stays
// allowed, matching existing documented behavior.
//
// Returns nil if the polecat is safe to operate on, or a SafetyCheckResult with reasons if blocked.
func checkPolecatSafety(target polecatTarget) *SafetyCheckResult {
	polecatLabel := fmt.Sprintf("%s/%s", target.rigName, target.polecatName)

	status, err := computePolecatRecoveryStatus(target.mgr, target.r, target.rigName, target.polecatName, recoveryStatusOptions{
		TreatStateAsIdle: true,
	})
	if err != nil {
		// Fail closed: an unverifiable polecat is not provably safe to destroy.
		return &SafetyCheckResult{
			Polecat: polecatLabel,
			Reasons: []string{fmt.Sprintf("cannot verify safety: %v", err)},
			Blocked: true,
		}
=======

	// Fields below let dry-run display render from an already-computed result
	// instead of re-running the bd/git queries checkPolecatSafety just made.
	HasAgentBead        bool
	HasPolecatInfo      bool
	HasBranchInfo       bool
	GitCheckFailed      bool
	ActiveMRBlocked     bool
	ActiveMRBlockReason string
}

// safetyCheckContext holds rig-scoped data batch-fetched once so a scan over
// many polecats in the same rig doesn't reissue the same expensive Dolt
// queries per polecat. nil means "no shared context" — checkPolecatSafety
// falls back to its normal per-call bd queries.
type safetyCheckContext struct {
	agentBeads     map[string]*beads.Issue
	openMRByBranch map[string]*beads.Issue
}

// newSafetyCheckContext batch-fetches agent beads and open merge requests for
// a rig once. This is what makes `gt polecat nuke <rig> --all --dry-run`
// bounded on large/recovery-heavy rigs: without it, checkPolecatSafety issued
// a full agent-bead lookup and a full merge-request list scan per polecat,
// so total Dolt round trips grew with polecat count (gt-j2lu).
//
// Returns nil (callers fall back to per-polecat live queries) if either batch
// query fails. Safety-check correctness matters more than the optimization —
// a partially-populated context could silently misreport an existing open MR
// or hooked agent bead as absent, which would be an unsafe false-negative on
// a destructive operation.
func newSafetyCheckContext(bd *beads.Beads) *safetyCheckContext {
	agents, err := bd.ListAgentBeads()
	if err != nil {
		return nil
	}
	mrs, err := bd.ListMergeRequests(beads.ListOptions{Status: "all", Label: "gt:merge-request"})
	if err != nil {
		return nil
	}
	return &safetyCheckContext{
		agentBeads:     agents,
		openMRByBranch: indexOpenMRsByBranch(mrs),
	}
}

// buildSafetyCheckContexts builds one safetyCheckContext per distinct rig
// referenced by targets, so a safety scan issues O(rigs) batch queries
// instead of O(polecats) individual ones.
func buildSafetyCheckContexts(targets []polecatTarget) map[string]*safetyCheckContext {
	ctxByRig := make(map[string]*safetyCheckContext, len(targets))
	for _, t := range targets {
		if _, ok := ctxByRig[t.r.Path]; ok {
			continue
		}
		ctxByRig[t.r.Path] = newSafetyCheckContext(beads.New(t.r.Path))
	}
	return ctxByRig
}

// indexOpenMRsByBranch mirrors Beads.FindMRForBranch's matching rules (skip
// closed MRs, key on the "branch:" field) so a batch lookup returns the same
// answer as the per-call query it replaces.
func indexOpenMRsByBranch(issues []*beads.Issue) map[string]*beads.Issue {
	byBranch := make(map[string]*beads.Issue, len(issues))
	for _, issue := range issues {
		if issue.Status == "closed" {
			continue
		}
		fields := beads.ParseMRFields(issue)
		if fields == nil || fields.Branch == "" {
			continue
		}
		if _, exists := byBranch[fields.Branch]; !exists {
			byBranch[fields.Branch] = issue
		}
	}
	return byBranch
}

// lookupAgentBead resolves an agent bead either from the shared batch context
// or, when sharedCtx is nil, via a live bd call.
func lookupAgentBead(bd *beads.Beads, sharedCtx *safetyCheckContext, agentBeadID string) (*beads.Issue, *beads.AgentFields, error) {
	if sharedCtx == nil {
		return bd.GetAgentBead(agentBeadID)
	}
	issue, ok := sharedCtx.agentBeads[agentBeadID]
	if !ok {
		return nil, nil, nil
	}
	return issue, parsePolecatAgentFields(issue), nil
}

// lookupOpenMRForBranch resolves the open MR for a branch either from the
// shared batch context or, when sharedCtx is nil, via a live bd call.
func lookupOpenMRForBranch(bd *beads.Beads, sharedCtx *safetyCheckContext, branch string) (*beads.Issue, error) {
	if sharedCtx == nil {
		return bd.FindMRForBranch(branch)
	}
	return sharedCtx.openMRByBranch[branch], nil
}

// checkPolecatSafety performs safety checks before destructive operations.
// sharedCtx may be nil, in which case each check queries bd live; pass a
// context built by newSafetyCheckContext to reuse batch-fetched rig data
// across many calls (see buildSafetyCheckContexts).
// Returns nil if the polecat is safe to operate on, or a SafetyCheckResult with reasons if blocked.
func checkPolecatSafety(target polecatTarget, sharedCtx *safetyCheckContext) *SafetyCheckResult {
	result := &SafetyCheckResult{
		Polecat: fmt.Sprintf("%s/%s", target.rigName, target.polecatName),
>>>>>>> origin/main
	}

	result := safetyResultFromRecoveryStatus(polecatLabel, status)

	// Additional guard not covered by DecideWorkstate: an open MR bead
	// referencing this branch, independent of active_mr/MQ-submitted state.
	polecatInfo, infoErr := target.mgr.Get(target.polecatName)
<<<<<<< HEAD
	if infoErr == nil && polecatInfo != nil && polecatInfo.Branch != "" {
		bd := beads.New(target.r.Path)
		mr, mrErr := bd.FindMRForBranch(polecatInfo.Branch)
=======
	result.HasPolecatInfo = infoErr == nil && polecatInfo != nil
	if result.HasPolecatInfo {
		result.HasBranchInfo = polecatInfo.Branch != ""
	}

	// Check 1: Unpushed commits via cleanup_status or git state
	bd := beads.New(target.r.Path)
	agentBeadID := polecatBeadIDForRig(target.r, target.rigName, target.polecatName)
	agentIssue, fields, err := lookupAgentBead(bd, sharedCtx, agentBeadID)

	if err != nil || fields == nil {
		// No agent bead - fall back to git check
		result.HasAgentBead = false
		if infoErr == nil && polecatInfo != nil {
			gitState, gitErr := getGitState(polecatInfo.ClonePath)
			result.GitState = gitState
			if gitErr != nil {
				result.GitCheckFailed = true
				result.Reasons = append(result.Reasons, "cannot check git state")
			} else if !gitState.Clean {
				if gitState.DetachedHead {
					result.Reasons = append(result.Reasons, "detached HEAD has no branch custody target")
				} else if gitState.UnpushedCommits > 0 {
					result.Reasons = append(result.Reasons, fmt.Sprintf("has %d unpushed commit(s)", gitState.UnpushedCommits))
				} else if len(gitState.UncommittedFiles) > 0 {
					result.Reasons = append(result.Reasons, fmt.Sprintf("has %d uncommitted file(s)", len(gitState.UncommittedFiles)))
				} else if gitState.StashCount > 0 {
					result.Reasons = append(result.Reasons, fmt.Sprintf("has %d stash(es)", gitState.StashCount))
				} else {
					result.Reasons = append(result.Reasons, "live git state reports work at risk")
				}
			}
		}
	} else {
		result.HasAgentBead = true
		currentIssue := ""
		if infoErr == nil && polecatInfo != nil {
			currentIssue = polecatInfo.Issue
		}
		sourceHint := agentSourceIssueHint(currentIssue, fields)
		hookBead := agentHookBead(agentIssue, fields)
		var gitState *GitState
		gitStateLoaded := false
		loadGitState := func() {
			if gitStateLoaded || infoErr != nil || polecatInfo == nil {
				return
			}
			gitState, _ = getGitState(polecatInfo.ClonePath)
			result.GitState = gitState
			gitStateLoaded = true
		}
		// cleanup_status is advisory. Always inspect the live worktree so dry-run
		// and execution reject new risk discovered after a previous status report.
		loadGitState()
		if gitState == nil {
			result.Reasons = append(result.Reasons, "cannot check live git state")
		} else if gitState.DetachedHead {
			result.Reasons = append(result.Reasons, "detached HEAD has no branch custody target")
		} else if !gitState.Clean {
			switch {
			case gitState.UnpushedCommits > 0:
				result.Reasons = append(result.Reasons, fmt.Sprintf("has %d unpushed commit(s)", gitState.UnpushedCommits))
			case len(gitState.UncommittedFiles) > 0:
				result.Reasons = append(result.Reasons, fmt.Sprintf("has %d uncommitted file(s)", len(gitState.UncommittedFiles)))
			case gitState.StashCount > 0:
				result.Reasons = append(result.Reasons, fmt.Sprintf("has %d stash(es)", gitState.StashCount))
			default:
				result.Reasons = append(result.Reasons, "live git state reports work at risk")
			}
		}
		activeMRAssessment := polecat.ActiveMRAssessment{}
		if fields.ActiveMR != "" {
			loadGitState()
			gitSafe := false
			if polecatInfo != nil {
				gitSafe = activeMRGitSafeForWorktree(polecatInfo.ClonePath)
			}
			activeMRAssessment = polecat.AssessActiveMR(bd, polecat.ActiveMRInput{ActiveMR: fields.ActiveMR, SourceIssueHint: sourceHint, RequireGitSafe: true, GitSafe: gitSafe})
		}
		beadTerminal := isAssignedBeadTerminal(bd, sourceHint)
		if activeMRAssessment.SourceTerminal {
			beadTerminal = true
		}

		// Check cleanup_status from agent bead
		result.CleanupStatus = polecat.CleanupStatus(fields.CleanupStatus)
		switch result.CleanupStatus {
		case polecat.CleanupClean:
			// OK
		default:
			if result.CleanupStatus == polecat.CleanupUnpushed {
				loadGitState()
			}
			gitSafe := false
			if polecatInfo != nil {
				gitSafe = activeMRGitSafeForWorktree(polecatInfo.ClonePath)
			}
			hookSafe, hookTerminal, _ := hookBeadSafeForCleanup(bd, hookBead)
			activeMRSafe := !activeMRAssessment.Pending
			if polecat.CanIgnoreStaleCleanupStatus(result.CleanupStatus, beadTerminal || hookTerminal, hookSafe, activeMRSafe, gitSafe) {
				// OK: stale self-report after terminal source and direct clean git.
			} else {
				result.Reasons = append(result.Reasons, cleanupStatusBlocker(result.CleanupStatus))
			}
		}

		// Check 3: Work on hook
		if hookBead != "" {
			result.HookBead = hookBead
			// Check if hooked bead is still active (not closed)
			hookedIssue, err := bd.Show(hookBead)
			if err == nil && hookedIssue != nil {
				if hookedIssue.Status != "closed" {
					result.Reasons = append(result.Reasons, fmt.Sprintf("has work on hook (%s)", hookBead))
				} else {
					result.HookStale = true
				}
			} else {
				result.Reasons = append(result.Reasons, fmt.Sprintf("has work on hook (%s, unverified)", hookBead))
			}
		}

		if fields.ActiveMR != "" {
			result.ActiveMR = fields.ActiveMR
			if blocker := activeMRAssessment.Reason; activeMRAssessment.Pending && blocker != "" {
				result.ActiveMRBlocked = true
				result.ActiveMRBlockReason = blocker
				result.Reasons = append(result.Reasons, blocker)
			}
		}
	}

	// Check 2: Open MR beads for this branch
	if result.HasBranchInfo {
		mr, mrErr := lookupOpenMRForBranch(bd, sharedCtx, polecatInfo.Branch)
>>>>>>> origin/main
		if mrErr != nil {
			result.Reasons = append(result.Reasons, fmt.Sprintf("open_mr_lookup_error: %v", mrErr))
		} else if mr != nil {
			result.OpenMR = mr.ID
			result.Reasons = append(result.Reasons, fmt.Sprintf("has open MR (%s)", mr.ID))
		}
	}

	result.Blocked = len(result.Reasons) > 0
	return result
}

// safetyResultFromRecoveryStatus translates a computePolecatRecoveryStatus
// disposition into nuke's SafetyCheckResult shape. Pulled out as a pure
// function so the exact wiring that closes gt-it1 — a NEEDS_MQ_SUBMIT/
// NEEDS_RECOVERY/PENDING_MR verdict must block — is unit-testable without a
// live beads/git backend.
func safetyResultFromRecoveryStatus(polecatLabel string, status RecoveryStatus) *SafetyCheckResult {
	result := &SafetyCheckResult{
		Polecat:       polecatLabel,
		CleanupStatus: status.CleanupStatus,
		HookBead:      status.HookBead,
		HookStale:     status.HookStale,
		ActiveMR:      status.ActiveMR,
		Verdict:       status.Verdict,
		MQStatus:      status.MQStatus,
	}
	for _, blocker := range status.Blockers {
		result.Reasons = append(result.Reasons, blocker)
	}
	result.Blocked = len(result.Reasons) > 0
	return result
}

func rigPrefix(r *rig.Rig) string {
	townRoot := filepath.Dir(r.Path)
	return beads.GetPrefixForRig(townRoot, r.Name)
}

func polecatBeadIDForRig(r *rig.Rig, rigName, polecatName string) string {
	return beads.PolecatBeadIDWithPrefix(rigPrefix(r), rigName, polecatName)
}

// displaySafetyCheckBlocked prints blocked polecats and guidance.
func displaySafetyCheckBlocked(blocked []*SafetyCheckResult) {
	displaySafetyCheckBlockedTo(os.Stderr, blocked)
}

func displaySafetyCheckBlockedTo(w io.Writer, blocked []*SafetyCheckResult) {
	fmt.Fprintf(w, "%s Cannot nuke the following polecats:\n\n", style.Error.Render("Error:"))
	var polecatList []string
	for _, b := range blocked {
		fmt.Fprintf(w, "  %s:\n", style.Bold.Render(b.Polecat))
		for _, r := range b.Reasons {
			fmt.Fprintf(w, "    - %s\n", r)
		}
		polecatList = append(polecatList, b.Polecat)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Safety checks failed. Resolve issues before nuking, or use --force.")
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "  1. Complete work: gt done (from polecat session)")
	fmt.Fprintln(w, "  2. Push changes: git push (from polecat worktree)")
	fmt.Fprintln(w, "  3. Escalate: gt mail send mayor/ -s \"RECOVERY_NEEDED\" -m \"...\"")
	fmt.Fprintf(w, "  4. Force nuke (LOSES WORK): gt polecat nuke --force %s\n", strings.Join(polecatList, " "))
	fmt.Fprintln(w)
}

func formatSafetyCheckBlockers(blocked []*SafetyCheckResult) string {
	parts := make([]string, 0, len(blocked))
	for _, b := range blocked {
		parts = append(parts, fmt.Sprintf("%s: %s", b.Polecat, strings.Join(b.Reasons, "; ")))
	}
	return strings.Join(parts, " | ")
}

<<<<<<< HEAD
// displayDryRunSafetyCheck shows safety check status for dry-run mode. It
// returns true when a normal nuke would refuse.
//
// Renders entirely off checkPolecatSafety's result — the same disposition
// that gates a real nuke — rather than an independent set of bd/git lookups.
// Before gt-it1 this function re-derived cleanup status, hook state, active
// MR, and open MR from scratch, so dry-run's checklist could silently drift
// from what nuke actually enforced; now there is exactly one place that
// decides "is this safe" and one place that reads the answer.
func displayDryRunSafetyCheck(target polecatTarget) bool {
	fmt.Printf("\n  Safety checks:\n")
	result := checkPolecatSafety(target)

	if result.CleanupStatus == "" {
		fmt.Printf("    - Cleanup status: %s\n", style.Dim.Render("unknown (no agent bead)"))
	} else if result.CleanupStatus.IsSafe() {
		fmt.Printf("    - Cleanup status: %s\n", style.Success.Render(string(result.CleanupStatus)))
	} else if result.CleanupStatus.RequiresRecovery() {
		fmt.Printf("    - Cleanup status: %s\n", style.Error.Render(string(result.CleanupStatus)))
	} else {
		fmt.Printf("    - Cleanup status: %s\n", style.Warning.Render(string(result.CleanupStatus)))
	}

	switch {
	case result.HookBead == "":
		fmt.Printf("    - Hook: %s\n", style.Success.Render("empty"))
	case result.HookStale:
		fmt.Printf("    - Hook: %s (%s, closed - stale)\n", style.Warning.Render("stale"), result.HookBead)
	default:
		fmt.Printf("    - Hook: %s (%s)\n", style.Error.Render("has work"), result.HookBead)
	}

	if result.ActiveMR != "" {
		if activeMRReasonBlocks(result.Reasons) {
			fmt.Printf("    - Active MR: %s (%s)\n", style.Error.Render("blocked"), result.ActiveMR)
		} else {
			fmt.Printf("    - Active MR: %s (%s)\n", style.Success.Render("terminal"), result.ActiveMR)
		}
	}

	if result.OpenMR != "" {
		fmt.Printf("    - Open MR: %s (%s)\n", style.Error.Render("yes"), result.OpenMR)
	} else {
		fmt.Printf("    - Open MR: %s\n", style.Success.Render("none"))
	}

	// Merge-queue submission status — from the same classifier nuke's actual
	// safety gate uses, so dry-run shows the real reason for a block even
	// when it's MQ-related (gt-it1: this predicate has no equivalent among
	// the ad hoc checks this function used to run on its own).
	if result.Verdict == "NEEDS_MQ_SUBMIT" {
		fmt.Printf("    - MQ status: %s (%s)\n", style.Error.Render("not submitted"), result.MQStatus)
	} else if result.MQStatus != "" {
		fmt.Printf("    - MQ status: %s (%s)\n", style.Success.Render("ok"), result.MQStatus)
=======
// displayDryRunSafetyCheck shows safety check status for dry-run mode,
// rendered entirely from an already-computed result. It returns true when a
// normal nuke would refuse.
//
// This takes a *SafetyCheckResult instead of a polecatTarget so a bulk
// `--all` dry-run scan can compute checkPolecatSafety once per polecat and
// reuse it here, instead of recomputing the same bd/git queries a second
// time purely for display (gt-j2lu).
func displayDryRunSafetyCheck(result *SafetyCheckResult) bool {
	fmt.Printf("\n  Safety checks:\n")

	// Check 1: cleanup status or fallback git state
	if !result.HasAgentBead {
		switch {
		case !result.HasPolecatInfo:
			fmt.Printf("    - Git state: %s\n", style.Dim.Render("unknown (no polecat info)"))
		case result.GitCheckFailed:
			fmt.Printf("    - Git state: %s\n", style.Warning.Render("cannot check"))
		case result.GitState != nil && result.GitState.Clean:
			fmt.Printf("    - Git state: %s\n", style.Success.Render("clean"))
		default:
			fmt.Printf("    - Git state: %s\n", style.Error.Render("dirty"))
		}
		fmt.Printf("    - Hook: %s\n", style.Dim.Render("unknown (no agent bead)"))
	} else {
		if result.CleanupStatus.IsSafe() {
			fmt.Printf("    - Cleanup status: %s\n", style.Success.Render(string(result.CleanupStatus)))
		} else if result.CleanupStatus.RequiresRecovery() {
			fmt.Printf("    - Cleanup status: %s\n", style.Error.Render(string(result.CleanupStatus)))
		} else {
			statusText := string(result.CleanupStatus)
			if statusText == "" {
				statusText = "<missing>"
			}
			fmt.Printf("    - Cleanup status: %s\n", style.Warning.Render(statusText))
		}

		if result.HookBead == "" {
			fmt.Printf("    - Hook: %s\n", style.Success.Render("empty"))
		} else if result.HookStale {
			fmt.Printf("    - Hook: %s (%s, closed - stale)\n", style.Warning.Render("stale"), result.HookBead)
		} else {
			fmt.Printf("    - Hook: %s (%s)\n", style.Error.Render("has work"), result.HookBead)
		}

		if result.ActiveMR != "" {
			if result.ActiveMRBlocked {
				fmt.Printf("    - Active MR: %s (%s)\n", style.Error.Render("blocked"), result.ActiveMRBlockReason)
			} else {
				fmt.Printf("    - Active MR: %s (%s)\n", style.Success.Render("terminal"), result.ActiveMR)
			}
		}
	}

	// Check 2: Open MR
	if !result.HasBranchInfo {
		fmt.Printf("    - Open MR: %s\n", style.Dim.Render("unknown (no branch info)"))
	} else if result.OpenMR != "" {
		fmt.Printf("    - Open MR: %s (%s)\n", style.Error.Render("yes"), result.OpenMR)
	} else {
		fmt.Printf("    - Open MR: %s\n", style.Success.Render("none"))
>>>>>>> origin/main
	}

	return result.Blocked
}

// activeMRReasonBlocks reports whether an active-MR predicate ("active_mr=…")
// appears among a safety check's blocking reasons.
func activeMRReasonBlocks(reasons []string) bool {
	for _, r := range reasons {
		if strings.HasPrefix(r, "active_mr=") {
			return true
		}
	}
	return false
}
