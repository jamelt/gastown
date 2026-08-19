package hooks

import (
	"os"
	"strings"
	"testing"
)

// TestTestIsolationGuardWiredInBothInstallPaths ensures the gt-x3yy `go test`
// isolation guard is present in BOTH polecat install paths — the Go
// DefaultBase() (used by the ComputeExpected/sync backfill) and the autonomous
// Claude template settings-autonomous.json (installed directly at spawn) — so a
// future edit to one cannot silently drop the guard from the other.
func TestTestIsolationGuardWiredInBothInstallPaths(t *testing.T) {
	const guardCmd = "tap guard test-isolation"

	foundInDefaultBase := false
	for _, entry := range DefaultBase().PreToolUse {
		for _, h := range entry.Hooks {
			if strings.Contains(h.Command, guardCmd) {
				foundInDefaultBase = true
			}
		}
	}
	if !foundInDefaultBase {
		t.Error("DefaultBase() PreToolUse is missing the test-isolation guard")
	}

	data, err := os.ReadFile("templates/claude/settings-autonomous.json")
	if err != nil {
		t.Fatalf("reading autonomous template: %v", err)
	}
	if !strings.Contains(string(data), guardCmd) {
		t.Error("templates/claude/settings-autonomous.json is missing the test-isolation guard")
	}
}
