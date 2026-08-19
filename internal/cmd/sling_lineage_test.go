package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/doltserver"
)

func TestVerifyRigDoltLineageRejectsIndependentHistory(t *testing.T) {
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

	original := inspectRigDoltLineageFn
	t.Cleanup(func() { inspectRigDoltLineageFn = original })
	inspectRigDoltLineageFn = func(gotRoot, gotDB string) (doltserver.LineageReport, error) {
		if gotRoot != townRoot || gotDB != "rigdb" {
			t.Fatalf("unexpected inspection target root=%q db=%q", gotRoot, gotDB)
		}
		return doltserver.LineageReport{
			Database: "rigdb", State: doltserver.LineageDiverged,
			LocalHead: "local", RemoteHead: "remote",
			LocalOnlyRecords: 2, RemoteOnlyRecords: 4,
		}, nil
	}

	err := verifyRigDoltLineage(townRoot, "testrig")
	if err == nil {
		t.Fatal("expected independent histories to block dispatch")
	}
	for _, want := range []string{"refusing dispatch", "local", "remote", "local-only", "remote-only", "gt dolt reconcile --db rigdb"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q: %v", want, err)
		}
	}
}

func TestVerifyRigDoltLineageAllowsUnregisteredRemote(t *testing.T) {
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

	original := inspectRigDoltLineageFn
	t.Cleanup(func() { inspectRigDoltLineageFn = original })
	inspectRigDoltLineageFn = func(_, _ string) (doltserver.LineageReport, error) {
		return doltserver.LineageReport{Database: "rigdb", State: doltserver.LineageNoRemote}, nil
	}

	if err := verifyRigDoltLineage(townRoot, "testrig"); err != nil {
		t.Fatalf("declared-but-unregistered remote should degrade to local-only, not block dispatch: %v", err)
	}
}

func TestVerifyRigDoltLineageAllowsUnverifiedRemote(t *testing.T) {
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

	original := inspectRigDoltLineageFn
	t.Cleanup(func() { inspectRigDoltLineageFn = original })
	inspectRigDoltLineageFn = func(_, _ string) (doltserver.LineageReport, error) {
		return doltserver.LineageReport{Database: "rigdb", State: doltserver.LineageRemoteUnverified}, nil
	}

	err := verifyRigDoltLineage(townRoot, "testrig")
	if err != nil {
		t.Fatalf("unverified remote should be allowed at dispatch gate (gt-a7o5): %v", err)
	}
}

func TestVerifyRigDoltLineageSkipsLocalOnlyRig(t *testing.T) {
	townRoot := t.TempDir()
	beadsDir := filepath.Join(townRoot, "testrig", ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "config.yaml"), []byte("issue-prefix: tr\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	called := false
	original := inspectRigDoltLineageFn
	t.Cleanup(func() { inspectRigDoltLineageFn = original })
	inspectRigDoltLineageFn = func(_, _ string) (doltserver.LineageReport, error) {
		called = true
		return doltserver.LineageReport{}, nil
	}
	if err := verifyRigDoltLineage(townRoot, "testrig"); err != nil {
		t.Fatalf("local-only rig: %v", err)
	}
	if called {
		t.Fatal("local-only rig should not query remote lineage")
	}
}
