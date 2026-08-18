package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

func TestContainsString(t *testing.T) {
	if !containsString([]string{"gt", "hq"}, "hq") {
		t.Error("expected containsString to find \"hq\"")
	}
	if containsString([]string{"gt", "hq"}, "gti") {
		t.Error("expected containsString to not find \"gti\"")
	}
	if containsString(nil, "gt") {
		t.Error("expected containsString(nil, ...) to be false")
	}
}

func TestGroupRoutesByDatabase(t *testing.T) {
	tmpDir := t.TempDir()

	routes := []beads.Route{
		{Prefix: "hq-", Path: "."},
		{Prefix: "hq-cv-", Path: "."}, // Same physical DB as "hq-" above.
		{Prefix: "gt-", Path: "rigA/mayor/rig"},
	}

	groups, order := groupRoutesByDatabase(tmpDir, routes)

	if len(order) != 2 {
		t.Fatalf("expected 2 distinct databases, got %d: %v", len(order), order)
	}

	townBeadsDir, _ := filepath.Abs(beads.ResolveBeadsDir(tmpDir))
	townGroup, ok := groups[townBeadsDir]
	if !ok {
		t.Fatalf("expected a group for town root %s, got keys %v", townBeadsDir, order)
	}
	if len(townGroup.prefixes) != 2 || !containsString(townGroup.prefixes, "hq") || !containsString(townGroup.prefixes, "hq-cv") {
		t.Errorf("expected town group prefixes [hq hq-cv], got %v", townGroup.prefixes)
	}

	rigBeadsDir, _ := filepath.Abs(beads.ResolveBeadsDir(filepath.Join(tmpDir, "rigA/mayor/rig")))
	rigGroup, ok := groups[rigBeadsDir]
	if !ok {
		t.Fatalf("expected a group for rigA, got keys %v", order)
	}
	if len(rigGroup.prefixes) != 1 || rigGroup.prefixes[0] != "gt" {
		t.Errorf("expected rigA group prefixes [gt], got %v", rigGroup.prefixes)
	}
}

func TestMisroutedBeadIDCheck_NoRoutes(t *testing.T) {
	tmpDir := t.TempDir()

	check := NewMisroutedBeadIDCheck()
	result := check.Run(&CheckContext{TownRoot: tmpDir})

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK with no routes.jsonl, got %v: %s", result.Status, result.Message)
	}
}

func TestMisroutedBeadIDCheck_DetectsMisfiledRow(t *testing.T) {
	tmpDir := t.TempDir()

	// Town-root database.
	if err := os.MkdirAll(filepath.Join(tmpDir, ".beads"), 0755); err != nil {
		t.Fatal(err)
	}
	routesContent := `{"prefix":"hq-","path":"."}
{"prefix":"gt-","path":"rigA/mayor/rig"}
`
	if err := os.WriteFile(filepath.Join(tmpDir, ".beads", "routes.jsonl"), []byte(routesContent), 0644); err != nil {
		t.Fatal(err)
	}

	// rigA's own database.
	rigBeadsDir := filepath.Join(tmpDir, "rigA", "mayor", "rig", ".beads")
	if err := os.MkdirAll(rigBeadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Fake bd: "bd sql --csv <query>" returns rows keyed by which database
	// (cwd) it's invoked against. rigA's gt- database has a misfiled hq-999
	// row alongside a properly-prefixed gt-1 row; the town DB is clean.
	binDir := t.TempDir()
	bdScript := filepath.Join(binDir, "bd")
	script := fmt.Sprintf(`#!/bin/sh
case "$PWD" in
  %s)
    printf 'id,title\nhq-999,Misfiled town bead\ngt-1,Proper gt bead\n'
    ;;
  *)
    printf 'id,title\nhq-1,Town bead\n'
    ;;
esac
`, filepath.Join(tmpDir, "rigA", "mayor", "rig"))
	if err := os.WriteFile(bdScript, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fmt.Sprintf("%s%c%s", binDir, os.PathListSeparator, os.Getenv("PATH")))

	check := NewMisroutedBeadIDCheck()
	result := check.Run(&CheckContext{TownRoot: tmpDir})

	if result.Status != StatusWarning {
		t.Fatalf("expected StatusWarning, got %v: %s (details: %v)", result.Status, result.Message, result.Details)
	}
	if len(check.misrouted) != 1 {
		t.Fatalf("expected exactly 1 misrouted bead, got %d: %+v", len(check.misrouted), check.misrouted)
	}
	if check.misrouted[0].ID != "hq-999" {
		t.Errorf("expected misrouted bead hq-999, got %s", check.misrouted[0].ID)
	}
	if !strings.Contains(result.Details[0], "hq-999") {
		t.Errorf("expected details to mention hq-999, got %v", result.Details)
	}
	if check.CanFix() {
		t.Error("expected CanFix() to be false — this check is report-only")
	}
}

func TestQueryAllBeadIDs_ParsesCSV(t *testing.T) {
	binDir := t.TempDir()
	bdScript := filepath.Join(binDir, "bd")
	script := "#!/bin/sh\nprintf 'id,title\\ngt-1,First\\ngt-2,Second\\n'\n"
	if err := os.WriteFile(bdScript, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fmt.Sprintf("%s%c%s", binDir, os.PathListSeparator, os.Getenv("PATH")))

	rows, err := queryAllBeadIDs(t.TempDir())
	if err != nil {
		t.Fatalf("queryAllBeadIDs returned error: %v", err)
	}
	if len(rows) != 2 || rows[0].ID != "gt-1" || rows[1].ID != "gt-2" {
		t.Errorf("unexpected rows: %+v", rows)
	}
}
