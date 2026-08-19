package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ResolveAgentRoute returns the ordered primary-plus-backups route for a
// session. Role routes take precedence when currentAgent belongs to that
// route. Otherwise an explicit agent_backups route is used, which supports
// formula-selected agents such as an Expeditor or council chair running in a
// polecat session.
func ResolveAgentRoute(role, currentAgent, townRoot, rigPath string) ([]string, error) {
	townSettings, err := LoadOrCreateTownSettings(TownSettingsPath(townRoot))
	if err != nil {
		return nil, fmt.Errorf("loading town settings: %w", err)
	}

	_ = LoadAgentRegistry(DefaultAgentRegistryPath(townRoot))

	var rigSettings *RigSettings
	if rigPath != "" {
		rigSettings, _ = LoadRigSettings(RigSettingsPath(rigPath))
		_ = LoadRigAgentRegistry(RigAgentRegistryPath(rigPath))
	}

	primary := ""
	if rigSettings != nil {
		primary = strings.TrimSpace(rigSettings.RoleAgents[role])
	}
	if primary == "" {
		primary = strings.TrimSpace(townSettings.RoleAgents[role])
	}
	if primary == "" {
		if rigSettings != nil {
			primary = strings.TrimSpace(rigSettings.Agent)
		}
		if primary == "" {
			primary = strings.TrimSpace(townSettings.DefaultAgent)
		}
	}
	if primary == "" {
		primary = "claude"
	}

	var roleBackups []string
	if rigSettings != nil {
		if configured, ok := rigSettings.RoleAgentBackups[role]; ok {
			roleBackups = configured
		} else {
			roleBackups = townSettings.RoleAgentBackups[role]
		}
	} else {
		roleBackups = townSettings.RoleAgentBackups[role]
	}
	roleRoute := cleanAgentRoute(append([]string{primary}, roleBackups...))

	if currentAgent == "" || routeContains(roleRoute, currentAgent) {
		return roleRoute, nil
	}

	var explicitBackups []string
	var explicitFound bool
	if rigSettings != nil {
		explicitBackups, explicitFound = rigSettings.AgentBackups[currentAgent]
	}
	if !explicitFound {
		explicitBackups, explicitFound = townSettings.AgentBackups[currentAgent]
	}
	if explicitFound {
		return cleanAgentRoute(append([]string{currentAgent}, explicitBackups...)), nil
	}

	// Preserve an explicit, unconfigured override rather than silently moving
	// it into a role route that was never intended for it.
	return cleanAgentRoute([]string{currentAgent}), nil
}

// ResolveAgentQuotaProvider returns the shared allocation pool for an agent
// alias. quota_provider is authoritative; conservative name/command inference
// keeps older catalogs usable.
func ResolveAgentQuotaProvider(townRoot, rigPath, agentName string) (string, error) {
	rc, _, err := ResolveAgentConfigWithOverride(townRoot, rigPath, agentName)
	if err != nil {
		return "", err
	}
	if provider := strings.ToLower(strings.TrimSpace(rc.QuotaProvider)); provider != "" {
		return provider, nil
	}

	name := strings.ToLower(agentName)
	command := strings.ToLower(filepath.Base(rc.Command))
	switch {
	case strings.HasPrefix(name, "openrouter-"):
		return "openrouter", nil
	case strings.HasPrefix(name, "claude"), strings.Contains(command, "claude"):
		return "anthropic", nil
	case strings.HasPrefix(name, "codex"), command == "codex" || command == "codex.exe":
		return "openai", nil
	case strings.HasPrefix(name, "gemini"), strings.Contains(command, "gemini"):
		return "google", nil
	}
	if rc.Provider != "" {
		return strings.ToLower(rc.Provider), nil
	}
	return "unknown", nil
}

func cleanAgentRoute(route []string) []string {
	seen := make(map[string]bool, len(route))
	cleaned := make([]string, 0, len(route))
	for _, agent := range route {
		agent = strings.TrimSpace(agent)
		if agent == "" || seen[agent] {
			continue
		}
		seen[agent] = true
		cleaned = append(cleaned, agent)
	}
	return cleaned
}

func routeContains(route []string, agent string) bool {
	for _, candidate := range route {
		if candidate == agent {
			return true
		}
	}
	return false
}
