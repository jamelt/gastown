package doltserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateReconciliationBundleRequiresExplicitAuthorityAndApproval(t *testing.T) {
	report := LineageReport{
		Database: "gastown", State: LineageDiverged,
		LocalHead: "0123456789abcdef", RemoteHead: "fedcba9876543210",
		RemoteName: "origin",
	}
	if got, want := ReconciliationApprovalToken(report), "RECONCILE-gastown-0123456789ab-fedcba987654"; got != want {
		t.Fatalf("approval token = %q, want %q", got, want)
	}
	if _, err := CreateReconciliationBundle(t.TempDir(), report, "", "", ""); err == nil || !strings.Contains(err.Error(), "authoritative") {
		t.Fatalf("missing authority error = %v", err)
	}
	if _, err := CreateReconciliationBundle(t.TempDir(), report, "remote", "wrong", ""); err == nil || !strings.Contains(err.Error(), "approval required") {
		t.Fatalf("missing approval error = %v", err)
	}
}

func TestCreateReconciliationBundleExportsBothHistoriesAndReceipt(t *testing.T) {
	report := LineageReport{
		Database: "gastown", State: LineageDiverged,
		LocalHead: "0123456789abcdef", RemoteHead: "fedcba9876543210",
		RemoteName: "origin", LocalOnlyRecords: 3, RemoteOnlyRecords: 5,
	}
	originalCSV, originalJSON := reconciliationQueryCSV, reconciliationQueryJSON
	t.Cleanup(func() {
		reconciliationQueryCSV = originalCSV
		reconciliationQueryJSON = originalJSON
	})
	reconciliationQueryCSV = func(_, query string) (string, error) {
		if !strings.Contains(query, "SHOW TABLES AS OF") {
			t.Fatalf("non-read-only CSV query: %s", query)
		}
		return "Tables_in_gastown\ncomments\nissues\n", nil
	}
	reconciliationQueryJSON = func(_, query string) (string, error) {
		upper := strings.ToUpper(query)
		for _, forbidden := range []string{"INSERT ", "UPDATE ", "DELETE ", "DROP ", "DOLT_FETCH", "DOLT_PUSH", "DOLT_MERGE"} {
			if strings.Contains(upper, forbidden) {
				t.Fatalf("mutating reconciliation query: %s", query)
			}
		}
		return `{"rows":[]}`, nil
	}

	output := filepath.Join(t.TempDir(), "bundle")
	got, err := CreateReconciliationBundle(t.TempDir(), report, "remote", ReconciliationApprovalToken(report), output)
	if err != nil {
		t.Fatalf("CreateReconciliationBundle: %v", err)
	}
	if got != output {
		t.Fatalf("bundle path = %q, want %q", got, output)
	}
	for _, relative := range []string{
		"local/comments.json", "local/issues.json", "local/dolt_log.json",
		"remote/comments.json", "remote/issues.json", "remote/dolt_log.json",
	} {
		if _, err := os.Stat(filepath.Join(output, relative)); err != nil {
			t.Errorf("missing preserved file %s: %v", relative, err)
		}
	}
	receiptData, err := os.ReadFile(filepath.Join(output, "receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	var receipt ReconciliationReceipt
	if err := json.Unmarshal(receiptData, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Authority != "remote" || len(receipt.Files) != 6 {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}
	if !strings.Contains(receipt.Mutation, "no fetch") || !strings.Contains(receipt.NextStep, "Never force-push") {
		t.Fatalf("receipt lacks safety audit: %#v", receipt)
	}
}

func writeIssuesJSON(t *testing.T, dir, label string, ids []string) {
	t.Helper()
	rows := make([]map[string]string, len(ids))
	for i, id := range ids {
		rows[i] = map[string]string{"id": id}
	}
	data, err := json.Marshal(map[string]any{"rows": rows})
	if err != nil {
		t.Fatal(err)
	}
	labelDir := filepath.Join(dir, label)
	if err := os.MkdirAll(labelDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(labelDir, "issues.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyReconciliationImportDetectsMissingIssues(t *testing.T) {
	bundle := t.TempDir()
	writeIssuesJSON(t, bundle, "local", []string{"gt-1", "gt-2", "gt-3"})
	writeIssuesJSON(t, bundle, "remote", []string{"gt-3", "gt-4"})

	originalJSON := reconciliationQueryJSON
	t.Cleanup(func() { reconciliationQueryJSON = originalJSON })
	reconciliationQueryJSON = func(_, query string) (string, error) {
		if !strings.Contains(query, "SELECT id FROM issues") {
			t.Fatalf("unexpected query: %s", query)
		}
		// gt-2 was dropped during reconstruction.
		return `{"rows":[{"id":"gt-1"},{"id":"gt-3"},{"id":"gt-4"}]}`, nil
	}

	report, err := VerifyReconciliationImport(t.TempDir(), bundle, "gastown")
	if err != nil {
		t.Fatalf("VerifyReconciliationImport: %v", err)
	}
	if report.OK() {
		t.Fatalf("expected a detected loss, got OK report: %#v", report)
	}
	if report.ExpectedCount != 4 || report.CurrentCount != 3 {
		t.Fatalf("unexpected counts: %#v", report)
	}
	if len(report.MissingIDs) != 1 || report.MissingIDs[0] != "gt-2" {
		t.Fatalf("missing IDs = %v, want [gt-2]", report.MissingIDs)
	}
}

func TestVerifyReconciliationImportOKWhenNothingLost(t *testing.T) {
	bundle := t.TempDir()
	writeIssuesJSON(t, bundle, "local", []string{"gt-1", "gt-2"})
	writeIssuesJSON(t, bundle, "remote", []string{"gt-2", "gt-3"})

	originalJSON := reconciliationQueryJSON
	t.Cleanup(func() { reconciliationQueryJSON = originalJSON })
	reconciliationQueryJSON = func(_, query string) (string, error) {
		return `{"rows":[{"id":"gt-1"},{"id":"gt-2"},{"id":"gt-3"},{"id":"gt-5"}]}`, nil
	}

	report, err := VerifyReconciliationImport(t.TempDir(), bundle, "gastown")
	if err != nil {
		t.Fatalf("VerifyReconciliationImport: %v", err)
	}
	if !report.OK() {
		t.Fatalf("expected OK report, got missing IDs: %v", report.MissingIDs)
	}
	if report.ExpectedCount != 3 || report.CurrentCount != 4 {
		t.Fatalf("unexpected counts: %#v", report)
	}
}

func TestVerifyReconciliationImportRejectsEmptyBundle(t *testing.T) {
	if _, err := VerifyReconciliationImport(t.TempDir(), t.TempDir(), "gastown"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error for missing bundle contents, got %v", err)
	}
}

func TestVerifyReconciliationImportRejectsPartialBundle(t *testing.T) {
	bundle := t.TempDir()
	writeIssuesJSON(t, bundle, "local", []string{"gt-1"})
	// "remote/issues.json" is deliberately absent, simulating a bundle whose
	// export failed partway through (see exportDoltRevision's "partial
	// preservation bundle retained" error path).
	if _, err := VerifyReconciliationImport(t.TempDir(), bundle, "gastown"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error for a partial bundle, got %v", err)
	}
}

func TestVerifyReconciliationImportRejectsMalformedRow(t *testing.T) {
	bundle := t.TempDir()
	writeIssuesJSON(t, bundle, "local", []string{"gt-1"})
	writeIssuesJSON(t, bundle, "remote", []string{""})
	if _, err := VerifyReconciliationImport(t.TempDir(), bundle, "gastown"); err == nil || !strings.Contains(err.Error(), "empty or missing id") {
		t.Fatalf("expected malformed-row error, got %v", err)
	}
}

func TestVerifyReconciliationImportRejectsInvalidDatabaseName(t *testing.T) {
	bundle := t.TempDir()
	writeIssuesJSON(t, bundle, "local", []string{"gt-1"})
	writeIssuesJSON(t, bundle, "remote", []string{"gt-1"})
	if _, err := VerifyReconciliationImport(t.TempDir(), bundle, "gastown`; DROP TABLE issues; --"); err == nil || !strings.Contains(err.Error(), "invalid database name") {
		t.Fatalf("expected invalid-database-name error, got %v", err)
	}
}
