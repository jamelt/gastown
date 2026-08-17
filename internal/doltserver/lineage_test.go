package doltserver

import (
	"fmt"
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
		case strings.Contains(query, "dolt_log('main')"):
			return "COUNT(*)\n3\n", nil
		case strings.Contains(query, "dolt_log('remotes/origin/main')"):
			return "COUNT(*)\n5\n", nil
		case strings.Contains(query, "issues AS OF 'main'"):
			return "COUNT(*)\n2\n", nil
		case strings.Contains(query, "issues AS OF 'remotes/origin/main'"):
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
	if report.State != LineageRemoteUnverified || report.Shared() == true || !report.SafeToPush() {
		t.Fatalf("unexpected unverified report: %#v", report)
	}
}
