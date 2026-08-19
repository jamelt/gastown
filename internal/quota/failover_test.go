package quota

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/config"
)

func routeAndProvider(routes map[string][]string, providers map[string]string) (AgentRouteResolver, AgentProviderResolver) {
	routeResolver := func(result ScanResult) ([]string, error) {
		route, ok := routes[result.Session]
		if !ok {
			return nil, errors.New("route not found")
		}
		return route, nil
	}
	providerResolver := func(agent string, _ ScanResult) (string, error) {
		provider, ok := providers[agent]
		if !ok {
			return "", errors.New("provider not found")
		}
		return provider, nil
	}
	return routeResolver, providerResolver
}

func TestPlanAgentFailoverUsesFirstDifferentProvider(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	results := []ScanResult{{Session: "hq-mayor", Agent: "claude-opus-5", Role: "mayor", RateLimited: true, MatchedLine: "usage quota exhausted"}}
	routes := map[string][]string{"hq-mayor": {"claude-opus-5", "codex-sol-high", "openrouter-qwen38-max"}}
	providers := map[string]string{"claude-opus-5": "anthropic", "codex-sol-high": "openai", "openrouter-qwen38-max": "openrouter"}
	routeResolver, providerResolver := routeAndProvider(routes, providers)

	plan, err := PlanAgentFailover(results, &config.QuotaState{}, &config.AgentFailoverConfig{Enabled: true}, now, routeResolver, providerResolver)
	if err != nil {
		t.Fatal(err)
	}
	assignment := plan.Assignments["hq-mayor"]
	if assignment.NextAgent != "codex-sol-high" || assignment.NextProvider != "openai" {
		t.Fatalf("assignment = %+v", assignment)
	}
}

func TestPlanAgentFailoverSkipsProvidersLimitedInSameScan(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	results := []ScanResult{
		{Session: "hq-mayor", Agent: "claude-opus-5", RateLimited: true},
		{Session: "gt-polecat", Agent: "codex-terra", RateLimited: true},
	}
	routes := map[string][]string{
		"hq-mayor":   {"claude-opus-5", "codex-sol-high", "openrouter-qwen38-max"},
		"gt-polecat": {"codex-terra", "claude-sonnet-5", "openrouter-deepseek-v4-pro"},
	}
	providers := map[string]string{
		"claude-opus-5": "anthropic", "codex-sol-high": "openai", "openrouter-qwen38-max": "openrouter",
		"codex-terra": "openai", "claude-sonnet-5": "anthropic", "openrouter-deepseek-v4-pro": "openrouter",
	}
	routeResolver, providerResolver := routeAndProvider(routes, providers)

	plan, err := PlanAgentFailover(results, &config.QuotaState{}, &config.AgentFailoverConfig{Enabled: true, MaxPerSession: 2}, now, routeResolver, providerResolver)
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Assignments["hq-mayor"].NextAgent; got != "openrouter-qwen38-max" {
		t.Fatalf("mayor next = %q, want openrouter-qwen38-max", got)
	}
	if got := plan.Assignments["gt-polecat"].NextAgent; got != "openrouter-deepseek-v4-pro" {
		t.Fatalf("polecat next = %q, want openrouter-deepseek-v4-pro", got)
	}
}

func TestPlanAgentFailoverHonorsCooldownAndMaximum(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	state := &config.QuotaState{
		Providers: map[string]config.ProviderQuotaState{
			"openai": {CooldownUntil: now.Add(30 * time.Minute).Format(time.RFC3339)},
		},
	}
	results := []ScanResult{{Session: "hq-mayor", Agent: "claude-opus-5", RateLimited: true}}
	routes := map[string][]string{"hq-mayor": {"claude-opus-5", "codex-sol-high", "openrouter-qwen38-max"}}
	providers := map[string]string{"claude-opus-5": "anthropic", "codex-sol-high": "openai", "openrouter-qwen38-max": "openrouter"}
	routeResolver, providerResolver := routeAndProvider(routes, providers)

	plan, err := PlanAgentFailover(results, state, &config.AgentFailoverConfig{Enabled: true, MaxPerSession: 1}, now, routeResolver, providerResolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Assignments) != 0 {
		t.Fatalf("expected no assignment, got %+v", plan.Assignments)
	}
	if !strings.Contains(plan.Skipped["hq-mayor"], "no eligible backup") {
		t.Fatalf("unexpected skip reason: %q", plan.Skipped["hq-mayor"])
	}
}

func TestPlanAgentFailoverIgnoresNonHardFailures(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	results := []ScanResult{{Session: "hq-mayor", Agent: "claude-opus-5", NearLimit: true, MatchedLine: "ordinary task failed"}}
	routeResolver, providerResolver := routeAndProvider(map[string][]string{}, map[string]string{})
	plan, err := PlanAgentFailover(results, &config.QuotaState{}, &config.AgentFailoverConfig{Enabled: true}, now, routeResolver, providerResolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.LimitedSessions) != 0 || len(plan.Assignments) != 0 {
		t.Fatalf("non-hard failure planned: %+v", plan)
	}
}

func TestPlanAgentFailoverIncludeNearLimitPlansBeforeHardLimit(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	results := []ScanResult{{Session: "hq-mayor", Agent: "claude-opus-5", NearLimit: true, MatchedLine: "98% of session limit used"}}
	routes := map[string][]string{"hq-mayor": {"claude-opus-5", "codex-sol-high", "openrouter-qwen38-max"}}
	providers := map[string]string{"claude-opus-5": "anthropic", "codex-sol-high": "openai", "openrouter-qwen38-max": "openrouter"}
	routeResolver, providerResolver := routeAndProvider(routes, providers)

	plan, err := PlanAgentFailover(results, &config.QuotaState{}, &config.AgentFailoverConfig{Enabled: true, IncludeNearLimit: true}, now, routeResolver, providerResolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.LimitedSessions) != 0 {
		t.Fatalf("expected no hard-limited sessions, got %+v", plan.LimitedSessions)
	}
	if len(plan.NearLimitSessions) != 1 {
		t.Fatalf("expected 1 near-limit session, got %+v", plan.NearLimitSessions)
	}
	assignment := plan.Assignments["hq-mayor"]
	if assignment.NextAgent != "codex-sol-high" || assignment.NextProvider != "openai" {
		t.Fatalf("assignment = %+v", assignment)
	}
}

func TestPlanAgentFailoverNearLimitDoesNotExcludeOwnProvider(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	results := []ScanResult{
		{Session: "hq-mayor", Agent: "claude-opus-5", NearLimit: true, MatchedLine: "98% of session limit used"},
		{Session: "gt-polecat", Agent: "codex-terra", RateLimited: true},
	}
	routes := map[string][]string{
		"hq-mayor":   {"claude-opus-5", "codex-sol-high"},
		"gt-polecat": {"codex-terra", "claude-opus-5"},
	}
	providers := map[string]string{
		"claude-opus-5": "anthropic", "codex-sol-high": "openai", "codex-terra": "openai",
	}
	routeResolver, providerResolver := routeAndProvider(routes, providers)

	plan, err := PlanAgentFailover(results, &config.QuotaState{}, &config.AgentFailoverConfig{Enabled: true, IncludeNearLimit: true}, now, routeResolver, providerResolver)
	if err != nil {
		t.Fatal(err)
	}
	// gt-polecat's hard-limited provider (openai) must still exclude codex-sol-high
	// as a candidate for hq-mayor, but hq-mayor's own near-limit provider
	// (anthropic) must not exclude itself as a candidate for gt-polecat.
	if got := plan.Assignments["gt-polecat"].NextAgent; got != "claude-opus-5" {
		t.Fatalf("polecat next = %q, want claude-opus-5 (near-limit must not self-exclude)", got)
	}
	if _, assigned := plan.Assignments["hq-mayor"]; assigned {
		t.Fatalf("mayor should have no eligible backup (only candidate's provider is hard-limited): %+v", plan.Assignments["hq-mayor"])
	}
}

func TestRecordAgentFailoverPersistsProviderAndSessionState(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	state := &config.QuotaState{}
	assignment := AgentFailoverAssignment{
		Session: "hq-mayor", CurrentProvider: "anthropic", NextAgent: "codex-sol-high", FailoverCount: 1, Cause: "quota exhausted",
	}
	RecordAgentFailover(state, assignment, now, time.Hour)
	if state.AgentSessions["hq-mayor"].CurrentAgent != "codex-sol-high" {
		t.Fatalf("session state = %+v", state.AgentSessions["hq-mayor"])
	}
	if got := state.Providers["anthropic"].CooldownUntil; got != now.Add(time.Hour).Format(time.RFC3339) {
		t.Fatalf("cooldown = %q", got)
	}
}

func agentProviderMap(providers map[string]string) AgentProviderResolverFunc {
	return func(agent string) (string, error) {
		provider, ok := providers[agent]
		if !ok {
			return "", errors.New("provider not found")
		}
		return provider, nil
	}
}

func TestSelectAvailableAgentReturnsFirstWhenNotCoolingDown(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	route := []string{"codex-terra", "claude-sonnet-5"}
	providerResolver := agentProviderMap(map[string]string{"codex-terra": "openai", "claude-sonnet-5": "anthropic"})

	agent, provider, substituted := SelectAvailableAgent(&config.QuotaState{}, route, now, providerResolver)
	if agent != "codex-terra" || provider != "openai" || substituted {
		t.Fatalf("agent=%q provider=%q substituted=%v", agent, provider, substituted)
	}
}

func TestSelectAvailableAgentSubstitutesWhenFirstIsCoolingDown(t *testing.T) {
	now := time.Date(2026, 8, 19, 1, 0, 0, 0, time.UTC)
	route := []string{"codex-terra", "claude-sonnet-5"}
	providerResolver := agentProviderMap(map[string]string{"codex-terra": "openai", "claude-sonnet-5": "anthropic"})
	state := &config.QuotaState{Providers: map[string]config.ProviderQuotaState{
		"openai": {CooldownUntil: now.Add(time.Hour).Format(time.RFC3339)},
	}}

	agent, provider, substituted := SelectAvailableAgent(state, route, now, providerResolver)
	if agent != "claude-sonnet-5" || provider != "anthropic" || !substituted {
		t.Fatalf("agent=%q provider=%q substituted=%v", agent, provider, substituted)
	}
}

func TestSelectAvailableAgentFallsBackToFirstWhenAllCoolingDown(t *testing.T) {
	now := time.Date(2026, 8, 19, 1, 0, 0, 0, time.UTC)
	route := []string{"codex-terra", "claude-sonnet-5"}
	providerResolver := agentProviderMap(map[string]string{"codex-terra": "openai", "claude-sonnet-5": "anthropic"})
	cooldown := now.Add(time.Hour).Format(time.RFC3339)
	state := &config.QuotaState{Providers: map[string]config.ProviderQuotaState{
		"openai":    {CooldownUntil: cooldown},
		"anthropic": {CooldownUntil: cooldown},
	}}

	agent, provider, substituted := SelectAvailableAgent(state, route, now, providerResolver)
	if agent != "codex-terra" || provider != "" || substituted {
		t.Fatalf("agent=%q provider=%q substituted=%v", agent, provider, substituted)
	}
}

func TestSelectAvailableAgentPastCooldownUsesFirst(t *testing.T) {
	now := time.Date(2026, 8, 19, 3, 0, 0, 0, time.UTC)
	route := []string{"codex-terra", "claude-sonnet-5"}
	providerResolver := agentProviderMap(map[string]string{"codex-terra": "openai", "claude-sonnet-5": "anthropic"})
	state := &config.QuotaState{Providers: map[string]config.ProviderQuotaState{
		"openai": {CooldownUntil: now.Add(-time.Hour).Format(time.RFC3339)},
	}}

	agent, _, substituted := SelectAvailableAgent(state, route, now, providerResolver)
	if agent != "codex-terra" || substituted {
		t.Fatalf("expected expired cooldown to leave primary agent, got agent=%q substituted=%v", agent, substituted)
	}
}
