package version

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestMain stubs findTownRoot for the whole package by default. Without
// this, every test here runs from inside an actual checkout nested in a
// real Gas Town installation (this very repo), which has its own real
// <town>/.runtime/activation/current.json. Left unstubbed, tests that never
// intended to exercise the activation-receipt path could accidentally read
// that real file. Tests that want the receipt path opt in explicitly via
// stubTownRoot.
func TestMain(m *testing.M) {
	findTownRoot = func() (string, error) { return "", errors.New("no town in tests") }
	os.Exit(m.Run())
}

// stubTownRoot points findTownRoot at dir for the duration of the calling
// test only, restoring the package-wide no-town stub afterward.
func stubTownRoot(t *testing.T, dir string) {
	t.Helper()
	orig := findTownRoot
	t.Cleanup(func() { findTownRoot = orig })
	findTownRoot = func() (string, error) { return dir, nil }
}

// writeReceipt writes a <town>/.runtime/activation/current.json fixture.
func writeReceipt(t *testing.T, townRoot string, receipt activationReceipt) {
	t.Helper()
	dir := filepath.Join(townRoot, ".runtime", "activation")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "current.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func validReceipt(sha string) activationReceipt {
	return activationReceipt{
		Action:       "activate",
		Result:       "success",
		NewSHA:       sha,
		SourceRemote: "github.com/jamelt/gastown",
	}
}

// --- activationSHAForTown: pure receipt parsing/validation ---

func TestActivationSHAForTown_Valid(t *testing.T) {
	town := t.TempDir()
	writeReceipt(t, town, validReceipt("abc1234567890abcdef1234567890abcdef1234"))

	sha, ok := activationSHAForTown(town)
	if !ok {
		t.Fatal("expected ok=true for a valid receipt")
	}
	if sha != "abc1234567890abcdef1234567890abcdef1234" {
		t.Errorf("sha = %q, want the receipt's new_sha", sha)
	}
}

func TestActivationSHAForTown_MissingFile(t *testing.T) {
	if _, ok := activationSHAForTown(t.TempDir()); ok {
		t.Error("expected ok=false when current.json does not exist")
	}
}

func TestActivationSHAForTown_MalformedJSON(t *testing.T) {
	town := t.TempDir()
	dir := filepath.Join(town, ".runtime", "activation")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "current.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := activationSHAForTown(town); ok {
		t.Error("expected ok=false for malformed JSON")
	}
}

func TestActivationSHAForTown_RejectsUntrusted(t *testing.T) {
	tests := []struct {
		name    string
		receipt activationReceipt
	}{
		{"wrong action", activationReceipt{Action: "rollback", Result: "success", NewSHA: "abc1234", SourceRemote: "github.com/jamelt/gastown"}},
		{"failed result", activationReceipt{Action: "activate", Result: "failed", NewSHA: "abc1234", SourceRemote: "github.com/jamelt/gastown"}},
		{"untrusted remote", activationReceipt{Action: "activate", Result: "success", NewSHA: "abc1234", SourceRemote: "github.com/someone/fork"}},
		{"empty remote", activationReceipt{Action: "activate", Result: "success", NewSHA: "abc1234", SourceRemote: ""}},
		{"empty sha", activationReceipt{Action: "activate", Result: "success", NewSHA: "", SourceRemote: "github.com/jamelt/gastown"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			town := t.TempDir()
			writeReceipt(t, town, tt.receipt)
			if _, ok := activationSHAForTown(town); ok {
				t.Errorf("expected ok=false for %s receipt, a divergent/untrusted source must fail closed", tt.name)
			}
		})
	}
}

// --- CheckStaleBinaryForCommit: activation receipt short-circuit ---

// TestCheckStaleBinaryForCommit_ActivationReceiptOverridesStaleLocalBranch is
// the gt-qxm regression: right after a verified activation, the resolved
// source worktree's local branch (e.g. the Mayor's rig, which activation
// never touches) can still be behind the very origin/main activation just
// fetched and verified. Before this fix that local lag alone produced a
// false "gt binary is stale ... NOT safe for automated rebuild" warning even
// though the running binary was exactly the just-activated, verified
// revision. The activation receipt must settle freshness first.
func TestCheckStaleBinaryForCommit_ActivationReceiptOverridesStaleLocalBranch(t *testing.T) {
	dir := newGitRepo(t)
	// A local main that lags behind the SHA the binary was actually built
	// from — reproduces the reported "main was not a descendant" case.
	staleLocalMain := gitCommit(t, dir, "a.go", "1")
	gitRun(t, dir, "branch", "-M", "main")

	town := t.TempDir()
	activatedSHA := "abc1234567890abcdef1234567890abcdef1234"
	writeReceipt(t, town, validReceipt(activatedSHA))
	stubTownRoot(t, town)

	info := CheckStaleBinaryForCommit(dir, activatedSHA)
	if info.Error != nil {
		t.Fatalf("unexpected error: %v", info.Error)
	}
	if info.IsStale {
		t.Errorf("IsStale = true, want false: a binary matching the activation receipt is fresh regardless of local branch state (local main at %s)", staleLocalMain)
	}
	if info.RepoCommit != activatedSHA {
		t.Errorf("RepoCommit = %q, want the activated SHA %q", info.RepoCommit, activatedSHA)
	}
}

// TestCheckStaleBinaryForCommit_ActivationReceiptMismatchFallsThrough proves
// the receipt only ever short-circuits on an exact match; when the binary
// differs from the last verified activation, normal repo-based comparison
// still runs (a mismatch is not itself treated as "fresh").
func TestCheckStaleBinaryForCommit_ActivationReceiptMismatchFallsThrough(t *testing.T) {
	dir := newGitRepo(t)
	old := gitCommit(t, dir, "a.go", "1")
	tip := gitCommit(t, dir, "b.go", "2")
	gitRun(t, dir, "branch", "-M", "main")

	town := t.TempDir()
	writeReceipt(t, town, validReceipt("0000000000000000000000000000000000000000"))
	stubTownRoot(t, town)

	info := CheckStaleBinaryForCommit(dir, old)
	if info.Error != nil {
		t.Fatalf("unexpected error: %v", info.Error)
	}
	if !info.IsStale {
		t.Fatal("expected normal repo-based staleness when the receipt doesn't match the binary")
	}
	if info.RepoCommit != tip {
		t.Errorf("RepoCommit = %q, want local main tip %q (repo-based fallback)", info.RepoCommit, tip)
	}
}

// --- resolveRepoRoot / isAuthoritativeGtSource: candidate authority ---

const authoritativeRemote = "https://github.com/jamelt/gastown.git"
const unrelatedRemote = "https://github.com/someone/unrelated.git"

// makeGtSourceRepo creates a temp git repo containing cmd/gt/main.go (the
// hasGtSource marker) with the given "origin" remote URL, or no remote at
// all when remoteURL is "".
func makeGtSourceRepo(t *testing.T, remoteURL string) string {
	t.Helper()
	dir := newGitRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "cmd", "gt"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitCommit(t, dir, "cmd/gt/main.go", "package main\nfunc main() {}\n")
	if remoteURL != "" {
		gitRun(t, dir, "remote", "add", "origin", remoteURL)
	}
	return dir
}

func TestResolveRepoRoot_PrefersAuthoritativeGtRootCandidate(t *testing.T) {
	gtRoot := t.TempDir()
	authoritative := makeGtSourceRepo(t, authoritativeRemote)
	if err := os.Rename(authoritative, filepath.Join(gtRoot, "gastown")); err != nil {
		t.Fatal(err)
	}

	got, err := resolveRepoRoot(gtRoot, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(gtRoot, "gastown")
	if got != want {
		t.Errorf("resolveRepoRoot = %q, want %q", got, want)
	}
}

// TestResolveRepoRoot_TownRootCWDNeverQualifies is the "town-root CWD"
// scenario: CWD resolves (via `git rev-parse --show-toplevel`) to the
// town's own operational git repo — even one that coincidentally contains a
// cmd/gt/main.go file — and it must never be treated as the gt source, since
// it isn't traceable to the authoritative remote.
func TestResolveRepoRoot_TownRootCWDNeverQualifies(t *testing.T) {
	townRoot := makeGtSourceRepo(t, "") // no remote at all, like operational state

	_, err := resolveRepoRoot("", "", townRoot)
	if err == nil {
		t.Fatal("expected error: a CWD-resolved repo with no authoritative remote must never qualify")
	}
}

// TestResolveRepoRoot_UnrelatedRepositoryNeverQualifies is the "unrelated
// Git repository" scenario: CWD falls back to some other git repo entirely,
// which must fail closed rather than being adopted as the gt source.
func TestResolveRepoRoot_UnrelatedRepositoryNeverQualifies(t *testing.T) {
	unrelated := makeGtSourceRepo(t, unrelatedRemote)

	_, err := resolveRepoRoot("", "", unrelated)
	if err == nil {
		t.Fatal("expected error: an unrelated repository's remote must never satisfy authority")
	}
}

// TestResolveRepoRoot_SymlinkedLegacyPathNeverQualifies is the "compatibility
// symlink" scenario: the well-known GT_ROOT/gastown path is a symlink to a
// directory that has gt source but the wrong remote (e.g. a stale forwarding
// symlink left over from a prior layout). It must not be trusted just
// because the path resolves and the file marker is present.
func TestResolveRepoRoot_SymlinkedLegacyPathNeverQualifies(t *testing.T) {
	gtRoot := t.TempDir()
	legacyTarget := makeGtSourceRepo(t, unrelatedRemote)
	if err := os.Symlink(legacyTarget, filepath.Join(gtRoot, "gastown")); err != nil {
		t.Fatal(err)
	}

	_, err := resolveRepoRoot(gtRoot, "", "")
	if err == nil {
		t.Fatal("expected error: a symlinked candidate with the wrong remote must never qualify")
	}
}

// TestResolveRepoRoot_DirtySourceCloneStillQualifies proves authority
// checking is orthogonal to worktree cleanliness: an authoritative candidate
// with uncommitted changes must still resolve normally.
func TestResolveRepoRoot_DirtySourceCloneStillQualifies(t *testing.T) {
	gtRoot := t.TempDir()
	authoritative := makeGtSourceRepo(t, authoritativeRemote)
	if err := os.WriteFile(filepath.Join(authoritative, "dirty.txt"), []byte("uncommitted"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(authoritative, filepath.Join(gtRoot, "gastown")); err != nil {
		t.Fatal(err)
	}

	got, err := resolveRepoRoot(gtRoot, "", "")
	if err != nil {
		t.Fatalf("unexpected error for a dirty but authoritative clone: %v", err)
	}
	want := filepath.Join(gtRoot, "gastown")
	if got != want {
		t.Errorf("resolveRepoRoot = %q, want %q", got, want)
	}
}

func TestResolveRepoRoot_NoCandidatesErrors(t *testing.T) {
	if _, err := resolveRepoRoot("", "", ""); err == nil {
		t.Fatal("expected error when no candidate is available at all")
	}
}
