package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMatchesGoTest(t *testing.T) {
	cases := []struct {
		command string
		want    bool
	}{
		// Real `go test` invocations that must be recognized.
		{"go test ./...", true},
		{"go test -run TestFoo ./internal/cmd/", true},
		{"CGO_ENABLED=0 go test ./...", true},
		{"cd internal/cmd && go test .", true},
		{"/usr/local/go/bin/go test ./...", true},
		{"go build ./... && go test ./...", true},
		// Transparent command wrappers still run go test.
		{"time go test ./...", true},
		{"env FOO=1 go test ./...", true},
		{"command -p go test ./...", true},
		{"nohup go test ./...", true},
		{"go -C internal/cmd test", true}, // -C takes a dir value, then `test`
		// matchesGoTest is deliberately liberal (only ever gates a fail-open
		// block), so it accepts multi-space; note that the production hook
		// matcher Bash(*go test*) requires a single space, so this exact form
		// would not reach the guard in production — covered here for the helper.
		{"go   test    ./...", true},
		// Not a `go test` command — must pass through.
		{"go build ./...", false},
		{"go vet ./...", false},
		{"go run ./cmd/gt", false},
		{"echo go test", false},    // go is an arg of echo, not the command
		{"cargo test", false},      // different tool
		{"gotestsum ./...", false}, // base name is not "go"
		{"git commit -m 'go test'", false},
		// Documented residuals: indirection matchesGoTest cannot see through.
		{"make test", false},                // shells out to go test, not visible
		{`bash -c "go test ./..."`, false},  // command word is bash, not go
		{"nice -n 10 go test ./...", false}, // valued-arg wrapper (residual)
		{"", false},
	}
	for _, tc := range cases {
		if got := matchesGoTest(tc.command); got != tc.want {
			t.Errorf("matchesGoTest(%q) = %v, want %v", tc.command, got, tc.want)
		}
	}
}

func TestDirIsStaleGastown(t *testing.T) {
	writeGoMod := func(dir, module string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"),
			[]byte("module "+module+"\n\ngo 1.24\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	addIsolationSource := func(dir string) {
		t.Helper()
		guardDir := filepath.Join(dir, "internal", "testguard")
		if err := os.MkdirAll(guardDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(guardDir, "testguard.go"),
			[]byte("package testguard\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("stale gastown worktree (no testguard) is stale", func(t *testing.T) {
		dir := t.TempDir()
		writeGoMod(dir, gastownModulePath)
		if !dirIsStaleGastown(dir) {
			t.Error("expected stale=true for gastown worktree lacking internal/testguard")
		}
	})

	t.Run("current gastown worktree (has testguard) is not stale", func(t *testing.T) {
		dir := t.TempDir()
		writeGoMod(dir, gastownModulePath)
		addIsolationSource(dir)
		if dirIsStaleGastown(dir) {
			t.Error("expected stale=false for gastown worktree containing internal/testguard")
		}
	})

	t.Run("stale detected from a nested subdirectory", func(t *testing.T) {
		dir := t.TempDir()
		writeGoMod(dir, gastownModulePath)
		sub := filepath.Join(dir, "internal", "cmd")
		if err := os.MkdirAll(sub, 0755); err != nil {
			t.Fatal(err)
		}
		if !dirIsStaleGastown(sub) {
			t.Error("expected stale=true when walking up from a subdirectory")
		}
	})

	t.Run("non-gastown module is never our concern", func(t *testing.T) {
		dir := t.TempDir()
		writeGoMod(dir, "github.com/example/other")
		if dirIsStaleGastown(dir) {
			t.Error("expected stale=false for a non-gastown module")
		}
	})

	t.Run("no go.mod resolves to allow", func(t *testing.T) {
		dir := t.TempDir()
		if dirIsStaleGastown(dir) {
			t.Error("expected stale=false when no go.mod is found")
		}
	})
}

func TestTestIsolationShouldBlock(t *testing.T) {
	// A live-looking (non-temp) town root, as a stale pane would carry.
	const liveRoot = "/home/user/gt"

	staleDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staleDir, "go.mod"),
		[]byte("module "+gastownModulePath+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	currentDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(currentDir, "go.mod"),
		[]byte("module "+gastownModulePath+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	guardDir := filepath.Join(currentDir, "internal", "testguard")
	if err := os.MkdirAll(guardDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(guardDir, "testguard.go"), []byte("package testguard\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name     string
		command  string
		liveRoot string
		cwd      string
		want     bool
	}{
		{"blocks stale go test against live town", "go test ./...", liveRoot, staleDir, true},
		{"allows current worktree go test", "go test ./...", liveRoot, currentDir, false},
		{"allows non-test go command", "go build ./...", liveRoot, staleDir, false},
		{"allows when town root is empty", "go test ./...", "", staleDir, false},
		// An isolated sandbox town (a temp dir) has nothing live to protect.
		{"allows when town is an isolated sandbox", "go test ./...", staleDir, staleDir, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := testIsolationShouldBlock(tc.command, tc.liveRoot, tc.cwd); got != tc.want {
				t.Errorf("testIsolationShouldBlock(%q, %q, cwd) = %v, want %v",
					tc.command, tc.liveRoot, got, tc.want)
			}
		})
	}
}
