package cmd

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/constants"
	"github.com/steveyegge/gastown/internal/quota"
	"github.com/steveyegge/gastown/internal/session"
	"github.com/steveyegge/gastown/internal/style"
	ttmux "github.com/steveyegge/gastown/internal/tmux"
	"github.com/steveyegge/gastown/internal/workspace"
)

var failoverDryRun bool

var quotaFailoverCmd = &cobra.Command{
	Use:   "failover",
	Short: "Move blocked sessions to configured backup providers",
	Long: `Move hard quota-limited sessions to the next eligible agent provider.

This command only acts when settings/config.json enables agent_failover. It
uses role_agent_backups for normal role sessions and agent_backups for explicit
formula agents such as the Expeditor or a council convergence chair. Routes
advance strictly forward, respect provider cooldowns and max_per_session, and
never trigger from ordinary task failures or near-limit warnings.

Cross-provider continuation restores from durable Gas Town state (the hooked
bead, worktree, comments, and gt prime output); it does not attempt to resume a
provider-specific conversation transcript.

Examples:
  gt quota failover --dry-run
  gt quota failover --json`,
	RunE: runQuotaFailover,
}

// AgentFailoverResult reports one planned or executed provider transition.
type AgentFailoverResult struct {
	quota.AgentFailoverAssignment
	Changed bool   `json:"changed"`
	DryRun  bool   `json:"dry_run,omitempty"`
	Error   string `json:"error,omitempty"`
}

type agentFailoverTmux interface {
	GetEnvironment(session, key string) (string, error)
	SetEnvironment(session, key, value string) error
	GetPaneID(session string) (string, error)
	SetRemainOnExit(pane string, on bool) error
	KillPaneProcesses(pane string) error
	ClearHistory(pane string) error
	RespawnPane(pane, command string) error
	AcceptStartupDialogs(session string) error
}

func init() {
	quotaFailoverCmd.Flags().BoolVar(&failoverDryRun, "dry-run", false, "Show provider transitions without restarting sessions")
	quotaFailoverCmd.Flags().BoolVar(&quotaJSON, "json", false, "Output as JSON")
	quotaCmd.AddCommand(quotaFailoverCmd)
}

func runQuotaFailover(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return fmt.Errorf("finding town root: %w", err)
	}
	townSettings, err := config.LoadOrCreateTownSettings(config.TownSettingsPath(townRoot))
	if err != nil {
		return fmt.Errorf("loading town settings: %w", err)
	}
	if townSettings.AgentFailover == nil || !townSettings.AgentFailover.Enabled {
		return fmt.Errorf("agent failover is disabled (set agent_failover.enabled=true in settings/config.json)")
	}

	// Accounts are optional here. The scanner still detects provider limits
	// when no same-provider Claude account pool is configured.
	var acctCfg *config.AccountsConfig
	if loaded, loadErr := config.LoadAccountsConfig(constants.MayorAccountsPath(townRoot)); loadErr == nil {
		acctCfg = loaded
	}
	t := ttmux.NewTmux()
	scanner, err := quota.NewScanner(t, nil, acctCfg)
	if err != nil {
		return fmt.Errorf("creating scanner: %w", err)
	}
	results, err := scanner.ScanAll()
	if err != nil {
		return fmt.Errorf("scanning sessions: %w", err)
	}

	mgr := quota.NewManager(townRoot)
	state, err := mgr.Load()
	if err != nil {
		return fmt.Errorf("loading quota state: %w", err)
	}

	rigPathFor := func(result quota.ScanResult) string {
		identity, parseErr := session.ParseSessionName(result.Session)
		if parseErr != nil || identity.Rig == "" {
			return ""
		}
		return filepath.Join(townRoot, identity.Rig)
	}
	roleFor := func(result quota.ScanResult) string {
		if role := config.ExtractSimpleRole(result.Role); role != "" {
			return role
		}
		identity, parseErr := session.ParseSessionName(result.Session)
		if parseErr != nil {
			return ""
		}
		return config.ExtractSimpleRole(identity.GTRole())
	}

	plan, err := quota.PlanAgentFailover(
		results,
		state,
		townSettings.AgentFailover,
		time.Now(),
		func(result quota.ScanResult) ([]string, error) {
			return config.ResolveAgentRoute(roleFor(result), result.Agent, townRoot, rigPathFor(result))
		},
		func(agent string, result quota.ScanResult) (string, error) {
			return config.ResolveAgentQuotaProvider(townRoot, rigPathFor(result), agent)
		},
	)
	if err != nil {
		return fmt.Errorf("planning agent failover: %w", err)
	}

	sessions := quota.SortedFailoverSessions(plan)
	if len(sessions) == 0 {
		if quotaJSON {
			return json.NewEncoder(os.Stdout).Encode([]AgentFailoverResult{})
		}
		if len(plan.LimitedSessions) == 0 {
			fmt.Printf(" %s No hard quota-limited sessions detected\n", style.SuccessPrefix)
		} else {
			fmt.Printf(" %s No eligible provider failovers\n", style.WarningPrefix)
			for _, sessionName := range slices.Sorted(maps.Keys(plan.Skipped)) {
				fmt.Printf("   %s: %s\n", sessionName, plan.Skipped[sessionName])
			}
		}
		return nil
	}

	if !quotaJSON {
		fmt.Println(style.Bold.Render("Provider Failover Plan"))
		fmt.Println()
		for _, sessionName := range sessions {
			a := plan.Assignments[sessionName]
			fmt.Printf(" %s %-25s %s (%s) → %s (%s)\n", style.ArrowPrefix, sessionName,
				a.CurrentAgent, a.CurrentProvider, a.NextAgent, a.NextProvider)
		}
	}

	var failoverResults []AgentFailoverResult
	if failoverDryRun {
		for _, sessionName := range sessions {
			failoverResults = append(failoverResults, AgentFailoverResult{
				AgentFailoverAssignment: plan.Assignments[sessionName],
				DryRun:                  true,
			})
		}
	} else {
		cooldown, _ := time.ParseDuration(plan.ProviderCooldown)
		for _, sessionName := range sessions {
			assignment := plan.Assignments[sessionName]
			result := executeAgentFailover(t, mgr, assignment, cooldown, func(sessionName, nextAgent string) (string, error) {
				return buildRestartCommandWithOpts(sessionName, buildRestartCommandOpts{
					AgentOverride: nextAgent,
					StartupPrompt: "Provider failover: the previous runtime reached its configured quota. Run `gt prime --hook`, inspect the same hooked bead and worktree, and continue from durable state. Do not assume transcript context.",
				})
			})
			failoverResults = append(failoverResults, result)
			if !quotaJSON {
				if result.Changed {
					fmt.Printf(" %s %s → %s\n", style.SuccessPrefix, result.Session, result.NextAgent)
				} else {
					fmt.Printf(" %s %s: %s\n", style.ErrorPrefix, result.Session, result.Error)
				}
			}
		}
	}

	if quotaJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(failoverResults)
	}
	if failoverDryRun {
		fmt.Println()
		fmt.Println(style.Dim.Render(" (dry run — no changes made)"))
	}
	return nil
}

func executeAgentFailover(
	t agentFailoverTmux,
	mgr *quota.Manager,
	assignment quota.AgentFailoverAssignment,
	cooldown time.Duration,
	restartBuilder func(session, nextAgent string) (string, error),
) AgentFailoverResult {
	result := AgentFailoverResult{AgentFailoverAssignment: assignment}
	restartCmd, err := restartBuilder(assignment.Session, assignment.NextAgent)
	if err != nil {
		result.Error = "building restart command: " + err.Error()
		return result
	}
	pane, err := t.GetPaneID(assignment.Session)
	if err != nil {
		result.Error = "getting pane: " + err.Error()
		return result
	}

	oldAgent, _ := t.GetEnvironment(assignment.Session, "GT_AGENT")
	oldProcessNames, _ := t.GetEnvironment(assignment.Session, "GT_PROCESS_NAMES")
	newProcessNames := strings.Join(config.ResolveProcessNames(assignment.NextAgent, ""), ",")
	if err := t.SetEnvironment(assignment.Session, "GT_AGENT", assignment.NextAgent); err != nil {
		result.Error = "setting GT_AGENT: " + err.Error()
		return result
	}
	if err := t.SetEnvironment(assignment.Session, "GT_PROCESS_NAMES", newProcessNames); err != nil {
		_ = t.SetEnvironment(assignment.Session, "GT_AGENT", oldAgent)
		result.Error = "setting GT_PROCESS_NAMES: " + err.Error()
		return result
	}
	// Account swaps are Claude-specific and must not leak across providers.
	_ = t.SetEnvironment(assignment.Session, "GT_QUOTA_ACCOUNT", "")

	if err := t.SetRemainOnExit(pane, true); err != nil {
		style.PrintWarning("could not set remain-on-exit for %s: %v", assignment.Session, err)
	}
	if err := t.KillPaneProcesses(pane); err != nil {
		style.PrintWarning("could not kill pane processes for %s: %v", assignment.Session, err)
	}
	if err := t.ClearHistory(pane); err != nil {
		style.PrintWarning("could not clear history for %s: %v", assignment.Session, err)
	}
	if err := t.RespawnPane(pane, restartCmd); err != nil {
		_ = t.SetEnvironment(assignment.Session, "GT_AGENT", oldAgent)
		_ = t.SetEnvironment(assignment.Session, "GT_PROCESS_NAMES", oldProcessNames)
		result.Error = "respawning pane: " + err.Error()
		return result
	}
	if err := t.AcceptStartupDialogs(assignment.Session); err != nil {
		style.PrintWarning("could not accept startup dialogs for %s: %v", assignment.Session, err)
	}

	now := time.Now()
	if err := mgr.WithLock(func() error {
		state, loadErr := mgr.Load()
		if loadErr != nil {
			return loadErr
		}
		quota.RecordAgentFailover(state, assignment, now, cooldown)
		return mgr.SaveUnlocked(state)
	}); err != nil {
		result.Error = "session changed but quota state was not persisted: " + err.Error()
		result.Changed = true
		return result
	}

	result.Changed = true
	return result
}
