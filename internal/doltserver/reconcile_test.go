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
