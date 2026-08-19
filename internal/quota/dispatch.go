package quota

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/config"
)

// ErrAllProvidersCoolingDown is returned by SelectRoleAgent when the primary
// agent for a role and every agent in its configured backup chain currently
// have a provider in active cooldown (mayor/quota.json providers[]).
// Callers should defer dispatch rather than spawn a session doomed to hit
// the same quota wall.
var ErrAllProvidersCoolingDown = errors.New("all providers in the configured backup chain are cooling down")

// SelectRoleAgent resolves the runtime agent config for a role the same way
// config.ResolveRoleAgentConfig does, but additionally skips candidates
// whose provider is in an active cooldown, walking the role's configured
// backup chain (role_agents + role_agent_backups) with the same route and
// cooldown logic the reactive `gt quota failover` engine already uses for
// live sessions (config.ResolveAgentRoute, providerCoolingDown).
//
// Cooldown-aware selection only applies when agent_failover is enabled and
// the role has a configured backup chain; otherwise this returns exactly
// what config.ResolveRoleAgentConfig would, so dispatch is unaffected for
// roles that haven't opted in.
//
// Returns the selected config and a human-readable reason (empty when the
// primary agent was used unchanged). An error is returned only when every
// candidate in the chain is cooling down — callers should defer dispatch
// rather than spawn with a nil config.
func SelectRoleAgent(role, townRoot, rigPath string, now time.Time) (*config.RuntimeConfig, string, error) {
	primary := config.ResolveRoleAgentConfig(role, townRoot, rigPath)
	if primary.ResolvedAgent == "" {
		return primary, "", nil
	}

	townSettings, err := config.LoadOrCreateTownSettings(config.TownSettingsPath(townRoot))
	if err != nil || townSettings.AgentFailover == nil || !townSettings.AgentFailover.Enabled {
		return primary, "", nil
	}

	route, err := config.ResolveAgentRoute(role, primary.ResolvedAgent, townRoot, rigPath)
	if err != nil || len(route) <= 1 {
		return primary, "", nil
	}

	mgr := NewManager(townRoot)
	state, err := mgr.Load()
	if err != nil {
		return primary, "", nil
	}

	var skipped []string
	for i, candidate := range route {
		provider, provErr := config.ResolveAgentQuotaProvider(townRoot, rigPath, candidate)
		if provErr == nil && provider != "" && providerCoolingDown(state, provider, now) {
			skipped = append(skipped, fmt.Sprintf("%s (%s cooling down)", candidate, provider))
			continue
		}
		if i == 0 {
			return primary, "", nil
		}
		rc, _, rcErr := config.ResolveAgentConfigWithOverride(townRoot, rigPath, candidate)
		if rcErr != nil {
			skipped = append(skipped, fmt.Sprintf("%s (resolve error: %v)", candidate, rcErr))
			continue
		}
		reason := fmt.Sprintf("dispatch cooldown failover: %s; selected %s", strings.Join(skipped, "; "), candidate)
		return rc, reason, nil
	}

	return nil, "", fmt.Errorf("%w: %s", ErrAllProvidersCoolingDown, strings.Join(skipped, "; "))
}

// SelectRoleAgentOrDefault wraps SelectRoleAgent for call sites that only
// want the best available agent for a role (runtime settings provisioning,
// readiness polling) and should silently fall back to the plain primary
// agent if cooldown-aware selection can't produce one — for example every
// provider in the chain is cooling down. The authoritative dispatch
// decision, including deferring when every candidate is cooling down,
// happens once in SelectRoleAgent's caller at session start.
func SelectRoleAgentOrDefault(role, townRoot, rigPath string, now time.Time) *config.RuntimeConfig {
	rc, _, err := SelectRoleAgent(role, townRoot, rigPath, now)
	if err != nil || rc == nil {
		return config.ResolveRoleAgentConfig(role, townRoot, rigPath)
	}
	return rc
}

// RecordDispatchFailover persists a dispatch-time cooldown failover decision
// to the same session state the reactive failover engine records to
// (mayor/quota.json agent_sessions[]), so the choice survives daemon
// restarts and is inspectable the same way as a reactive transition.
func RecordDispatchFailover(townRoot, sessionID, agent, reason string, now time.Time) error {
	mgr := NewManager(townRoot)
	return mgr.WithLock(func() error {
		state, err := mgr.Load()
		if err != nil {
			return err
		}
		if state.AgentSessions == nil {
			state.AgentSessions = make(map[string]config.AgentFailoverSessionState)
		}
		state.AgentSessions[sessionID] = config.AgentFailoverSessionState{
			CurrentAgent:   agent,
			LastFailoverAt: now.UTC().Format(time.RFC3339),
			LastCause:      reason,
		}
		return mgr.SaveUnlocked(state)
	})
}
