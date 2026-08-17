package doltserver

import (
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
