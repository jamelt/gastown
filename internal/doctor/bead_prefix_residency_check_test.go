package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

// mockResidencyIssueLister returns canned issues by working directory.
type mockResidencyIssueLister struct {
	issues map[string][]*beads.Issue // workDir -> issues
}

func (m *mockResidencyIssueLister) ListIssues(workDir string) ([]*beads.Issue, error) {
	if issues, ok := m.issues[workDir]; ok {
		return issues, nil
	}
	return nil, fmt.Errorf("no issues configured for %s", workDir)
}

func TestNewBeadPrefixResidencyCheck(t *testing.T) {
	check := NewBeadPrefixResidencyCheck()

	if check.Name() != "bead-prefix-residency" {
		t.Errorf("expected name 'bead-prefix-residency', got %q", check.Name())
	}
	if check.CanFix() {
		t.Error("expected CanFix to return false (read-only check)")
	}
}

func TestBeadPrefixResidencyCheck_NoRoutes(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	check := NewBeadPrefixResidencyCheck()
	result := check.Run(&CheckContext{TownRoot: tmpDir})

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK for no routes, got %v", result.Status)
	}
}

// setupRoutes writes routes.jsonl and creates the (empty) beads directories
// each route resolves to, so the check doesn't skip them as "not checked out".
func setupRoutes(t *testing.T, tmpDir string, routes []beads.Route) {
	t.Helper()
	townBeads := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(townBeads, 0755); err != nil {
		t.Fatal(err)
	}

	var content string
	for _, r := range routes {
		content += fmt.Sprintf(`{"prefix":%q,"path":%q}`+"\n", r.Prefix, r.Path)

		workDir := tmpDir
		if r.Path != "." {
			workDir = filepath.Join(tmpDir, r.Path)
		}
		if err := os.MkdirAll(filepath.Join(workDir, ".beads"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(townBeads, "routes.jsonl"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestBeadPrefixResidencyCheck_AllMatching(t *testing.T) {
	tmpDir := t.TempDir()
	setupRoutes(t, tmpDir, []beads.Route{
		{Prefix: "hq-", Path: "."},
		{Prefix: "gt-", Path: "gastown/mayor/rig"},
	})

	check := NewBeadPrefixResidencyCheck()
	check.lister = &mockResidencyIssueLister{
		issues: map[string][]*beads.Issue{
			tmpDir: {{ID: "hq-abc1"}, {ID: "hq-abc2"}},
			filepath.Join(tmpDir, "gastown/mayor/rig"): {{ID: "gt-xyz1"}},
		},
	}

	result := check.Run(&CheckContext{TownRoot: tmpDir})
	if result.Status != StatusOK {
		t.Errorf("expected StatusOK, got %v: %s %v", result.Status, result.Message, result.Details)
	}
}

func TestBeadPrefixResidencyCheck_MisfiledBead(t *testing.T) {
	tmpDir := t.TempDir()
	setupRoutes(t, tmpDir, []beads.Route{
		{Prefix: "hq-", Path: "."},
		{Prefix: "gt-", Path: "gastown/mayor/rig"},
	})

	check := NewBeadPrefixResidencyCheck()
	check.lister = &mockResidencyIssueLister{
		issues: map[string][]*beads.Issue{
			// hq- bead misfiled into the gastown rig database.
			tmpDir: {{ID: "hq-abc1"}},
			filepath.Join(tmpDir, "gastown/mayor/rig"): {{ID: "gt-xyz1"}, {ID: "hq-26y"}},
		},
	}

	result := check.Run(&CheckContext{TownRoot: tmpDir})
	if result.Status != StatusWarning {
		t.Fatalf("expected StatusWarning, got %v: %s", result.Status, result.Message)
	}
	if len(result.Details) != 1 {
		t.Fatalf("expected 1 detail, got %d: %v", len(result.Details), result.Details)
	}
	want := "hq-26y stored in gastown/mayor/rig but routes to town root"
	if result.Details[0] != want {
		t.Errorf("expected detail %q, got %q", want, result.Details[0])
	}
}

func TestBeadPrefixResidencyCheck_UnknownPrefixIgnored(t *testing.T) {
	tmpDir := t.TempDir()
	setupRoutes(t, tmpDir, []beads.Route{
		{Prefix: "hq-", Path: "."},
	})

	check := NewBeadPrefixResidencyCheck()
	check.lister = &mockResidencyIssueLister{
		issues: map[string][]*beads.Issue{
			// "legacy-" has no route at all; not this check's concern.
			tmpDir: {{ID: "hq-abc1"}, {ID: "legacy-old1"}},
		},
	}

	result := check.Run(&CheckContext{TownRoot: tmpDir})
	if result.Status != StatusOK {
		t.Errorf("expected StatusOK (unrouted prefix ignored), got %v: %s %v", result.Status, result.Message, result.Details)
	}
}

func TestBeadPrefixResidencyCheck_SharedRedirectedDBNotFlagged(t *testing.T) {
	tmpDir := t.TempDir()
	// site_manager redirects to the town root's database, so beads under
	// either prefix in that single physical DB are legitimate.
	setupRoutes(t, tmpDir, []beads.Route{
		{Prefix: "hq-", Path: "."},
		{Prefix: "sm-", Path: "site_manager"},
	})
	if err := os.WriteFile(filepath.Join(tmpDir, "site_manager", ".beads", "redirect"), []byte("../.beads\n"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewBeadPrefixResidencyCheck()
	check.lister = &mockResidencyIssueLister{
		issues: map[string][]*beads.Issue{
			// Both workDirs resolve to the same physical DB, but the lister
			// is keyed per representative workDir picked by the check
			// (first route seen for that canonical dir) — only town root's
			// entry is consulted.
			tmpDir: {{ID: "hq-abc1"}, {ID: "sm-def2"}},
		},
	}

	result := check.Run(&CheckContext{TownRoot: tmpDir})
	if result.Status != StatusOK {
		t.Errorf("expected StatusOK (shared redirected DB), got %v: %s %v", result.Status, result.Message, result.Details)
	}
}
