package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/testguard"
)

// gastownModulePath is the go.mod module path of this repo. A `go test` compiled
// from a worktree of this module is protected by the internal/testguard guard
// ONLY if that worktree's source actually contains the testguard package (i.e.
// it is at/after gt-8ik). A pre-gt-8ik ("stale") worktree compiles a test binary
// with no such guard, which is exactly the vector gt-x3yy closes.
const gastownModulePath = "github.com/steveyegge/gastown"

// isolationSourceMarker is the path (relative to a module root) whose presence
// proves the worktree carries the compiled-in test isolation guard. Because
// `go test` always recompiles from the worktree source, source presence is a
// causal — not heuristic — signal for whether the resulting binary is protected.
// It points at the internal/testguard package DIRECTORY (not a single file) so
// that an intra-package refactor cannot make every current worktree look stale.
// Keep this in sync with the internal/testguard package location.
var isolationSourceMarker = filepath.Join("internal", "testguard")

var tapGuardTestIsolationCmd = &cobra.Command{
	Use:   "test-isolation",
	Short: "Block `go test` from a stale worktree that would mutate the live control plane",
	Long: `Block ` + "`go test`" + ` launched from a STALE worktree against a LIVE town.

gt-8ik added a compiled, fail-closed isolation guard (internal/testguard) so that
a test binary cannot mutate the live Gas Town control plane (beads DB, tmux
sockets, hooks, capacity). That guard only exists in binaries compiled AFTER
gt-8ik. A polecat worktree checked out BEFORE gt-8ik ("stale") compiles a test
binary with none of it, and its ` + "`go test`" + ` — inheriting the live GT_ROOT the
daemon injects into the pane, and resolving live beads/tmux from its cwd (which is
physically nested inside the live town) — can silently corrupt production. No
in-repo source guard can retroactively protect a pre-fix compiled binary, so this
host-level PreToolUse guard, invoked from the CURRENT gt binary, fires before the
stale ` + "`go test`" + ` runs. See gt-8ik and gt-x3yy.

The guard blocks ONLY when all hold, so it never breaks legitimate testing:
  1. the command is a real ` + "`go test`" + ` invocation, AND
  2. GT_ROOT resolves to a LIVE (non-isolated) town, AND
  3. the worktree is a gastown worktree that LACKS the isolation source
     (i.e. it is stale — its recompiled test binary would be unprotected).

A current worktree (containing internal/testguard) is allowed: its test binary
self-isolates. A non-gastown repo or an already-isolated sandbox is allowed.

Residuals (accepted, out of this guard's reach): a directly-executed pre-compiled
*.test binary and indirection the PreToolUse matcher never sees as "go test"
(make test, a shelled-out script, bash -c '…'); non-gastown rig worktrees; and,
because the live-town signal is read from GT_ROOT/GT_TOWN_ROOT, a run that
deliberately strips both env vars (the daemon always injects them into panes, so
this only matters for an adversarial, not accidental, invocation).

Exit codes:
  0 - Operation allowed
  2 - Operation BLOCKED`,
	RunE: runTapGuardTestIsolation,
}

func init() {
	tapGuardCmd.AddCommand(tapGuardTestIsolationCmd)
}

func runTapGuardTestIsolation(cmd *cobra.Command, args []string) error {
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil // fail open
	}

	command := extractCommand(input)
	cwd, err := os.Getwd()
	if err != nil {
		return nil // fail open
	}
	liveRoot := testguard.LiveTownRoot()
	if !testIsolationShouldBlock(command, liveRoot, cwd) {
		return nil
	}

	printTestIsolationBlock(command, liveRoot)
	return NewSilentExit(2)
}

// testIsolationShouldBlock is the pure decision: block iff the command is a real
// `go test`, GT_ROOT resolves to a LIVE (non-isolated) town, and cwd is a stale
// gastown worktree whose recompiled test binary would lack the isolation guard.
// Every uncertainty resolves to false (allow) so legitimate testing is never
// broken.
func testIsolationShouldBlock(command, liveRoot, cwd string) bool {
	if command == "" || !matchesGoTest(command) {
		return false
	}
	// Only a LIVE (non-isolated) town is at risk. An unset GT_ROOT or an
	// isolated temp sandbox has nothing live to protect — reuse testguard's
	// canonical isolation heuristic rather than re-deriving it.
	if liveRoot == "" || testguard.IsIsolatedSandbox(liveRoot) {
		return false
	}
	// A current worktree's recompiled test binary carries the compiled guard and
	// self-isolates; only a stale one is dangerous.
	return dirIsStaleGastown(cwd)
}

// transparentPrefixes are commands that wrap and then exec another command
// without changing that a `go test` still runs (e.g. `time go test`,
// `env FOO=1 go test`, `command go test`). We recognize only wrappers that take
// no positional argument of their own before the wrapped command (their flags
// are skipped by the leading-flag handling below); wrappers that consume a
// value argument (nice -n N, timeout N, stdbuf -oL) are intentionally left as
// documented residuals rather than parsed heuristically.
var transparentPrefixes = map[string]bool{
	"time": true, "command": true, "exec": true, "env": true, "nohup": true,
}

// matchesGoTest reports whether command runs `go test` as an actual command
// (not merely a string mentioning it). It handles leading env assignments
// (CGO_ENABLED=0 go test), absolute go paths (/usr/local/go/bin/go test),
// transparent wrappers (time/env/command/... go test), and compound commands
// (cd x && go test ./...), while rejecting non-command mentions like
// `echo go test` and unrelated tools like `cargo test`.
//
// It is intentionally liberal: because it only ever gates a fail-open block,
// over-matching is harmless, while missing a real `go test` is the costly
// error. It still cannot see through indirection the substring matcher never
// routes here anyway (`make test`, `bash -c '…'`, a pre-compiled *.test binary)
// — those are documented residuals of this guard.
func matchesGoTest(command string) bool {
	fields := strings.Fields(command)
	cmdPos := true
	for i := 0; i < len(fields); i++ {
		tok := fields[i]

		if isShellSeparator(tok) {
			cmdPos = true
			continue
		}
		if !cmdPos {
			continue
		}
		// Skip leading VAR=val env assignments, transparent command wrappers,
		// and any wrapper flags (e.g. `command -p`, `env -u X`); the real
		// command word follows and cmdPos stays true.
		if isEnvAssignment(tok) || transparentPrefixes[filepath.Base(tok)] || strings.HasPrefix(tok, "-") {
			continue
		}

		// tok is the command word for this pipeline stage.
		cmdPos = false
		if filepath.Base(tok) != "go" {
			continue
		}
		if goArgsRunTest(fields[i+1:]) {
			return true
		}
	}
	return false
}

// goArgsRunTest reports whether the arguments after the `go` command word
// invoke the `test` subcommand. It skips leading flags, including `-C dir`
// which takes a value, and stops at a shell separator.
func goArgsRunTest(args []string) bool {
	for j := 0; j < len(args); j++ {
		a := args[j]
		if isShellSeparator(a) {
			return false
		}
		if a == "-C" { // `go -C <dir> test`: flag takes a directory value
			j++
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		return a == "test"
	}
	return false
}

func isShellSeparator(tok string) bool {
	switch tok {
	case "&&", "||", ";", "|", "&":
		return true
	}
	return false
}

func isEnvAssignment(tok string) bool {
	eq := strings.IndexByte(tok, '=')
	if eq <= 0 {
		return false
	}
	for i := 0; i < eq; i++ {
		c := tok[i]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (i > 0 && c >= '0' && c <= '9') {
			continue
		}
		return false
	}
	return true
}

// dirIsStaleGastown returns true only when dir is inside a gastown-module
// checkout that LACKS the isolation source — i.e. a `go test` compiled here
// would be unprotected. Any uncertainty resolves to false (allow).
func dirIsStaleGastown(dir string) bool {
	moduleRoot, module := findGoModule(dir)
	if moduleRoot == "" || module != gastownModulePath {
		return false
	}
	_, err := os.Stat(filepath.Join(moduleRoot, isolationSourceMarker))
	return os.IsNotExist(err)
}

// findGoModule walks up from dir to the nearest go.mod and returns its
// directory and declared module path. Returns ("", "") if none is found or the
// module line is unreadable.
func findGoModule(dir string) (root, module string) {
	for {
		modPath := filepath.Join(dir, "go.mod")
		if module := readModulePath(modPath); module != "" {
			return dir, module
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ""
		}
		dir = parent
	}
}

func readModulePath(goModPath string) string {
	f, err := os.Open(goModPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if rest, ok := strings.CutPrefix(line, "module"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

func printTestIsolationBlock(command, townRoot string) {
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "╔══════════════════════════════════════════════════════════════════╗")
	fmt.Fprintln(os.Stderr, "║  ❌ STALE-WORKTREE `go test` BLOCKED                             ║")
	fmt.Fprintln(os.Stderr, "╠══════════════════════════════════════════════════════════════════╣")
	fmt.Fprintf(os.Stderr, "║  Command: %-53s ║\n", truncateStr(command, 53))
	fmt.Fprintf(os.Stderr, "║  Town:    %-53s ║\n", truncateStr(townRoot, 53))
	fmt.Fprintln(os.Stderr, "║                                                                  ║")
	fmt.Fprintln(os.Stderr, "║  This worktree predates the test-isolation fix (gt-8ik) and its  ║")
	fmt.Fprintln(os.Stderr, "║  recompiled test binary would carry NO isolation guard, so it    ║")
	fmt.Fprintln(os.Stderr, "║  could mutate the LIVE control plane (beads, tmux, hooks).       ║")
	fmt.Fprintln(os.Stderr, "║                                                                  ║")
	fmt.Fprintln(os.Stderr, "║  Rebase onto origin/main before running go test. See gt-x3yy.    ║")
	fmt.Fprintln(os.Stderr, "╚══════════════════════════════════════════════════════════════════╝")
	fmt.Fprintln(os.Stderr, "")
}
