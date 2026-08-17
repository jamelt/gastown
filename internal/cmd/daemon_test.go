package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDaemonStatusJSONCompatibility(t *testing.T) {
	if daemonStatusCmd.Flags().Lookup("json") == nil {
		t.Fatal("gt daemon status is missing --json")
	}

	encoded, err := json.Marshal(daemonStatusOutput{Running: true, PID: 42, Town: "/tmp/town"})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"running", "pid", "town", "binary_newer", "sessions"} {
		if _, ok := decoded[field]; !ok {
			t.Errorf("daemon status JSON missing %q", field)
		}
	}
}

func TestReadDaemonStartupFailure(t *testing.T) {
	townRoot := t.TempDir()
	daemonDir := filepath.Join(townRoot, "daemon")
	if err := os.MkdirAll(daemonDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	logData := "" +
		"2026/03/28 22:00:00 Daemon startup failed (PID 111): stale error\n" +
		"2026/03/28 22:00:01 Daemon startup failed (PID 222): incompatible beads workspace / gt binary combination\n"
	if err := os.WriteFile(filepath.Join(daemonDir, "daemon.log"), []byte(logData), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got := readDaemonStartupFailure(townRoot, 222)
	want := "incompatible beads workspace / gt binary combination"
	if got != want {
		t.Fatalf("readDaemonStartupFailure() = %q, want %q", got, want)
	}
}

func TestReadDaemonStartupFailure_MissingPIDReturnsEmpty(t *testing.T) {
	townRoot := t.TempDir()
	daemonDir := filepath.Join(townRoot, "daemon")
	if err := os.MkdirAll(daemonDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(daemonDir, "daemon.log"), []byte("2026/03/28 22:00:00 Daemon startup failed (PID 111): stale error\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if got := readDaemonStartupFailure(townRoot, 222); got != "" {
		t.Fatalf("readDaemonStartupFailure() = %q, want empty string", got)
	}
}
