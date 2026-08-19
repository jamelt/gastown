package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/lock"
	"github.com/steveyegge/gastown/internal/scheduler/capacity"
	"github.com/steveyegge/gastown/internal/session"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/tmux"
	"github.com/steveyegge/gastown/internal/workspace"
)

var (
	schedulerStatusJSON bool
	schedulerListJSON   bool
	schedulerClearBead  string
	schedulerRunBatch   int
	schedulerRunDryRun  bool
)

var schedulerCmd = &cobra.Command{
	Use:     "scheduler",
	GroupID: GroupWork,
	Short:   "Manage dispatch scheduler",
	Long: `Manage the capacity-controlled dispatch scheduler.

Subcommands:
  gt scheduler status    # Show scheduler state
  gt scheduler list      # List all scheduled beads
  gt scheduler feed      # Survey ready beads and schedule eligible work
  gt scheduler run       # Manual dispatch trigger
  gt scheduler pause     # Pause dispatch
  gt scheduler resume    # Resume dispatch
  gt scheduler clear     # Remove beads from scheduler

Config:
  gt config set scheduler.max_polecats 5    # Enable deferred dispatch
  gt config set scheduler.max_polecats -1   # Direct dispatch (default)`,
	RunE: requireSubcommand,
}

var schedulerStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show scheduler state: pending, capacity, active polecats",
	RunE:  runSchedulerStatus,
}

var schedulerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all scheduled beads with titles, rig, blocked status",
	RunE:  runSchedulerList,
}

var schedulerPauseCmd = &cobra.Command{
	Use:   "pause",
	Short: "Pause all scheduler dispatch (town-wide)",
	RunE:  runSchedulerPause,
}

var schedulerResumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Resume scheduler dispatch",
	RunE:  runSchedulerResume,
}

var schedulerClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Remove beads from the scheduler",
	Long: `Remove beads from the scheduler by closing sling context beads.

Without --bead, removes ALL beads from the scheduler.
With --bead, removes only the specified bead.`,
	RunE: runSchedulerClear,
}

var schedulerRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Manually trigger scheduler dispatch",
	Long: `Manually trigger dispatch of scheduled work.

This dispatches scheduled beads using the same logic as the daemon heartbeat,
but can be run ad-hoc. Useful for testing or when the daemon is not running.

  gt scheduler run                  # Dispatch using config defaults
  gt scheduler run --batch 5        # Dispatch up to 5
  gt scheduler run --dry-run        # Preview what would dispatch`,
	RunE: runSchedulerRun,
}

func init() {
	// Status flags
	schedulerStatusCmd.Flags().BoolVar(&schedulerStatusJSON, "json", false, "Output as JSON")

	// List flags
	schedulerListCmd.Flags().BoolVar(&schedulerListJSON, "json", false, "Output as JSON")

	// Clear flags
	schedulerClearCmd.Flags().StringVar(&schedulerClearBead, "bead", "", "Remove specific bead from scheduler")

	// Run flags
	schedulerRunCmd.Flags().IntVar(&schedulerRunBatch, "batch", 0, "Override batch size (0 = use config)")
	schedulerRunCmd.Flags().BoolVar(&schedulerRunDryRun, "dry-run", false, "Preview what would dispatch")

	// Build command tree (flat — no intermediary "capacity" level)
	schedulerCmd.AddCommand(schedulerStatusCmd)
	schedulerCmd.AddCommand(schedulerListCmd)
	schedulerCmd.AddCommand(schedulerPauseCmd)
	schedulerCmd.AddCommand(schedulerResumeCmd)
	schedulerCmd.AddCommand(schedulerClearCmd)
	schedulerCmd.AddCommand(schedulerRunCmd)

	rootCmd.AddCommand(schedulerCmd)
}

// scheduledBeadInfo holds info about a scheduled bead for display.
type scheduledBeadInfo struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	TargetRig string `json:"target_rig"`
	Blocked   bool   `json:"blocked,omitempty"`
}

// dispatchLockStatus reports the current scheduler-dispatch.lock state for
// `gt scheduler status`, so a wedged or stale dispatch lock (gt-jpib) is
// visible without reading .runtime/ by hand.
type dispatchLockStatus struct {
	PID        int    `json:"pid,omitempty"`
	Hostname   string `json:"hostname,omitempty"`
	AcquiredAt string `json:"acquired_at,omitempty"`
	AgeSeconds int64  `json:"age_seconds,omitempty"`
	Stale      bool   `json:"stale,omitempty"`
	Error      string `json:"error,omitempty"`
}

// loadSchedulerDispatchLockStatus reads the dispatch lock for display.
// Returns nil when unlocked (the common case) so callers can omit it from
// output entirely. A corrupt lock file is reported via Error rather than
// failing the whole status command — status must stay usable precisely when
// an operator is diagnosing a wedged or corrupted lock.
func loadSchedulerDispatchLockStatus(townRoot string) *dispatchLockStatus {
	info, err := lock.New(schedulerDispatchLockPath(townRoot)).Read()
	if err != nil {
		if errors.Is(err, lock.ErrNotLocked) {
			return nil
		}
		return &dispatchLockStatus{Error: err.Error()}
	}
	return &dispatchLockStatus{
		PID:        info.PID,
		Hostname:   info.Hostname,
		AcquiredAt: info.AcquiredAt.Format(time.RFC3339),
		AgeSeconds: int64(time.Since(info.AcquiredAt).Round(time.Second).Seconds()),
		Stale:      info.IsStaleAfter(schedulerDispatchLockTTL),
	}
}

func runSchedulerStatus(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return err
	}

	state, err := capacity.LoadState(townRoot)
	if err != nil {
		return fmt.Errorf("loading scheduler state: %w", err)
	}

	scheduled, err := listScheduledBeads(townRoot)
	if err != nil {
		return fmt.Errorf("listing scheduled beads: %w", err)
	}

	capacitySnapshot, err := polecatCapacitySnapshotForTown(townRoot)
	if err != nil {
		return fmt.Errorf("loading polecat capacity: %w", err)
	}

	if schedulerStatusJSON {
		out := struct {
			Paused         bool                    `json:"paused"`
			PausedBy       string                  `json:"paused_by,omitempty"`
			ScheduledTotal int                     `json:"queued_total"`
			ScheduledReady int                     `json:"queued_ready"`
			ActivePolecats int                     `json:"active_polecats"`
			Capacity       polecatCapacitySnapshot `json:"capacity"`
			LastDispatchAt string                  `json:"last_dispatch_at,omitempty"`
			DispatchLock   *dispatchLockStatus     `json:"dispatch_lock,omitempty"`
			Beads          []scheduledBeadInfo     `json:"beads"`
		}{
			Paused:         state.Paused,
			PausedBy:       state.PausedBy,
			ScheduledTotal: len(scheduled),
			ActivePolecats: capacitySnapshot.ActiveSessions,
			Capacity:       capacitySnapshot,
			LastDispatchAt: state.LastDispatchAt,
			DispatchLock:   loadSchedulerDispatchLockStatus(townRoot),
			Beads:          scheduled,
		}
		for _, b := range scheduled {
			if !b.Blocked {
				out.ScheduledReady++
			}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	readyCount := 0
	for _, b := range scheduled {
		if !b.Blocked {
			readyCount++
		}
	}

	fmt.Printf("%s\n\n", style.Bold.Render("Scheduler Status"))
	if state.Paused {
		fmt.Printf("  State:    %s (by %s)\n", style.Warning.Render("PAUSED"), state.PausedBy)
	} else {
		fmt.Printf("  State:    active\n")
	}
	fmt.Printf("  Scheduled: %d total, %d ready\n", len(scheduled), readyCount)
	fmt.Printf("  Active:    %d polecats\n", capacitySnapshot.ActiveSessions)
	if capacitySnapshot.Max > 0 {
		fmt.Printf("  Capacity:  %d free of %d (working: %d, recovery: %d, reservations: %d, reusable idle: %d, pending MR: %d)\n",
			capacitySnapshot.Free,
			capacitySnapshot.Max,
			capacitySnapshot.Working,
			capacitySnapshot.RecoveryBlocked,
			capacitySnapshot.Reservations,
			capacitySnapshot.ReusableIdle,
			capacitySnapshot.PendingMR,
		)
	} else {
		fmt.Printf("  Capacity:  direct dispatch (scheduler.max_polecats=%d)\n", capacitySnapshot.Max)
	}
	if dl := loadSchedulerDispatchLockStatus(townRoot); dl != nil {
		host := ""
		if dl.Hostname != "" {
			host = " on " + dl.Hostname
		}
		switch {
		case dl.Error != "":
			fmt.Printf("  Dispatch:  %s lock file unreadable (%s): %s\n",
				style.Warning.Render("⚠"), schedulerDispatchLockPath(townRoot), dl.Error)
		case dl.Stale:
			fmt.Printf("  Dispatch:  %s stale lock (PID %d%s, held %s — previous dispatch likely crashed; clears on next run)\n",
				style.Warning.Render("⚠"), dl.PID, host, time.Duration(dl.AgeSeconds)*time.Second)
		default:
			fmt.Printf("  Dispatch:  running (PID %d%s, held %s)\n",
				dl.PID, host, time.Duration(dl.AgeSeconds)*time.Second)
		}
	}
	if state.LastDispatchAt != "" {
		fmt.Printf("  Last dispatch: %s (%d beads)\n", state.LastDispatchAt, state.LastDispatchCount)
	}

	return nil
}

func runSchedulerList(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return err
	}

	scheduled, err := listScheduledBeads(townRoot)
	if err != nil {
		return fmt.Errorf("listing scheduled beads: %w", err)
	}

	if schedulerListJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(scheduled)
	}

	if len(scheduled) == 0 {
		fmt.Println("No beads scheduled.")
		fmt.Println("Enable deferred dispatch with: gt config set scheduler.max_polecats <N>")
		return nil
	}

	byRig := make(map[string][]scheduledBeadInfo)
	for _, b := range scheduled {
		byRig[b.TargetRig] = append(byRig[b.TargetRig], b)
	}

	fmt.Printf("%s (%d beads)\n\n", style.Bold.Render("Scheduled Work"), len(scheduled))
	for rig, beads := range byRig {
		fmt.Printf("  %s (%d):\n", style.Bold.Render(rig), len(beads))
		for _, b := range beads {
			indicator := "○"
			if b.Blocked {
				indicator = "⏸"
			}
			fmt.Printf("    %s %s: %s\n", indicator, b.ID, b.Title)
		}
		fmt.Println()
	}

	return nil
}

func runSchedulerPause(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return err
	}

	state, err := capacity.LoadState(townRoot)
	if err != nil {
		return fmt.Errorf("loading scheduler state: %w", err)
	}

	if state.Paused {
		fmt.Printf("%s Scheduler is already paused (by %s)\n", style.Dim.Render("○"), state.PausedBy)
		return nil
	}

	actor := detectActor()
	state.SetPaused(actor)
	if err := capacity.SaveState(townRoot, state); err != nil {
		return fmt.Errorf("saving scheduler state: %w", err)
	}

	fmt.Printf("%s Scheduler paused\n", style.Bold.Render("⏸"))
	return nil
}

func runSchedulerResume(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return err
	}

	state, err := capacity.LoadState(townRoot)
	if err != nil {
		return fmt.Errorf("loading scheduler state: %w", err)
	}

	if !state.Paused {
		fmt.Printf("%s Scheduler is not paused\n", style.Dim.Render("○"))
		return nil
	}

	state.SetResumed()
	if err := capacity.SaveState(townRoot, state); err != nil {
		return fmt.Errorf("saving scheduler state: %w", err)
	}

	fmt.Printf("%s Scheduler resumed\n", style.Bold.Render("▶"))
	return nil
}

func runSchedulerClear(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return err
	}

	if schedulerClearBead != "" {
		// Close ALL sling contexts for this specific work bead (there may be
		// duplicates if concurrent scheduleBead calls raced past idempotency).
		// Scan all rig dirs since contexts live in target rig beads. (GH#3468)
		contexts, err := listAllSlingContextRecords(townRoot)
		if err != nil {
			return fmt.Errorf("listing sling contexts: %w", err)
		}

		closed := 0
		for _, ctx := range contexts {
			fields := beads.ParseSlingContextFields(ctx.issue.Description)
			if fields != nil && fields.WorkBeadID == schedulerClearBead {
				if err := beadsForContextRecord(ctx).CloseSlingContext(ctx.issue.ID, "cleared"); err != nil {
					fmt.Printf("  %s Could not close context %s: %v\n", style.Dim.Render("Warning:"), ctx.issue.ID, err)
					continue
				}
				closed++
			}
		}

		// Mark the work bead itself, not just the context: an explicit clear
		// must survive the next feed cycle even when there is nothing left to
		// close (e.g. re-running clear, or a context that closed for another
		// reason moments earlier). Only a fresh `gt sling` lifts this marker.
		markSchedulerCleared(townRoot, schedulerClearBead)

		if closed == 0 {
			fmt.Printf("%s No sling context found for %s\n", style.Dim.Render("○"), schedulerClearBead)
		} else {
			fmt.Printf("%s Removed %s from scheduler (closed %d context(s))\n",
				style.Bold.Render("✓"), schedulerClearBead, closed)
		}
		return nil
	}

	// Close all open sling contexts across all dirs
	allContexts, err := listAllSlingContextRecords(townRoot)
	if err != nil {
		return fmt.Errorf("listing sling contexts: %w", err)
	}

	if len(allContexts) == 0 {
		fmt.Println("Scheduler is already empty.")
		return nil
	}

	cleared := 0
	for _, ctx := range allContexts {
		if err := beadsForContextRecord(ctx).CloseSlingContext(ctx.issue.ID, "cleared"); err != nil {
			fmt.Printf("  %s Could not close context %s: %v\n", style.Dim.Render("Warning:"), ctx.issue.ID, err)
			continue
		}
		cleared++
		if fields := beads.ParseSlingContextFields(ctx.issue.Description); fields != nil && fields.WorkBeadID != "" {
			markSchedulerCleared(townRoot, fields.WorkBeadID)
		}
	}

	fmt.Printf("%s Cleared %d context bead(s) from scheduler\n", style.Bold.Render("✓"), cleared)
	return nil
}

// markSchedulerCleared records a durable marker on the work bead itself
// (not the ephemeral context bead) saying its sling context was explicitly
// removed via `gt scheduler clear`. Closing the context alone is not
// durable: the next `gt scheduler feed` pass sees a ready, unscheduled bead
// and — with no memory that a human just cleared it — recreates the exact
// context it was cleared from (gt-5ti). The feeder checks this label and
// skips marked beads; only `gt sling` (which bypasses the feeder) removes it.
// Best-effort: the context close is the primary action and already
// succeeded or failed on its own by the time this runs.
func markSchedulerCleared(townRoot, workBeadID string) {
	rigName := resolveRigForBead(townRoot, workBeadID)
	if rigName == "" {
		fmt.Printf("  %s Could not resolve rig for %s; scheduler-cleared marker not set (a later feed pass may re-enqueue it)\n",
			style.Dim.Render("Warning:"), workBeadID)
		return
	}
	rigBeadsDir, ok := beads.ResolveRepoAliasBeadsDir(townRoot, rigName)
	if !ok {
		fmt.Printf("  %s Could not resolve beads dir for rig %s; scheduler-cleared marker not set on %s (a later feed pass may re-enqueue it)\n",
			style.Dim.Render("Warning:"), rigName, workBeadID)
		return
	}
	b := beads.NewWithBeadsDir(filepath.Dir(rigBeadsDir), rigBeadsDir).ForLocalBeads()
	if err := b.Update(workBeadID, beads.UpdateOptions{AddLabels: []string{capacity.LabelSchedulerCleared}}); err != nil {
		fmt.Printf("  %s Could not set scheduler-cleared marker on %s: %v (a later feed pass may re-enqueue it)\n",
			style.Dim.Render("Warning:"), workBeadID, err)
	}
}

func runSchedulerRun(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return err
	}

	_, err = dispatchScheduledWork(townRoot, detectActor(), schedulerRunBatch, schedulerRunDryRun)
	return err
}

// listScheduledBeads returns info about all scheduled beads for display.
// Reconciles sling context beads with work bead readiness to mark blocked status.
// Uses batch fetch for work bead info to avoid N+1 subprocess spawns.
func listScheduledBeads(townRoot string) ([]scheduledBeadInfo, error) {
	assessments, err := assessScheduledContexts(townRoot)
	if err != nil {
		return nil, err
	}
	return scheduledBeadInfosFromAssessments(assessments), nil
}

func scheduledBeadInfosFromAssessments(assessments []scheduledContextAssessment) []scheduledBeadInfo {
	var result []scheduledBeadInfo
	for _, assessment := range assessments {
		bead, ok := scheduledBeadInfoFromWork(assessment.context.issue.Title, assessment.fields, assessment.info, assessment.found, assessment.ready)
		if !ok {
			continue
		}
		result = append(result, bead)
	}

	return result
}

func scheduledBeadInfoFromWork(ctxTitle string, fields *capacity.SlingContextFields, info beadStatusInfo, found, ready bool) (scheduledBeadInfo, bool) {
	if fields == nil {
		return scheduledBeadInfo{}, false
	}
	title := ctxTitle
	status := "open"
	if found {
		title = info.Title
		status = info.Status
		if status == string(beads.IssueStatusHooked) || status == "closed" || status == "tombstone" {
			return scheduledBeadInfo{}, false
		}
	}
	return scheduledBeadInfo{
		ID:        fields.WorkBeadID,
		Title:     title,
		Status:    status,
		TargetRig: fields.TargetRig,
		Blocked:   !ready,
	}, true
}

// beadsSearchDirs returns directories to scan for scheduled beads:
// the town root plus any rig directories that have a .beads/ subdirectory.
func beadsSearchDirs(townRoot string) ([]string, error) {
	dirs := []string{townRoot}
	seen := map[string]bool{townRoot: true}
	entries, err := os.ReadDir(townRoot)
	if err != nil {
		return nil, fmt.Errorf("discovering scheduler beads search dirs: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || e.Name() == "mayor" || e.Name() == "settings" {
			continue
		}
		rigDir := filepath.Join(townRoot, e.Name())
		beadsDir := filepath.Join(rigDir, ".beads")
		if _, err := os.Stat(beadsDir); err == nil && !seen[rigDir] {
			dirs = append(dirs, rigDir)
			seen[rigDir] = true
		}
		mayorRigDir := filepath.Join(rigDir, "mayor", "rig")
		mayorBeadsDir := filepath.Join(mayorRigDir, ".beads")
		if _, err := os.Stat(mayorBeadsDir); err == nil && !seen[mayorRigDir] {
			dirs = append(dirs, mayorRigDir)
			seen[mayorRigDir] = true
		}
	}
	return dirs, nil
}

// countActivePolecats counts running, inventory-backed polecat tmux sessions.
// A parseable name alone is not sufficient evidence: malformed and test
// sessions such as gt-test-nudge-1 otherwise look like polecats because a
// polecat name may contain dashes.
// Capacity admission uses polecatCapacitySnapshotForTown instead; active sessions
// are shown for operator context only.
func countActivePolecats(townRoot string) int {
	listCmd := tmux.BuildCommand("list-sessions", "-F", "#{session_name}")
	out, err := listCmd.Output()
	if err != nil {
		return 0
	}

	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		identity, err := session.ParseSessionName(line)
		if err != nil {
			continue
		}
		if identity.Role == session.RolePolecat {
			if identity.Name == "." || identity.Name == ".." || filepath.Base(identity.Name) != identity.Name {
				continue
			}
			polecatDir := filepath.Join(townRoot, identity.Rig, "polecats", identity.Name)
			if info, statErr := os.Stat(polecatDir); statErr == nil && info.IsDir() {
				count++
			}
		}
	}
	return count
}
