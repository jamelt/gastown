package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeRouteJSON(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveAgentRouteRoleAndExplicitAgent(t *testing.T) {
	townRoot := t.TempDir()
	writeRouteJSON(t, TownSettingsPath(townRoot), &TownSettings{
		Type:         "town-settings",
		Version:      1,
		DefaultAgent: "claude-opus-5",
		RoleAgents: map[string]string{
			"polecat": "codex-terra",
		},
		RoleAgentBackups: map[string][]string{
			"polecat": {"claude-sonnet-5", "openrouter-deepseek-v4-pro"},
		},
		AgentBackups: map[string][]string{
			"claude-opus-5": {"codex-sol-high", "openrouter-qwen38-max"},
		},
	})

	roleRoute, err := ResolveAgentRoute("polecat", "codex-terra", townRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	wantRole := []string{"codex-terra", "claude-sonnet-5", "openrouter-deepseek-v4-pro"}
	if !reflect.DeepEqual(roleRoute, wantRole) {
		t.Fatalf("role route = %v, want %v", roleRoute, wantRole)
	}

	explicitRoute, err := ResolveAgentRoute("polecat", "claude-opus-5", townRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	wantExplicit := []string{"claude-opus-5", "codex-sol-high", "openrouter-qwen38-max"}
	if !reflect.DeepEqual(explicitRoute, wantExplicit) {
		t.Fatalf("explicit route = %v, want %v", explicitRoute, wantExplicit)
	}
}

func TestResolveAgentRouteRigOverride(t *testing.T) {
	townRoot := t.TempDir()
	rigPath := filepath.Join(townRoot, "trader")
	writeRouteJSON(t, TownSettingsPath(townRoot), &TownSettings{
		Type:    "town-settings",
		Version: 1,
		RoleAgents: map[string]string{
			"witness": "claude-sonnet-5",
		},
		RoleAgentBackups: map[string][]string{
			"witness": {"codex-terra", "openrouter-gemini37-flash"},
		},
	})
	writeRouteJSON(t, RigSettingsPath(rigPath), &RigSettings{
		Type:    "rig-settings",
		Version: 1,
		RoleAgents: map[string]string{
			"witness": "custom-witness",
		},
		RoleAgentBackups: map[string][]string{
			"witness": {"custom-backup"},
		},
	})

	got, err := ResolveAgentRoute("witness", "custom-witness", townRoot, rigPath)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"custom-witness", "custom-backup"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("route = %v, want %v", got, want)
	}
}

func TestResolveAgentQuotaProviderFromCatalog(t *testing.T) {
	townRoot := t.TempDir()
	writeRouteJSON(t, TownSettingsPath(townRoot), &TownSettings{Type: "town-settings", Version: 1})
	writeRouteJSON(t, DefaultAgentRegistryPath(townRoot), map[string]any{
		"version": 1,
		"agents": map[string]any{
			"frontier": map[string]any{
				"name":           "frontier",
				"command":        "custom-runtime",
				"args":           []string{},
				"quota_provider": "provider-a",
			},
		},
	})

	got, err := ResolveAgentQuotaProvider(townRoot, "", "frontier")
	if err != nil {
		t.Fatal(err)
	}
	if got != "provider-a" {
		t.Fatalf("provider = %q, want provider-a", got)
	}
}
