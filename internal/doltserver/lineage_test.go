package doltserver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInspectLineageDetectsNoCommonAncestor(t *testing.T) {
	queries := func(query string) (string, error) {
		switch {
		case strings.Contains(query, "FROM dolt_branches"):
			return "hash\nlocal-head\n", nil
		case strings.Contains(query, "FROM dolt_remote_branches"):
			return "hash\nremote-head\n", nil
		case strings.Contains(query, "DOLT_MERGE_BASE"):
			return "", fmt.Errorf("Error 1105: no common ancestor")
		case strings.HasPrefix(query, "SELECT COUNT(*) FROM dolt_log('main')"):
			return "COUNT(*)\n3\n", nil
		case strings.HasPrefix(query, "SELECT COUNT(*) FROM dolt_log('remotes/origin/main')"):
			return "COUNT(*)\n5\n", nil
		case strings.HasPrefix(query, "SELECT COUNT(*) FROM issues AS OF 'main'"):
			return "COUNT(*)\n2\n", nil
		case strings.HasPrefix(query, "SELECT COUNT(*) FROM issues AS OF 'remotes/origin/main'"):
			return "COUNT(*)\n4\n", nil
		default:
			return "", fmt.Errorf("unexpected query: %s", query)
		}
	}

	report, err := inspectLineage("gastown", "origin", "file:///remote", queries)
	if err != nil {
		t.Fatalf("inspectLineage: %v", err)
	}
	if report.State != LineageDiverged {
		t.Fatalf("state = %q, want %q", report.State, LineageDiverged)
	}
	if report.LocalHead != "local-head" || report.RemoteHead != "remote-head" {
		t.Fatalf("unexpected heads: %#v", report)
	}
	if report.LocalOnlyCommits != 3 || report.RemoteOnlyCommits != 5 || report.LocalOnlyRecords != 2 || report.RemoteOnlyRecords != 4 {
		t.Fatalf("unexpected unique counts: %#v", report)
	}
	if report.SafeToPush() || report.Shared() {
		t.Fatal("diverged history must fail closed")
	}
}

func TestInspectLineageSharedHistory(t *testing.T) {
	queries := func(query string) (string, error) {
		switch {
		case strings.Contains(query, "FROM dolt_branches"):
			return "hash\nlocal-head\n", nil
		case strings.Contains(query, "FROM dolt_remote_branches"):
			return "hash\nremote-head\n", nil
		case strings.Contains(query, "DOLT_MERGE_BASE"):
			return "merge_base\nshared-base\n", nil
		default:
			return "COUNT(*)\n0\n", nil
		}
	}

	report, err := inspectLineage("gastown", "origin", "file:///remote", queries)
	if err != nil {
		t.Fatalf("inspectLineage: %v", err)
	}
	if report.State != LineageShared || !report.SafeToPush() || !report.Shared() {
		t.Fatalf("unexpected shared report: %#v", report)
	}
}

func TestInspectLineageUnverifiedRemote(t *testing.T) {
	queries := func(query string) (string, error) {
		if strings.Contains(query, "FROM dolt_branches") {
			return "hash\nlocal-head\n", nil
		}
		return "hash\n", nil
	}
	report, err := inspectLineage("gastown", "origin", "file:///remote", queries)
	if err != nil {
		t.Fatalf("inspectLineage: %v", err)
	}
	if report.State != LineageRemoteUnverified || report.Shared() || report.SafeToPush() {
		t.Fatalf("unexpected unverified report: %#v", report)
	}
}

func TestInspectLineageCLIIndependentHistories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file remote fixture uses POSIX paths")
	}
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not installed")
	}
	root := t.TempDir()
	seedDir := filepath.Join(root, "seed")
	remoteStore := filepath.Join(root, "remote-store")
	localDir := filepath.Join(t.TempDir(), "local")
	for _, dir := range []string{seedDir, localDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := initDoltDB(dir); err != nil {
			t.Fatalf("init %s: %v", dir, err)
		}
	}
	runDoltLineageTest(t, seedDir, "sql", "-q", "CREATE TABLE remote_only (id INT PRIMARY KEY)")
	runDoltLineageTest(t, seedDir, "add", ".")
	runDoltLineageTest(t, seedDir, "commit", "-m", "remote history")
	runDoltLineageTest(t, seedDir, "remote", "add", "origin", "file://"+remoteStore)
	runDoltLineageTest(t, seedDir, "push", "-u", "origin", "main")
	runDoltLineageTest(t, localDir, "sql", "-q", "CREATE TABLE local_only (id INT PRIMARY KEY)")
	runDoltLineageTest(t, localDir, "add", ".")
	runDoltLineageTest(t, localDir, "commit", "-m", "local history")
	runDoltLineageTest(t, localDir, "remote", "add", "origin", "file://"+remoteStore)
	runDoltLineageTest(t, localDir, "fetch", "origin", "main")

	report, err := InspectLineageCLI(localDir, "fixture")
	if err != nil {
		t.Fatalf("InspectLineageCLI: %v", err)
	}
	if report.State != LineageDiverged || report.LocalHead == "" || report.RemoteHead == "" {
		t.Fatalf("expected independent histories, got %#v", report)
	}
}

func TestRemoteBootstrapSharesLineageAndPushes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file remote fixture uses POSIX paths")
	}
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not installed")
	}
	root := t.TempDir()
	seedDir := filepath.Join(root, "seed")
	remoteStore := filepath.Join(root, "remote-store")
	localDir := filepath.Join(root, "local")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := initDoltDB(seedDir); err != nil {
		t.Fatal(err)
	}
	runDoltLineageTest(t, seedDir, "sql", "-q", "CREATE TABLE issues (id VARCHAR(255) PRIMARY KEY)")
	runDoltLineageTest(t, seedDir, "add", ".")
	runDoltLineageTest(t, seedDir, "commit", "-m", "remote baseline")
	runDoltLineageTest(t, seedDir, "remote", "add", "origin", "file://"+remoteStore)
	runDoltLineageTest(t, seedDir, "push", "-u", "origin", "main")
	runDoltLineageTest(t, root, "clone", "file://"+remoteStore, localDir)
	runDoltLineageTest(t, localDir, "sql", "-q", "INSERT INTO issues VALUES ('local-new')")
	runDoltLineageTest(t, localDir, "add", ".")
	runDoltLineageTest(t, localDir, "commit", "-m", "local change")

	report, err := InspectLineageCLI(localDir, "fixture")
	if err != nil {
		t.Fatalf("InspectLineageCLI: %v", err)
	}
	if !report.Shared() || report.MergeBase == "" {
		t.Fatalf("cloned database must share lineage: %#v", report)
	}
	runDoltLineageTest(t, localDir, "push", "origin", "main")
}

func runDoltLineageTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("dolt", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dolt %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
