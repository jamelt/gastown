package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/doltserver"
)

func TestDoltLineageCheckReportsIndependentHistories(t *testing.T) {
	townRoot := t.TempDir()
	beadsDir := filepath.Join(townRoot, "testrig", ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "config.yaml"), []byte("sync.remote: file:///remote\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(`{"dolt_database":"rigdb"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	original := inspectDoltLineageFn
	t.Cleanup(func() { inspectDoltLineageFn = original })
	inspectDoltLineageFn = func(_, _ string) (doltserver.LineageReport, error) {
		return doltserver.LineageReport{
			Database: "rigdb", State: doltserver.LineageDiverged,
			LocalHead: "local-head", RemoteHead: "remote-head",
			LocalOnlyRecords: 7, RemoteOnlyRecords: 11,
		}, nil
	}

	result := NewDoltLineageCheck().Run(&CheckContext{TownRoot: townRoot, RigName: "testrig"})
	if result.Status != StatusError {
		t.Fatalf("status = %v, want error: %#v", result.Status, result)
	}
	details := strings.Join(result.Details, "\n")
	for _, want := range []string{"local-head", "remote-head", "records=7", "records=11"} {
		if !strings.Contains(details, want) {
			t.Fatalf("details missing %q: %s", want, details)
		}
	}
	if !strings.Contains(result.FixHint, "gt dolt reconcile --db rigdb") {
		t.Fatalf("unexpected fix hint: %s", result.FixHint)
	}
}

func TestDoltLineageCheckWarnsOnUnregisteredRemote(t *testing.T) {
	townRoot := t.TempDir()
	beadsDir := filepath.Join(townRoot, "testrig", ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "config.yaml"), []byte("sync.remote: file:///remote\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(`{"dolt_database":"rigdb"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	original := inspectDoltLineageFn
	t.Cleanup(func() { inspectDoltLineageFn = original })
	inspectDoltLineageFn = func(_, _ string) (doltserver.LineageReport, error) {
		return doltserver.LineageReport{Database: "rigdb", State: doltserver.LineageNoRemote}, nil
	}

	result := NewDoltLineageCheck().Run(&CheckContext{TownRoot: townRoot, RigName: "testrig"})
	if result.Status != StatusWarning {
		t.Fatalf("status = %v, want warning: %#v", result.Status, result)
	}
	if !strings.Contains(result.Message, "no registered remote") {
		t.Fatalf("unexpected message: %s", result.Message)
	}
	if !strings.Contains(result.FixHint, "rigdb") {
		t.Fatalf("unexpected fix hint: %s", result.FixHint)
	}
}

func TestDoltLineageCheckFlagsRemoteDivergingFromGitIntent(t *testing.T) {
	townRoot := t.TempDir()
	beadsDir := filepath.Join(townRoot, "testrig", ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "config.yaml"), []byte("sync.remote: file:///remote\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(`{"dolt_database":"rigdb"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	originalInspect := inspectDoltLineageFn
	originalLoadRig := loadRigEntryFn
	t.Cleanup(func() {
		inspectDoltLineageFn = originalInspect
		loadRigEntryFn = originalLoadRig
	})
	inspectDoltLineageFn = func(_, _ string) (doltserver.LineageReport, error) {
		return doltserver.LineageReport{
			Database: "rigdb", State: doltserver.LineageShared,
			RemoteName: "origin", RemoteURL: "https://github.com/steveyegge/gastown.git",
		}, nil
	}
	loadRigEntryFn = func(_, rigName string) (config.RigEntry, bool) {
		if rigName != "testrig" {
			return config.RigEntry{}, false
		}
		return config.RigEntry{
			GitURL:      "https://github.com/jamelt/gastown.git",
			UpstreamURL: "https://github.com/steveyegge/gastown.git",
		}, true
	}

	result := NewDoltLineageCheck().Run(&CheckContext{TownRoot: townRoot, RigName: "testrig"})
	if result.Status != StatusError {
		t.Fatalf("status = %v, want error: %#v", result.Status, result)
	}
	if !strings.Contains(result.Message, "does not match the rig's git remote") {
		t.Fatalf("unexpected message: %s", result.Message)
	}
	details := strings.Join(result.Details, "\n")
	if !strings.Contains(details, "read-only upstream") {
		t.Fatalf("details missing upstream callout: %s", details)
	}
}

func TestDoltLineageCheckAllowsRemoteMatchingGitIntent(t *testing.T) {
	townRoot := t.TempDir()
	beadsDir := filepath.Join(townRoot, "testrig", ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "config.yaml"), []byte("sync.remote: file:///remote\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(`{"dolt_database":"rigdb"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	originalInspect := inspectDoltLineageFn
	originalLoadRig := loadRigEntryFn
	t.Cleanup(func() {
		inspectDoltLineageFn = originalInspect
		loadRigEntryFn = originalLoadRig
	})
	inspectDoltLineageFn = func(_, _ string) (doltserver.LineageReport, error) {
		return doltserver.LineageReport{
			Database: "rigdb", State: doltserver.LineageShared,
			RemoteName: "origin", RemoteURL: "https://github.com/jamelt/gastown.git",
		}, nil
	}
	loadRigEntryFn = func(_, rigName string) (config.RigEntry, bool) {
		return config.RigEntry{
			GitURL:      "https://github.com/jamelt/gastown.git",
			UpstreamURL: "https://github.com/steveyegge/gastown.git",
		}, true
	}

	result := NewDoltLineageCheck().Run(&CheckContext{TownRoot: townRoot, RigName: "testrig"})
	if result.Status != StatusOK {
		t.Fatalf("status = %v, want OK: %#v", result.Status, result)
	}
}
