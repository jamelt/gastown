package beads

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReconcileRigContainerRedirectPreservesAuthoritativeManagerDatabase(t *testing.T) {
	rigPath := filepath.Join(t.TempDir(), "gastown")
	canonical := filepath.Join(rigPath, "mayor", "rig", ".beads")
	container := filepath.Join(rigPath, ".beads")
	if err := os.MkdirAll(canonical, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(container, 0755); err != nil {
		t.Fatal(err)
	}
	authoritative := []byte(`{"dolt_mode":"local","project_id":"authoritative"}`)
	if err := os.WriteFile(filepath.Join(canonical, "metadata.json"), authoritative, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonical, "sentinel"), []byte("records"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(container, "metadata.json"), []byte(`{"dolt_mode":"server","dolt_database":"gastown","project_id":"stale"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(container, "config.yaml"), []byte("prefix: wrong\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ReconcileRigContainerRedirect(rigPath); err != nil {
		t.Fatalf("ReconcileRigContainerRedirect: %v", err)
	}
	if got := ResolveBeadsDir(rigPath); filepath.Clean(got) != filepath.Clean(canonical) {
		t.Fatalf("resolved beads = %s, want %s", got, canonical)
	}
	for _, stale := range []string{"metadata.json", "config.yaml"} {
		if _, err := os.Stat(filepath.Join(container, stale)); !os.IsNotExist(err) {
			t.Fatalf("container %s survived reconciliation, stat err=%v", stale, err)
		}
	}
	if got, err := os.ReadFile(filepath.Join(canonical, "metadata.json")); err != nil || string(got) != string(authoritative) {
		t.Fatalf("authoritative metadata changed: %q, err=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(canonical, "sentinel")); err != nil || string(got) != "records" {
		t.Fatalf("authoritative records changed: %q, err=%v", got, err)
	}
}
