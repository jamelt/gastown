//go:build integration

package cmd

import (
	"context"
	"flag"
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
	// Force sequential test execution to avoid bd file locks on Windows.
	_ = flag.Set("test.parallel", "1")
	flag.Parse()

	// Start an ephemeral Dolt container for this package's integration tests.
	// Tests like TestAgentWorktreesStayClean and TestBeadsRoutingFromTownRoot
	// spawn gt/bd subprocesses that create databases (e.g., "tr", "hq").
	// By routing to an isolated container (via GT_DOLT_PORT), those databases
	// are destroyed when the container is terminated at cleanup —
	// preventing orphan accumulation in the shared production Dolt data dir.
	if err := testutil.EnsureDoltContainerForTestMain(); err != nil {
		fmt.Fprintf(os.Stderr, "integration TestMain: dolt setup: %v\n", err)
		os.Exit(1)
	}

	testRoot, err := os.MkdirTemp("", "gt-test-cmd-integration-town-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration TestMain: create isolated town: %v\n", err)
		testutil.TerminateDoltContainer()
		os.Exit(1)
	}
	beadsDir := filepath.Join(testRoot, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "integration TestMain: create isolated beads dir: %v\n", err)
		_ = os.RemoveAll(testRoot)
		testutil.TerminateDoltContainer()
		os.Exit(1)
	}
	socket := fmt.Sprintf("gt-test-cmd-integration-%d-%d", os.Getpid(), time.Now().UnixNano())
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

	code := m.Run()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = exec.CommandContext(ctx, "tmux", "-L", socket, "kill-server").Run()
	cancel()
	_ = os.Remove(filepath.Join(tmux.SocketDir(), socket))
	tmux.SetDefaultSocket("")
	_ = os.RemoveAll(testRoot)
	// Clean up the shared Dolt container.
	testutil.TerminateDoltContainer()
	os.Exit(code)
}
