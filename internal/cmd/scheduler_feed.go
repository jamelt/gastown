package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/constants"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/rig"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/workspace"
)

// defaultFeederMaxPerRig is the conservative default cap on how many ready
// beads the feeder schedules per rig per cycle (gt-j3xq: "start conservative").
const defaultFeederMaxPerRig = 2

var (
	schedulerFeedJSON      bool
	schedulerFeedDryRun    bool
	schedulerFeedMaxPerRig int
)

var schedulerFeedCmd = &cobra.Command{
	Use:   "feed",
	Short: "Survey ready beads across rigs and schedule eligible work into the queue",
	Long: `Feed connects 'bd ready' to the scheduler.

Without this command, the scheduler only dispatches work that has already
been explicitly scheduled (via 'gt sling' or 'gt scheduler run'). Nothing
surveys ready work and pulls it into the queue when capacity frees up, so
dispatch runs only as long as a human keeps hand-slinging beads.

Feed surveys ready beads across every rig, skips ineligible ones (already
scheduled, blocked, deferred, epics/convoys, beads requiring a human
decision, beads requiring a platform this host doesn't have), and schedules
up to --max-per-rig eligible beads per rig, bounded by town-wide free
polecat capacity. Every decision — fed or skipped, and why — is logged.

Feed only acts when the scheduler is in deferred-dispatch mode
(scheduler.max_polecats > 0); in direct-dispatch mode there is no queue to
feed.

  gt scheduler feed              # Feed eligible ready beads
  gt scheduler feed --dry-run    # Preview without scheduling
  gt scheduler feed --json       # Machine-readable decision log`,
	RunE: runSchedulerFeedCmd,
}

func init() {
	schedulerFeedCmd.Flags().BoolVar(&schedulerFeedJSON, "json", false, "Output as JSON")
	schedulerFeedCmd.Flags().BoolVar(&schedulerFeedDryRun, "dry-run", false, "Preview what would be fed without scheduling")
	schedulerFeedCmd.Flags().IntVar(&schedulerFeedMaxPerRig, "max-per-rig", 0, "Override max beads fed per rig per cycle (0 = default 2)")
	schedulerCmd.AddCommand(schedulerFeedCmd)
}

// feedDecision records what the feeder did with one ready bead, or a
// rig-level failure, during a feed cycle. Recording skips (not just feeds)
// is deliberate: silent skipping is how 194 stale beads and a stalled rig
// went unnoticed before.
type feedDecision struct {
	ID     string `json:"id,omitempty"`
	Rig    string `json:"rig"`
	Action string `json:"action"` // "fed" or "skipped"
	Reason string `json:"reason"`
}

// feedResult is the outcome of one feed cycle.
type feedResult struct {
	Deferred  bool           `json:"deferred_mode"`
	Fed       int            `json:"fed"`
	Decisions []feedDecision `json:"decisions,omitempty"`
	Reason    string         `json:"reason,omitempty"` // set on a town-wide no-op (not deferred, or no capacity)
}

func runSchedulerFeedCmd(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return err
	}

	maxPerRig := schedulerFeedMaxPerRig
	if maxPerRig <= 0 {
		maxPerRig = defaultFeederMaxPerRig
	}

	result, err := runSchedulerFeed(townRoot, maxPerRig, schedulerFeedDryRun)
	if err != nil {
		return err
	}

	if schedulerFeedJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	printFeedResultHuman(result)
	return nil
}

func printFeedResultHuman(result feedResult) {
	if !result.Deferred {
		fmt.Printf("%s scheduler is in direct-dispatch mode, nothing to feed\n", style.Dim.Render("○"))
		return
	}
	if result.Reason != "" {
		fmt.Printf("%s %s\n", style.Dim.Render("○"), result.Reason)
	}
	for _, d := range result.Decisions {
		switch d.Action {
		case "fed":
			fmt.Printf("%s %s -> %s (%s)\n", style.Success.Render("✓"), d.ID, d.Rig, d.Reason)
		default:
			label := d.ID
			if label == "" {
				label = d.Rig
			}
			fmt.Printf("%s %s: %s\n", style.Dim.Render("○"), label, d.Reason)
		}
	}
	fmt.Printf("\n%s\n", style.Bold.Render(fmt.Sprintf("Fed %d bead(s)", result.Fed)))
}

// runSchedulerFeed surveys ready beads across every rig and schedules
// eligible ones into the deferred-dispatch queue, up to maxPerRig per rig
// and bounded by town-wide free polecat capacity. It is idempotent and safe
// to run on a timer: already-scheduled beads are skipped via scheduleBead's
// own idempotency check (and pre-filtered here to keep the decision log
// meaningful), and ineligible beads are skipped and logged rather than fed.
func runSchedulerFeed(townRoot string, maxPerRig int, dryRun bool) (feedResult, error) {
	deferred, err := shouldDeferDispatch()
	if err != nil {
		return feedResult{}, err
	}
	if !deferred {
		return feedResult{Deferred: false}, nil
	}

	capacitySnapshot, err := polecatCapacitySnapshotForTown(townRoot)
	if err != nil {
		return feedResult{}, fmt.Errorf("loading polecat capacity: %w", err)
	}
	budget := capacitySnapshot.Free
	if budget <= 0 {
		return feedResult{Deferred: true, Reason: "no free capacity, nothing fed"}, nil
	}

	rigsConfigPath := constants.MayorRigsPath(townRoot)
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		rigsConfig = &config.RigsConfig{Rigs: make(map[string]config.RigEntry)}
	}
	g := git.NewGit(townRoot)
	rigs, err := rig.NewManager(townRoot, rigsConfig, g).DiscoverRigs()
	if err != nil {
		return feedResult{}, fmt.Errorf("discovering rigs: %w", err)
	}

	result := feedResult{Deferred: true}
	for _, r := range rigs {
		if budget <= 0 {
			break
		}

		issues, err := beads.New(r.BeadsPath()).Ready()
		if err != nil {
			result.Decisions = append(result.Decisions, feedDecision{
				Rig: r.Name, Action: "skipped", Reason: fmt.Sprintf("ready scan failed: %v", err),
			})
			continue
		}

		// Same filter chain 'gt ready' uses: strips formula scaffolds, wisps,
		// beads that don't route back to this rig, and agent/role/rig identity
		// beads — every ID that survives is one 'gt sling <id> <rig>' can use.
		formulaNames := getFormulaNames(r.BeadsPath())
		issues = filterFormulaScaffolds(issues, formulaNames)
		wispIDs := getWispIDs(r.BeadsPath())
		issues = filterWisps(issues, wispIDs)
		issues = filterReadyIssuesByRoute(townRoot, r.Name, issues)
		issues = filterIdentityBeads(issues)

		ids := make([]string, len(issues))
		for i, iss := range issues {
			ids[i] = iss.ID
		}
		scheduled := areScheduled(ids)

		fedThisRig := 0
		for _, issue := range issues {
			if budget <= 0 || fedThisRig >= maxPerRig {
				break
			}
			if scheduled[issue.ID] {
				result.Decisions = append(result.Decisions, feedDecision{
					ID: issue.ID, Rig: r.Name, Action: "skipped", Reason: "already scheduled",
				})
				continue
			}
			if reason, skip := feedSkipReason(issue); skip {
				result.Decisions = append(result.Decisions, feedDecision{
					ID: issue.ID, Rig: r.Name, Action: "skipped", Reason: reason,
				})
				continue
			}

			if !dryRun {
				if err := scheduleBead(issue.ID, r.Name, ScheduleOptions{}); err != nil {
					result.Decisions = append(result.Decisions, feedDecision{
						ID: issue.ID, Rig: r.Name, Action: "skipped", Reason: fmt.Sprintf("schedule failed: %v", err),
					})
					continue
				}
			}
			result.Decisions = append(result.Decisions, feedDecision{
				ID: issue.ID, Rig: r.Name, Action: "fed", Reason: "ready",
			})
			result.Fed++
			fedThisRig++
			budget--
		}
	}

	return result, nil
}

// feedSkipReason reports whether a ready issue should be skipped by the
// feeder, and why. Feeding is opt-out: a bead is fed unless it matches one
// of these conservative exclusions.
func feedSkipReason(issue *beads.Issue) (string, bool) {
	if isDeferredBead(&beadInfo{
		Status:      issue.Status,
		Description: issue.Description,
	}) {
		return "deferred", true
	}
	if isEpicOrConvoyIssue(issue) {
		return "epic/convoy container, not dispatchable work", true
	}
	if requiresHumanDecision(issue) {
		return "requires human decision", true
	}
	if reason, incompatible := platformIncompatible(issue); incompatible {
		return reason, true
	}
	return "", false
}

// isEpicOrConvoyIssue mirrors detectSchedulerIDType's epic/convoy check
// (sling_schedule.go) but reads the already-fetched Issue directly instead
// of re-fetching bead info.
func isEpicOrConvoyIssue(issue *beads.Issue) bool {
	switch issue.Type {
	case "epic", "convoy":
		return true
	}
	for _, label := range issue.Labels {
		switch label {
		case "gt:epic", "gt:convoy":
			return true
		}
	}
	return false
}

// requiresHumanDecision reports whether a ready bead explicitly calls for a
// human to resolve it rather than a polecat implementing it (e.g. beads
// titled "Human decision for..."). No structured label convention exists
// for this yet, so this is a conservative text heuristic mirroring
// isDeferredBead: false negatives are safe (a human still ends up looking at
// it via normal review), false positives just leave a bead unfed for a
// human to route explicitly.
func requiresHumanDecision(issue *beads.Issue) bool {
	if strings.HasPrefix(strings.ToLower(issue.Title), "human decision") {
		return true
	}
	desc := strings.ToLower(issue.Description)
	for _, phrase := range []string{
		"requires human decision",
		"requires human approval",
		"needs human approval",
		"human decision for",
	} {
		if strings.Contains(desc, phrase) {
			return true
		}
	}
	for _, label := range issue.Labels {
		if label == "gt:needs-human" {
			return true
		}
	}
	return false
}

// platformIncompatible reports whether a ready bead requires a host platform
// this machine doesn't have (e.g. macOS-only gate capture, which can never
// run on a Linux host) via a "gt:platform:<goos>" label. No beads use this
// label convention yet, so today this is a no-op safety net rather than an
// active filter; it exists so beads can opt in without any feeder change.
func platformIncompatible(issue *beads.Issue) (string, bool) {
	for _, label := range issue.Labels {
		platform, ok := strings.CutPrefix(label, "gt:platform:")
		if ok && platform != runtime.GOOS {
			return fmt.Sprintf("requires platform %q, host is %q", platform, runtime.GOOS), true
		}
	}
	return "", false
}
