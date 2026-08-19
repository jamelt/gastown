package rig

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEnsureRigIdentitiesPinnedAndIdempotent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses POSIX fake bd")
	}
	townRoot := t.TempDir()
	rigPath := filepath.Join(townRoot, "gastown")
	rigBeads := filepath.Join(rigPath, ".beads")
	for _, dir := range []string{filepath.Join(townRoot, "mayor"), filepath.Join(townRoot, ".beads"), rigBeads} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(townRoot, "mayor", "town.json"), []byte(`{"name":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(townRoot, ".beads", "routes.jsonl"), []byte(`{"prefix":"gt-","path":"gastown"}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigBeads, "config.yaml"), []byte("prefix: gt\nissue-prefix: gt\n"), 0644); err != nil {
		t.Fatal(err)
	}

	stateDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "bd.log")
	script := `#!/bin/sh
set -eu
printf 'beads=%s args=%s\n' "${BEADS_DIR:-}" "$*" >> "$BD_LOG"
if [ "$1" = "--allow-stale" ]; then shift; fi
cmd="$1"; shift
case "$cmd" in
  version|config) exit 0 ;;
  show)
    id="$1"
    if [ -f "$BD_STATE/$id" ]; then printf '['; cat "$BD_STATE/$id"; printf ']\n'; else echo '[]'; fi
    ;;
  create)
    id=''; title=''
    for arg in "$@"; do
      case "$arg" in --id=*) id="${arg#--id=}" ;; --title=*) title="${arg#--title=}" ;; esac
    done
    [ ! -f "$BD_STATE/$id" ] || exit 1
    labels='["gt:rig"]'; description='Rig identity'
    case "$id" in
      *-witness) labels='["gt:agent"]'; description='role_type: witness\nrig: gastown\nagent_state: idle' ;;
      *-refinery) labels='["gt:agent"]'; description='role_type: refinery\nrig: gastown\nagent_state: idle' ;;
    esac
    printf '{"id":"%s","title":"%s","description":"%s","issue_type":"task","status":"open","labels":%s}' "$id" "$title" "$description" "$labels" | tee "$BD_STATE/$id"
    ;;
  update|reopen) exit 0 ;;
  *) exit 0 ;;
esac
`
	binDir := writeFakeBD(t, script, "")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BD_STATE", stateDir)
	t.Setenv("BD_LOG", logPath)

	if err := EnsureRigIdentities(rigPath, "gastown", "gt", "https://example.test/gastown"); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	// Startup verification must not reset live singleton lifecycle fields.
	witnessState := `{"id":"gt-gastown-witness","title":"Witness","description":"role_type: witness\nrig: gastown\nagent_state: working\nactive_mr: gt-mr-1","issue_type":"task","status":"open","labels":["gt:agent"]}`
	if err := os.WriteFile(filepath.Join(stateDir, "gt-gastown-witness"), []byte(witnessState), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureRigIdentities(rigPath, "gastown", "gt", "https://example.test/gastown"); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(stateDir, "gt-gastown-witness")); err != nil || string(got) != witnessState {
		t.Fatalf("idempotent verification rewrote live lifecycle state: %q, err=%v", got, err)
	}
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("durable identity count = %d, want 3", len(entries))
	}
	wantIDs := map[string]bool{"gt-rig-gastown": false, "gt-gastown-witness": false, "gt-gastown-refinery": false}
	for _, entry := range entries {
		if _, ok := wantIDs[entry.Name()]; !ok {
			t.Fatalf("unexpected identity %s", entry.Name())
		}
		wantIDs[entry.Name()] = true
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(logBytes)), "\n") {
		if strings.Contains(line, "args=create") && !strings.Contains(line, "beads="+rigBeads+" ") {
			t.Fatalf("identity create escaped rig database: %s", line)
		}
	}
}
