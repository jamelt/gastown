package doltserver

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	testRoot, err := os.MkdirTemp("", "gt-test-doltserver-town-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "doltserver TestMain: create isolated town: %v\n", err)
		os.Exit(1)
	}
	beadsDir := filepath.Join(testRoot, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "doltserver TestMain: create isolated beads dir: %v\n", err)
		_ = os.RemoveAll(testRoot)
		os.Exit(1)
	}
	for _, key := range []string{
		"GT_DOLT_PORT", "GT_DOLT_HOST", "GT_DOLT_DATA",
		"BEADS_DOLT_PORT", "BEADS_DOLT_SERVER_PORT", "BEADS_DOLT_SERVER_HOST",
		"BEADS_DOLT_SERVER_DATABASE", "BEADS_DB", "BD_DB", "BD_ACTOR",
	} {
		_ = os.Unsetenv(key)
	}
	_ = os.Setenv("GT_ROOT", testRoot)
	_ = os.Setenv("GT_TOWN_ROOT", testRoot)
	_ = os.Setenv("BEADS_DIR", beadsDir)
	_ = os.Setenv("BEADS_DOLT_AUTO_START", "0")

	code := m.Run()
	_ = os.RemoveAll(testRoot)
	os.Exit(code)
}
