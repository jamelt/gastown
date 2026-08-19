package cmd

import (
	"errors"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/quota"
)

type mockAgentFailoverTmux struct {
	env        map[string]string
	pane       string
	respawnCmd string
	respawnErr error
	killed     bool
	cleared    bool
}

func (m *mockAgentFailoverTmux) GetEnvironment(_, key string) (string, error) {
	value, ok := m.env[key]
	if !ok {
		return "", errors.New("not set")
	}
	return value, nil
}

func (m *mockAgentFailoverTmux) SetEnvironment(_, key, value string) error {
	m.env[key] = value
	return nil
}

func (m *mockAgentFailoverTmux) GetPaneID(string) (string, error)   { return m.pane, nil }
func (m *mockAgentFailoverTmux) SetRemainOnExit(string, bool) error { return nil }
func (m *mockAgentFailoverTmux) KillPaneProcesses(string) error {
	m.killed = true
	return nil
}
func (m *mockAgentFailoverTmux) ClearHistory(string) error {
	m.cleared = true
	return nil
}
func (m *mockAgentFailoverTmux) RespawnPane(_, command string) error {
	m.respawnCmd = command
	return m.respawnErr
}
func (m *mockAgentFailoverTmux) AcceptStartupDialogs(string) error { return nil }

func TestExecuteAgentFailoverChangesRuntimeAndPersistsState(t *testing.T) {
	townRoot := t.TempDir()
	mgr := quota.NewManager(townRoot)
	tmux := &mockAgentFailoverTmux{
		env: map[string]string{
			"GT_AGENT":         "claude-opus-5",
			"GT_PROCESS_NAMES": "claude,node",
			"GT_QUOTA_ACCOUNT": "work",
		},
		pane: "%1",
	}
	assignment := quota.AgentFailoverAssignment{
		Session:         "hq-mayor",
		CurrentAgent:    "claude-opus-5",
		CurrentProvider: "anthropic",
		NextAgent:       "codex-sol-high",
		NextProvider:    "openai",
		Cause:           "usage quota exhausted",
		FailoverCount:   1,
	}

	result := executeAgentFailover(tmux, mgr, assignment, time.Hour, func(_, nextAgent string) (string, error) {
		return "exec " + nextAgent, nil
	})
	if !result.Changed || result.Error != "" {
		t.Fatalf("result = %+v", result)
	}
	if tmux.env["GT_AGENT"] != "codex-sol-high" {
		t.Fatalf("GT_AGENT = %q", tmux.env["GT_AGENT"])
	}
	if tmux.env["GT_QUOTA_ACCOUNT"] != "" {
		t.Fatalf("GT_QUOTA_ACCOUNT was not cleared: %q", tmux.env["GT_QUOTA_ACCOUNT"])
	}
	if !tmux.killed || !tmux.cleared || tmux.respawnCmd != "exec codex-sol-high" {
		t.Fatalf("tmux mutation incomplete: %+v", tmux)
	}
	state, err := mgr.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.AgentSessions["hq-mayor"].CurrentAgent != "codex-sol-high" {
		t.Fatalf("session state = %+v", state.AgentSessions["hq-mayor"])
	}
	if state.Providers["anthropic"].CooldownUntil == "" {
		t.Fatal("anthropic provider cooldown was not recorded")
	}
}

func TestExecuteAgentFailoverRestoresEnvironmentOnRespawnFailure(t *testing.T) {
	mgr := quota.NewManager(t.TempDir())
	tmux := &mockAgentFailoverTmux{
		env: map[string]string{
			"GT_AGENT":         "claude-opus-5",
			"GT_PROCESS_NAMES": "claude,node",
		},
		pane:       "%1",
		respawnErr: errors.New("respawn failed"),
	}
	assignment := quota.AgentFailoverAssignment{
		Session: "hq-mayor", CurrentAgent: "claude-opus-5", CurrentProvider: "anthropic", NextAgent: "codex-sol-high", NextProvider: "openai",
	}

	result := executeAgentFailover(tmux, mgr, assignment, time.Hour, func(_, nextAgent string) (string, error) {
		return "exec " + nextAgent, nil
	})
	if result.Changed || result.Error == "" {
		t.Fatalf("result = %+v", result)
	}
	if tmux.env["GT_AGENT"] != "claude-opus-5" || tmux.env["GT_PROCESS_NAMES"] != "claude,node" {
		t.Fatalf("environment was not restored: %+v", tmux.env)
	}
	state, err := mgr.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.AgentSessions) != 0 || len(state.Providers) != 0 {
		t.Fatalf("failed transition was persisted: %+v", state)
	}
}
