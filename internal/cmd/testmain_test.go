//go:build !integration

package cmd

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

// TestMain prevents command-package tests that exercise real tmux behavior
// from inheriting the live town socket when go test is launched from a town.
func TestMain(m *testing.M) {
	testRoot, err := os.MkdirTemp("", "gt-test-cmd-town-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "cmd TestMain: create isolated town: %v\n", err)
		os.Exit(1)
	}
	beadsDir := filepath.Join(testRoot, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "cmd TestMain: create isolated beads dir: %v\n", err)
		_ = os.RemoveAll(testRoot)
		os.Exit(1)
	}
	socket := fmt.Sprintf("gt-test-cmd-%d-%d", os.Getpid(), time.Now().UnixNano())
	tmux.SetDefaultSocket(socket)
	for key, value := range map[string]string{
		"GT_ROOT":        testRoot,
		"GT_TOWN_ROOT":   testRoot,
		"GT_TMUX_SOCKET": socket,
		"GT_TOWN_SOCKET": socket,
		"BEADS_DIR":      beadsDir,
	} {
		_ = os.Setenv(key, value)
	}
	if err := testutil.EnsureDoltContainerForTestMain(); err != nil {
		fmt.Fprintf(os.Stderr, "cmd TestMain: isolated Dolt unavailable (%v); Dolt-dependent tests will skip\n", err)
	}

	code := m.Run()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = exec.CommandContext(ctx, "tmux", "-L", socket, "kill-server").Run()
	cancel()
	_ = os.Remove(filepath.Join(tmux.SocketDir(), socket))
	testutil.TerminateDoltContainer()
	_ = os.RemoveAll(testRoot)
	tmux.SetDefaultSocket("")
	os.Exit(code)
}
