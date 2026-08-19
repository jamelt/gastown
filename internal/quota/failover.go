package quota

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/config"
)

const (
	defaultProviderCooldown = time.Hour
	defaultMaxFailovers     = 2
)

// AgentRouteResolver returns the ordered route for one scanned session.
type AgentRouteResolver func(ScanResult) ([]string, error)

// AgentProviderResolver maps an agent alias to its shared quota provider.
type AgentProviderResolver func(agent string, result ScanResult) (string, error)

// AgentFailoverAssignment is one strictly-forward provider transition.
type AgentFailoverAssignment struct {
	Session         string `json:"session"`
	Role            string `json:"role,omitempty"`
	CurrentAgent    string `json:"current_agent"`
	CurrentProvider string `json:"current_provider"`
	NextAgent       string `json:"next_agent"`
	NextProvider    string `json:"next_provider"`
	Cause           string `json:"cause,omitempty"`
	FailoverCount   int    `json:"failover_count"`
}

// AgentFailoverPlan is a deterministic, reviewable cross-provider plan.
type AgentFailoverPlan struct {
	LimitedSessions   []ScanResult                       `json:"limited_sessions"`
	NearLimitSessions []ScanResult                       `json:"near_limit_sessions,omitempty"`
	Assignments       map[string]AgentFailoverAssignment `json:"assignments"`
	Skipped           map[string]string                  `json:"skipped,omitempty"`
	ProviderCooldown  string                             `json:"provider_cooldown"`
	MaxPerSession     int                                `json:"max_per_session"`
}

// PlanAgentFailover plans strictly-forward transitions for hard-limited
// sessions and, when settings.IncludeNearLimit is set, for sessions
// approaching their limit as well — so a session can move off a dying
// provider before it takes the hard 429 instead of always reacting to one.
// It never plans from ordinary task failures. Providers observed as
// hard-limited anywhere in the scan are excluded as candidates for the
// entire plan; a near-limit session does not exclude its own provider,
// since the limit has not actually been confirmed there.
func PlanAgentFailover(
	results []ScanResult,
	state *config.QuotaState,
	settings *config.AgentFailoverConfig,
	now time.Time,
	routeResolver AgentRouteResolver,
	providerResolver AgentProviderResolver,
) (*AgentFailoverPlan, error) {
	if settings == nil || !settings.Enabled {
		return nil, fmt.Errorf("agent failover is disabled")
	}
	if state == nil {
		state = &config.QuotaState{}
	}

	cooldown := defaultProviderCooldown
	if raw := strings.TrimSpace(settings.ProviderCooldown); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("invalid provider_cooldown %q", raw)
		}
		cooldown = parsed
	}
	maxPerSession := settings.MaxPerSession
	if maxPerSession <= 0 {
		maxPerSession = defaultMaxFailovers
	}

	plan := &AgentFailoverPlan{
		Assignments:      make(map[string]AgentFailoverAssignment),
		Skipped:          make(map[string]string),
		ProviderCooldown: cooldown.String(),
		MaxPerSession:    maxPerSession,
	}

	// Cache route/provider lookups so the two planning passes are consistent.
	type resolvedSession struct {
		result          ScanResult
		route           []string
		currentAgent    string
		currentProvider string
	}
	var sessions []resolvedSession
	limitedProviders := make(map[string]bool)

	for _, result := range results {
		switch {
		case result.RateLimited:
			plan.LimitedSessions = append(plan.LimitedSessions, result)
		case settings.IncludeNearLimit && result.NearLimit:
			plan.NearLimitSessions = append(plan.NearLimitSessions, result)
		default:
			continue
		}

		route, err := routeResolver(result)
		if err != nil {
			plan.Skipped[result.Session] = "resolving route: " + err.Error()
			continue
		}
		if len(route) == 0 {
			plan.Skipped[result.Session] = "no configured route"
			continue
		}
		currentAgent := strings.TrimSpace(result.Agent)
		if currentAgent == "" {
			currentAgent = route[0]
		}
		currentProvider, err := providerResolver(currentAgent, result)
		if err != nil || currentProvider == "" {
			if err != nil {
				plan.Skipped[result.Session] = "resolving current provider: " + err.Error()
			} else {
				plan.Skipped[result.Session] = "current agent has no quota provider"
			}
			continue
		}
		if result.RateLimited {
			limitedProviders[currentProvider] = true
		}
		sessions = append(sessions, resolvedSession{
			result:          result,
			route:           route,
			currentAgent:    currentAgent,
			currentProvider: currentProvider,
		})
	}

	for _, session := range sessions {
		currentIndex := routeIndex(session.route, session.currentAgent)
		if currentIndex < 0 {
			plan.Skipped[session.result.Session] = fmt.Sprintf("active agent %q is not in its configured route", session.currentAgent)
			continue
		}

		used := currentIndex
		if currentIndex > 0 {
			if persisted := state.AgentSessions[session.result.Session].FailoverCount; persisted > used {
				used = persisted
			}
		}
		if used >= maxPerSession {
			plan.Skipped[session.result.Session] = fmt.Sprintf("maximum %d failovers reached", maxPerSession)
			continue
		}

		var skippedCandidates []string
		for candidateIndex := currentIndex + 1; candidateIndex < len(session.route); candidateIndex++ {
			if candidateIndex > maxPerSession {
				break
			}
			candidate := session.route[candidateIndex]
			provider, err := providerResolver(candidate, session.result)
			if err != nil || provider == "" {
				reason := "unknown provider"
				if err != nil {
					reason = err.Error()
				}
				skippedCandidates = append(skippedCandidates, candidate+": "+reason)
				continue
			}
			if limitedProviders[provider] {
				skippedCandidates = append(skippedCandidates, candidate+": provider "+provider+" is hard-limited")
				continue
			}
			if providerCoolingDown(state, provider, now) {
				skippedCandidates = append(skippedCandidates, candidate+": provider "+provider+" is cooling down")
				continue
			}

			plan.Assignments[session.result.Session] = AgentFailoverAssignment{
				Session:         session.result.Session,
				Role:            session.result.Role,
				CurrentAgent:    session.currentAgent,
				CurrentProvider: session.currentProvider,
				NextAgent:       candidate,
				NextProvider:    provider,
				Cause:           session.result.MatchedLine,
				FailoverCount:   candidateIndex,
			}
			break
		}
		if _, assigned := plan.Assignments[session.result.Session]; !assigned {
			reason := "no eligible backup remains"
			if len(skippedCandidates) > 0 {
				reason += " (" + strings.Join(skippedCandidates, "; ") + ")"
			}
			plan.Skipped[session.result.Session] = reason
		}
	}

	return plan, nil
}

// SortedFailoverSessions returns assignment keys in stable order.
func SortedFailoverSessions(plan *AgentFailoverPlan) []string {
	if plan == nil {
		return nil
	}
	sessions := make([]string, 0, len(plan.Assignments))
	for session := range plan.Assignments {
		sessions = append(sessions, session)
	}
	sort.Strings(sessions)
	return sessions
}

// RecordAgentFailover mutates loaded quota state after a successful transition.
// The caller is responsible for locking and persistence.
func RecordAgentFailover(state *config.QuotaState, assignment AgentFailoverAssignment, now time.Time, cooldown time.Duration) {
	if state.Providers == nil {
		state.Providers = make(map[string]config.ProviderQuotaState)
	}
	if state.AgentSessions == nil {
		state.AgentSessions = make(map[string]config.AgentFailoverSessionState)
	}
	state.Providers[assignment.CurrentProvider] = config.ProviderQuotaState{
		LimitedAt:     now.UTC().Format(time.RFC3339),
		CooldownUntil: now.Add(cooldown).UTC().Format(time.RFC3339),
		Reason:        assignment.Cause,
		SourceSession: assignment.Session,
	}
	state.AgentSessions[assignment.Session] = config.AgentFailoverSessionState{
		CurrentAgent:   assignment.NextAgent,
		FailoverCount:  assignment.FailoverCount,
		LastFailoverAt: now.UTC().Format(time.RFC3339),
		LastCause:      assignment.Cause,
	}
}

// SelectAvailableAgent returns the first agent in route whose quota provider
// is not currently cooling down per state, so a spawn can go straight to a
// known-good provider instead of paying a failed attempt against one that
// quota.json already recorded as limited. substituted is true when the
// chosen agent differs from route[0], so the caller can log the swap instead
// of leaving it silent. Falls back to route[0] if every candidate is cooling
// down (or a provider can't be resolved for a candidate), so this never
// blocks a spawn outright — it only skips a known-bad first attempt.
func SelectAvailableAgent(state *config.QuotaState, route []string, now time.Time, providerResolver AgentProviderResolverFunc) (agent, provider string, substituted bool) {
	if len(route) == 0 {
		return "", "", false
	}
	for i, candidate := range route {
		p, err := providerResolver(candidate)
		if err != nil || p == "" || !providerCoolingDown(state, p, now) {
			return candidate, p, i > 0
		}
	}
	return route[0], "", false
}

// AgentProviderResolverFunc maps an agent alias to its shared quota provider,
// without the ScanResult a reactive PlanAgentFailover pass carries.
type AgentProviderResolverFunc func(agent string) (string, error)

func providerCoolingDown(state *config.QuotaState, provider string, now time.Time) bool {
	if state == nil {
		return false
	}
	raw := state.Providers[provider].CooldownUntil
	if raw == "" {
		return false
	}
	until, err := time.Parse(time.RFC3339, raw)
	return err == nil && now.Before(until)
}

func routeIndex(route []string, agent string) int {
	for i, candidate := range route {
		if candidate == agent {
			return i
		}
	}
	return -1
}
