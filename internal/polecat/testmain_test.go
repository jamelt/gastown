package polecat

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/testutil"
	"github.com/steveyegge/gastown/internal/tmux"
)

func TestMain(m *testing.M) {
	testRoot, err := os.MkdirTemp("", "gt-test-polecat-town-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "polecat TestMain: create isolated town: %v\n", err)
		os.Exit(1)
	}
	beadsDir := filepath.Join(testRoot, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "polecat TestMain: create isolated beads dir: %v\n", err)
		_ = os.RemoveAll(testRoot)
		os.Exit(1)
	}
	socket := fmt.Sprintf("gt-test-polecat-%d-%d", os.Getpid(), time.Now().UnixNano())
	for key, value := range map[string]string{
		"GT_ROOT":        testRoot,
		"GT_TOWN_ROOT":   testRoot,
		"GT_TMUX_SOCKET": socket,
		"GT_TOWN_SOCKET": socket,
		"BEADS_DIR":      beadsDir,
	} {
		if err := os.Setenv(key, value); err != nil {
			fmt.Fprintf(os.Stderr, "polecat TestMain: set %s: %v\n", key, err)
			_ = os.RemoveAll(testRoot)
			os.Exit(1)
		}
	}
	tmux.SetDefaultSocket(socket)

	// Integration tests must never fall back to the live town's Dolt server.
	// If Docker is unavailable, RequireDoltContainer skips Dolt-dependent tests.
	if err := testutil.EnsureDoltContainerForTestMain(); err != nil {
		fmt.Fprintf(os.Stderr, "polecat TestMain: isolated Dolt unavailable (%v); Dolt-dependent tests will skip\n", err)
	}

	code := m.Run()

	// Bound cleanup so a wedged tmux server cannot hang the test command.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = exec.CommandContext(ctx, "tmux", "-L", socket, "kill-server").Run()
	cancel()
	_ = os.Remove(filepath.Join(tmux.SocketDir(), socket))
	testutil.TerminateDoltContainer()
	_ = os.RemoveAll(testRoot)
	tmux.SetDefaultSocket("")
	os.Exit(code)
}
