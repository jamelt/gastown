package quota

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/config"
)

func writeDispatchTownSettings(t *testing.T, townRoot string, settings *config.TownSettings) {
	t.Helper()
	path := config.TownSettingsPath(townRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// dispatchTestTownSettings reproduces the real-world configuration from
// gt-lovb's RCA: role_agents.polecat=codex-terra (an OpenAI agent), with a
// declared backup chain of [claude-sonnet-5, openrouter-deepseek-v4-pro],
// and agent_failover enabled. Custom Agents entries (rather than a
// registered preset) let resolution succeed without a real binary on PATH.
func dispatchTestTownSettings() *config.TownSettings {
	return &config.TownSettings{
		Type:    "town-settings",
		Version: 1,
		RoleAgents: map[string]string{
			"polecat": "codex-terra",
		},
		RoleAgentBackups: map[string][]string{
			"polecat": {"claude-sonnet-5", "openrouter-deepseek-v4-pro"},
		},
		AgentFailover: &config.AgentFailoverConfig{Enabled: true},
		Agents: map[string]*config.RuntimeConfig{
			"codex-terra":                {QuotaProvider: "openai"},
			"claude-sonnet-5":            {QuotaProvider: "anthropic"},
			"openrouter-deepseek-v4-pro": {QuotaProvider: "openrouter"},
		},
	}
}

func TestSelectRoleAgentNoCooldownReturnsPrimaryUnchanged(t *testing.T) {
	townRoot := t.TempDir()
	writeDispatchTownSettings(t, townRoot, dispatchTestTownSettings())
	now := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)

	rc, reason, err := SelectRoleAgent("polecat", townRoot, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if rc.ResolvedAgent != "codex-terra" {
		t.Fatalf("ResolvedAgent = %q, want codex-terra", rc.ResolvedAgent)
	}
	if reason != "" {
		t.Fatalf("reason = %q, want empty when nothing is cooling down", reason)
	}
}

// TestSelectRoleAgentSkipsCooledProviderPicksBackup reproduces gt-lovb: a
// polecat dispatched while openai (codex-terra's provider) is hard-limited
// must not receive codex-terra. It must fall through to the first backup
// whose provider isn't cooling down.
func TestSelectRoleAgentSkipsCooledProviderPicksBackup(t *testing.T) {
	townRoot := t.TempDir()
	writeDispatchTownSettings(t, townRoot, dispatchTestTownSettings())
	now := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)

	mgr := NewManager(townRoot)
	if err := mgr.Save(&config.QuotaState{
		Providers: map[string]config.ProviderQuotaState{
			"openai": {CooldownUntil: now.Add(time.Hour).UTC().Format(time.RFC3339)},
		},
	}); err != nil {
		t.Fatal(err)
	}

	rc, reason, err := SelectRoleAgent("polecat", townRoot, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if rc.ResolvedAgent == "codex-terra" {
		t.Fatalf("selected agent from cooled provider: got %q", rc.ResolvedAgent)
	}
	if rc.ResolvedAgent != "claude-sonnet-5" {
		t.Fatalf("ResolvedAgent = %q, want claude-sonnet-5 (first non-cooled backup)", rc.ResolvedAgent)
	}
	if reason == "" || !strings.Contains(reason, "codex-terra") || !strings.Contains(reason, "claude-sonnet-5") {
		t.Fatalf("reason = %q, want it to record the skip and the selection", reason)
	}
}

// TestSelectRoleAgentAllProvidersCoolingDownDefers covers the acceptance
// criterion that dispatch defers with a clear reason, instead of spawning a
// doomed session, when every provider in the configured chain is cooling
// down.
func TestSelectRoleAgentAllProvidersCoolingDownDefers(t *testing.T) {
	townRoot := t.TempDir()
	writeDispatchTownSettings(t, townRoot, dispatchTestTownSettings())
	now := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	cooled := now.Add(time.Hour).UTC().Format(time.RFC3339)

	mgr := NewManager(townRoot)
	if err := mgr.Save(&config.QuotaState{
		Providers: map[string]config.ProviderQuotaState{
			"openai":     {CooldownUntil: cooled},
			"anthropic":  {CooldownUntil: cooled},
			"openrouter": {CooldownUntil: cooled},
		},
	}); err != nil {
		t.Fatal(err)
	}

	rc, _, err := SelectRoleAgent("polecat", townRoot, "", now)
	if err == nil {
		t.Fatalf("expected an error deferring dispatch, got rc=%+v", rc)
	}
	if !errors.Is(err, ErrAllProvidersCoolingDown) {
		t.Fatalf("err = %v, want it to wrap ErrAllProvidersCoolingDown", err)
	}
	if rc != nil {
		t.Fatalf("expected nil config when deferring dispatch, got %+v", rc)
	}
}

// TestSelectRoleAgentDisabledFailoverIgnoresCooldown ensures dispatch-time
// selection is opt-in via the same agent_failover flag as the reactive
// engine: existing towns that haven't enabled it see no behavior change.
func TestSelectRoleAgentDisabledFailoverIgnoresCooldown(t *testing.T) {
	townRoot := t.TempDir()
	settings := dispatchTestTownSettings()
	settings.AgentFailover = &config.AgentFailoverConfig{Enabled: false}
	writeDispatchTownSettings(t, townRoot, settings)
	now := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)

	mgr := NewManager(townRoot)
	if err := mgr.Save(&config.QuotaState{
		Providers: map[string]config.ProviderQuotaState{
			"openai": {CooldownUntil: now.Add(time.Hour).UTC().Format(time.RFC3339)},
		},
	}); err != nil {
		t.Fatal(err)
	}

	rc, reason, err := SelectRoleAgent("polecat", townRoot, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if rc.ResolvedAgent != "codex-terra" || reason != "" {
		t.Fatalf("agent_failover disabled should be a no-op: rc=%+v reason=%q", rc, reason)
	}
}

func TestSelectRoleAgentOrDefaultFallsBackWhenAllCoolingDown(t *testing.T) {
	townRoot := t.TempDir()
	writeDispatchTownSettings(t, townRoot, dispatchTestTownSettings())
	now := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	cooled := now.Add(time.Hour).UTC().Format(time.RFC3339)

	mgr := NewManager(townRoot)
	if err := mgr.Save(&config.QuotaState{
		Providers: map[string]config.ProviderQuotaState{
			"openai":     {CooldownUntil: cooled},
			"anthropic":  {CooldownUntil: cooled},
			"openrouter": {CooldownUntil: cooled},
		},
	}); err != nil {
		t.Fatal(err)
	}

	rc := SelectRoleAgentOrDefault("polecat", townRoot, "", now)
	if rc == nil || rc.ResolvedAgent != "codex-terra" {
		t.Fatalf("expected silent fallback to the plain primary agent, got %+v", rc)
	}
}
