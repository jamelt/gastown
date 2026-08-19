package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/constants"
)

// RunResult represents the outcome of a plugin execution.
type RunResult string

const (
	ResultSuccess RunResult = "success"
	ResultFailure RunResult = "failure"
	ResultSkipped RunResult = "skipped"
)

// PluginRunRecord represents data for creating a plugin run bead.
type PluginRunRecord struct {
	PluginName  string
	RigName     string
	Result      RunResult
	Title       string
	Body        string
	ExtraLabels []string
}

// PluginRunBead represents a recorded plugin run from the ledger.
type PluginRunBead struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	Labels    []string  `json:"labels"`
	Result    RunResult `json:"-"` // Parsed from labels
}

// Recorder handles plugin run recording and querying.
type Recorder struct {
	townRoot string
}

// NewRecorder creates a new plugin run recorder.
func NewRecorder(townRoot string) *Recorder {
	return &Recorder{townRoot: townRoot}
}

// RecordRun creates an ephemeral bead for a plugin run.
// This is pure data writing - the caller decides what result to record.
func (r *Recorder) RecordRun(record PluginRunRecord) (string, error) {
	title := record.Title
	if title == "" {
		title = fmt.Sprintf("Plugin run: %s", record.PluginName)
	}

	// Build labels
	labels := []string{
		"type:plugin-run",
		fmt.Sprintf("plugin:%s", record.PluginName),
		fmt.Sprintf("result:%s", record.Result),
	}
	if record.RigName != "" {
		labels = append(labels, fmt.Sprintf("rig:%s", record.RigName))
	}
	labels = append(labels, record.ExtraLabels...)

	// Build bd create command
	args := []string{
		"create",
		"--ephemeral",
		"--json",
		"-t", "chore",
		"--title=" + title,
	}
	for _, label := range labels {
		args = append(args, "-l", label)
	}
	if record.Body != "" {
		args = append(args, "--description="+record.Body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), constants.BdCommandTimeout)
	defer cancel()
	townBeads := beads.ResolveBeadsDir(r.townRoot)
	cmd := beads.CommandContext(ctx, r.townRoot, townBeads, beads.MutationPinned, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("creating plugin run bead: %s: %w", stderr.String(), err)
	}

	// Parse created bead ID from JSON output
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return "", fmt.Errorf("parsing bd create output: %w", err)
	}

	// Close the receipt immediately — it exists for audit/cooldown-gate queries
	// (which use --all to include closed beads) but should not stay open.
	closeCtx, closeCancel := context.WithTimeout(context.Background(), constants.BdCommandTimeout)
	defer closeCancel()
	closeCmd := beads.CommandContext(closeCtx, r.townRoot, townBeads, beads.MutationPinned, "close", result.ID, "--reason", "plugin run recorded")
	_ = closeCmd.Run() // Best-effort — reaper will catch it if this fails

	return result.ID, nil
}

// GetLastRun returns the most recent run for a plugin.
// Returns nil if no runs found.
func (r *Recorder) GetLastRun(pluginName string) (*PluginRunBead, error) {
	runs, err := r.queryRuns(pluginName, 1, "")
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, nil
	}
	return runs[0], nil
}

// GetRunsSince returns all runs for a plugin since the given duration.
// Duration format: "1h", "24h", "7d", etc.
func (r *Recorder) GetRunsSince(pluginName string, since string) ([]*PluginRunBead, error) {
	return r.queryRuns(pluginName, 0, since)
}

// queryRuns queries plugin run beads from the ledger.
//
// Plugin run receipts are created ephemeral (they live in the wisps table,
// not the issues table — see RecordRun's "--ephemeral" flag), so this must
// go through the canonical ephemeral-aware query path (Beads.List with
// Ephemeral: true). A plain "bd list --all" only searches the issues table
// and silently misses every closed ephemeral receipt.
func (r *Recorder) queryRuns(pluginName string, limit int, since string) ([]*PluginRunBead, error) {
	var cutoff time.Time
	if since != "" {
		// Parse as Go duration. bd's compact duration uses "m" for months,
		// but plugin gate durations use Go's time.ParseDuration where "m"
		// means minutes, so filtering is done client-side against an
		// absolute cutoff rather than passed through to bd.
		d, err := time.ParseDuration(since)
		if err != nil {
			return nil, fmt.Errorf("parsing duration %q: %w", since, err)
		}
		cutoff = time.Now().Add(-d).UTC()
	}

	issues, err := beads.New(r.townRoot).List(beads.ListOptions{
		Ephemeral: true,
		Status:    "all", // Include closed beads too
		Priority:  -1,    // No priority filter (0 is a valid priority, not "unset")
		Labels:    []string{"type:plugin-run", fmt.Sprintf("plugin:%s", pluginName)},
	})
	if err != nil {
		return nil, fmt.Errorf("querying plugin runs: %w", err)
	}

	// Convert to PluginRunBead with parsed result. Results are already
	// ordered newest-first (priority ASC, created_at DESC) by the underlying
	// query, matching callers like GetLastRun that rely on runs[0].
	runs := make([]*PluginRunBead, 0, len(issues))
	for _, issue := range issues {
		var createdAt time.Time
		if t, err := time.Parse(time.RFC3339, issue.CreatedAt); err == nil {
			createdAt = t
		}
		if since != "" && createdAt.Before(cutoff) {
			continue
		}

		run := &PluginRunBead{
			ID:        issue.ID,
			Title:     issue.Title,
			CreatedAt: createdAt,
			Labels:    issue.Labels,
		}

		// Extract result from labels
		for _, label := range issue.Labels {
			if len(label) > 7 && label[:7] == "result:" {
				run.Result = RunResult(label[7:])
				break
			}
		}

		runs = append(runs, run)
	}

	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}

	return runs, nil
}

// CountRunsSince returns the count of runs for a plugin since the given duration.
// This is useful for cooldown gate evaluation.
func (r *Recorder) CountRunsSince(pluginName string, since string) (int, error) {
	runs, err := r.GetRunsSince(pluginName, since)
	if err != nil {
		return 0, err
	}
	return len(runs), nil
}
