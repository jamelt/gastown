package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gofrs/flock"
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
scheduled, blocked, deferred, epics/convoys, hq-1s4w hard-prohibition
categories — production, trading-money-policy, credentials, destructive
migrations, human-decision — signaled via labels like "human", "risk:money",
"area:security", "gt:needs-human", beads requiring a platform this host
doesn't have), and schedules up to --max-per-rig eligible beads per rig,
bounded by town-wide free polecat capacity. Every decision — fed or
skipped, and why — is logged.

Feed only acts when the scheduler is in deferred-dispatch mode
(scheduler.max_polecats > 0); in direct-dispatch mode there is no queue to
feed. Feed does not survey town-level (hq-*) beads: scheduleBead only
targets rigs, so town-level ready work is out of scope for now.

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
	Reason    string         `json:"reason,omitempty"` // always set on a town-wide no-op: not deferred, lock held, or no capacity
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
//
// A non-blocking file lock serializes feed cycles the same way
// dispatchScheduledWork serializes dispatch cycles: without it, an
// overlapping daemon tick and manual `gt scheduler feed` could each read the
// same free-capacity snapshot and jointly over-schedule.
func runSchedulerFeed(townRoot string, maxPerRig int, dryRun bool) (feedResult, error) {
	deferred, err := shouldDeferDispatch()
	if err != nil {
		return feedResult{}, err
	}
	if !deferred {
		return feedResult{Deferred: false, Reason: "scheduler is in direct-dispatch mode, nothing to feed"}, nil
	}

	if !dryRun {
		runtimeDir := filepath.Join(townRoot, ".runtime")
		_ = os.MkdirAll(runtimeDir, 0755)
		lockFile := filepath.Join(runtimeDir, "scheduler-feed.lock")
		fileLock := flock.New(lockFile)
		locked, err := fileLock.TryLock()
		if err != nil {
			return feedResult{}, fmt.Errorf("acquiring feed lock: %w", err)
		}
		if !locked {
			if isDaemonDispatch() {
				return feedResult{Deferred: true, Reason: "feed already in progress, skipping this tick"}, nil
			}
			return feedResult{}, fmt.Errorf("scheduler feed already in progress (lock held: %s)", lockFile)
		}
		defer func() { _ = fileLock.Unlock() }()
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

		issues, err := readyIssuesForSource(townRoot, r.Name, r.BeadsPath())
		if err != nil {
			result.Decisions = append(result.Decisions, feedDecision{
				Rig: r.Name, Action: "skipped", Reason: fmt.Sprintf("ready scan failed: %v", err),
			})
			continue
		}

		ids := make([]string, len(issues))
		for i, iss := range issues {
			ids[i] = iss.ID
		}
		scheduled := areScheduled(ids)

		// bd ready --json does not include labels (see readyIssuesForSource /
		// filterIdentityBeads); a hard-prohibition label like "human" or
		// "area:security" would silently never be seen without this batch
		// `bd show` fetch. Batched per rig to avoid an N+1 subprocess per bead.
		labelsByID := batchFetchBeadInfoByIDs(townRoot, ids)

		formula := resolveFormula("", false, townRoot, r.Name)

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

			effective := effectiveIssueForFeed(issue, labelsByID)
			if reason, skip := feedSkipReason(effective); skip {
				result.Decisions = append(result.Decisions, feedDecision{
					ID: issue.ID, Rig: r.Name, Action: "skipped", Reason: reason,
				})
				continue
			}

			if !dryRun {
				// scheduleBead no-ops (returns nil) if another process scheduled
				// this bead between the areScheduled() snapshot above and here;
				// that race is narrow (single lock held for this whole cycle,
				// see above) and self-correcting — actual dispatch re-checks
				// capacity independently, so a stale "fed" log entry here costs
				// nothing beyond one inaccurate decision line.
				if err := scheduleBead(issue.ID, r.Name, ScheduleOptions{Formula: formula}); err != nil {
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

// effectiveIssueForFeed overlays a batch bd-show label fetch onto a bd-ready
// issue. bd ready --json never populates Labels (see readyIssuesForSource),
// so every eligibility decision that reads issue.Labels — hard-prohibition
// labels above all — is a silent no-op unless this overlay runs first. This
// is deliberately its own function so that fact is directly unit-testable
// rather than only implied by feedSkipReason's label checks.
func effectiveIssueForFeed(issue *beads.Issue, labelsByID map[string]beadStatusInfo) *beads.Issue {
	effective := *issue
	if info, ok := labelsByID[issue.ID]; ok {
		effective.Labels = info.Labels
	}
	return &effective
}

// feedSkipReason reports whether a ready issue should be skipped by the
// feeder, and why. Feeding is opt-out: a bead is fed unless it matches one
// of these conservative exclusions. issue.Labels must already reflect a
// `bd show`-sourced batch fetch (see runSchedulerFeed) — bd ready --json
// does not populate labels.
func feedSkipReason(issue *beads.Issue) (string, bool) {
	// bd ready computes readiness from the dependency graph; status is a
	// separate, manually-set field that ready does not gate on. Real queue
	// contents have included status=blocked/deferred beads with no unmet
	// dependency edges, so both must be checked explicitly here.
	if issue.Status == "blocked" {
		return "status: blocked", true
	}
	if isDeferredBead(&beadInfo{
		Status:      issue.Status,
		Description: issue.Description,
	}) {
		return "deferred", true
	}
	if isEpicOrConvoyIssue(issue) {
		return "epic/convoy container, not dispatchable work", true
	}
	if reason, blocked := requiresHumanDecision(issue); blocked {
		return reason, true
	}
	if reason, incompatible := platformIncompatible(issue); incompatible {
		return reason, true
	}
	return "", false
}

// isEpicOrConvoyIssue mirrors detectSchedulerIDType's epic/convoy check
// (sling_schedule.go) but reads the already-fetched Issue directly instead
// of re-fetching bead info via getBeadInfo — the feeder already batch-fetches
// everything it needs per rig and an extra per-bead bd show would reintroduce
// the N+1 subprocess cost that batching was added to avoid. The hq-cv- ID
// fast path is duplicated here for the same reason.
func isEpicOrConvoyIssue(issue *beads.Issue) bool {
	if strings.HasPrefix(issue.ID, "hq-cv-") {
		return true
	}
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

// hardProhibitionLabels are structured signals for the hq-1s4w standing
// hard-prohibition categories (production, trading-money-policy, credentials,
// destructive migrations, human-decision work) that require fresh human
// approval before every dispatch — not just review after the fact. Verified
// against live queue contents 2026-08-19: trader-obs-d9 (a credentials bead
// custodying an ops webhook secret, titled to read like ordinary
// observability work) carries "human" and "area:security"; trader-obs-a20
// and trader-atm-r26 carry "risk:money". These are in active use, not a
// speculative convention.
var hardProhibitionLabels = map[string]string{
	"human":          "human label",
	"gt:needs-human": "gt:needs-human label",
	"risk:money":     "risk:money label",
	"area:security":  "area:security label",
}

// requiresHumanDecision reports whether a ready bead falls into an hq-1s4w
// hard-prohibition category. Label checks come first and are load-bearing:
// title/description text is unreliable in both directions on real data —
// trader-obs-d9 (credentials) has an innocuous title and none of the
// trigger phrases, while trader-lt8e ("Repair legacy production
// acknowledgement migration cleanup") reads as alarming but is a safe
// client-side localStorage key migration. A false negative here is not
// recoverable by later review: review happens at merge, the damage (a
// polecat starting credentials/production/money-policy work unsupervised)
// happens at dispatch. A match here means the feeder never auto-schedules
// the bead; a human can still schedule it manually via gt sling, which is
// where the required fresh, per-dispatch approval happens. Note this gate
// exists only in the feeder — gt sling itself does not consult it, so it
// only ever narrows automatic feeding, never manual dispatch.
func requiresHumanDecision(issue *beads.Issue) (string, bool) {
	for _, label := range issue.Labels {
		if reason, blocked := hardProhibitionLabels[label]; blocked {
			return reason, true
		}
	}
	if strings.HasPrefix(strings.ToLower(issue.Title), "human decision") {
		return "title indicates human decision required", true
	}
	desc := strings.ToLower(issue.Description)
	for _, phrase := range []string{
		"requires human decision",
		"requires human approval",
		"needs human approval",
		"human decision for",
	} {
		if strings.Contains(desc, phrase) {
			return "description indicates human decision required", true
		}
	}
	return "", false
}

// platformIncompatible reports whether a ready bead requires a host platform
// this machine doesn't have (e.g. macOS-only gate capture, which can never
// run on a Linux host) via a "gt:platform:<goos>" label. No beads use this
// label convention yet, so today this is a no-op safety net rather than an
// active filter; it exists so beads can opt in without any feeder change.
// The real platform-impossible beads observed live (trader-q29t, trader-hkdt)
// carried no such label and were caught instead by the blocked-status check
// in feedSkipReason.
func platformIncompatible(issue *beads.Issue) (string, bool) {
	for _, label := range issue.Labels {
		platform, ok := strings.CutPrefix(label, "gt:platform:")
		if ok && platform != runtime.GOOS {
			return fmt.Sprintf("requires platform %q, host is %q", platform, runtime.GOOS), true
		}
	}
	return "", false
}
