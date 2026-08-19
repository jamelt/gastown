package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gitpkg "github.com/steveyegge/gastown/internal/git"
)

// fakeRebaseGit lets us drive autoRebaseOnTarget without a real git repo for
// the gating-decision tests.
type fakeRebaseGit struct {
	rebaseErr   error
	rebaseCalls int
	abortCalls  int
}

func (f *fakeRebaseGit) Rebase(onto string) error {
	f.rebaseCalls++
	return f.rebaseErr
}

func (f *fakeRebaseGit) AbortRebase() error {
	f.abortCalls++
	return nil
}

// fakePreVerifiedGit drives preVerifiedFields without a real git repo.
type fakePreVerifiedGit struct {
	baseRef    string
	baseSHA    string
	baseErr    error
	ancestor   bool
	ancestErr  error
	ancestorOf [2]string // records the (ancestor, descendant) args of IsAncestor
}

func (f *fakePreVerifiedGit) CleanBaseRef(remote, defaultBranch, target string) string {
	return f.baseRef
}

func (f *fakePreVerifiedGit) Rev(ref string) (string, error) {
	return f.baseSHA, f.baseErr
}

func (f *fakePreVerifiedGit) IsAncestor(ancestor, descendant string) (bool, error) {
	f.ancestorOf = [2]string{ancestor, descendant}
	return f.ancestor, f.ancestErr
}

// TestPreVerifiedFields verifies that the pre-verification attestation is only
// recorded when the submitted commit is actually based on the recorded target
// base (gt-fi6e). An unrebased/behind branch must fall back to full gates.
func TestPreVerifiedFields(t *testing.T) {
	const base = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const commit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	t.Run("records attestation when commit is based on target", func(t *testing.T) {
		g := &fakePreVerifiedGit{baseRef: "origin/main", baseSHA: base, ancestor: true}
		lines, skip := preVerifiedFields(g, "main", "main", commit, "2026-08-19T00:00:00Z")
		if skip != "" {
			t.Fatalf("expected attestation, got skip reason %q", skip)
		}
		if !strings.Contains(lines, "pre_verified: true") ||
			!strings.Contains(lines, "pre_verified_at: 2026-08-19T00:00:00Z") ||
			!strings.Contains(lines, "pre_verified_base: "+base) {
			t.Fatalf("attestation lines missing expected fields: %q", lines)
		}
		if g.ancestorOf != [2]string{base, commit} {
			t.Fatalf("IsAncestor called with %v, want ancestor=base descendant=commit", g.ancestorOf)
		}
	})

	t.Run("refuses attestation when commit is not based on target", func(t *testing.T) {
		g := &fakePreVerifiedGit{baseRef: "origin/main", baseSHA: base, ancestor: false}
		lines, skip := preVerifiedFields(g, "main", "main", commit, "2026-08-19T00:00:00Z")
		if lines != "" {
			t.Fatalf("expected no attestation, got lines %q", lines)
		}
		if !strings.Contains(skip, "not based on target") {
			t.Fatalf("skip reason %q should explain the branch was not rebased", skip)
		}
	})

	t.Run("refuses attestation when commit SHA is unknown", func(t *testing.T) {
		g := &fakePreVerifiedGit{baseRef: "origin/main", baseSHA: base, ancestor: true}
		lines, skip := preVerifiedFields(g, "main", "main", "", "2026-08-19T00:00:00Z")
		if lines != "" || skip == "" {
			t.Fatalf("expected skip for empty commit, got lines=%q skip=%q", lines, skip)
		}
	})

	t.Run("refuses attestation when base cannot be resolved", func(t *testing.T) {
		g := &fakePreVerifiedGit{baseRef: "origin/main", baseErr: errors.New("bad ref")}
		lines, skip := preVerifiedFields(g, "main", "main", commit, "2026-08-19T00:00:00Z")
		if lines != "" || skip == "" {
			t.Fatalf("expected skip when base unresolved, got lines=%q skip=%q", lines, skip)
		}
	})

	t.Run("refuses attestation when ancestry check errors", func(t *testing.T) {
		g := &fakePreVerifiedGit{baseRef: "origin/main", baseSHA: base, ancestErr: errors.New("object missing")}
		lines, skip := preVerifiedFields(g, "main", "main", commit, "2026-08-19T00:00:00Z")
		if lines != "" || skip == "" {
			t.Fatalf("expected skip when ancestry errors, got lines=%q skip=%q", lines, skip)
		}
	})
}

// TestAutoRebaseOnTarget_GatingDecisions verifies the skip/rebase decision
// matrix (gh#3400). The behavior under test is the *decision*, not the actual
// git mechanics — those are exercised separately below against a real repo.
func TestAutoRebaseOnTarget_GatingDecisions(t *testing.T) {
	tests := []struct {
		name          string
		behind        int
		preVerified   bool
		alreadyPushed bool
		wantRebased   bool
		wantSkip      string
		wantCalls     int
	}{
		{
			name:        "not behind: no-op",
			behind:      0,
			wantRebased: false,
			wantSkip:    "",
			wantCalls:   0,
		},
		{
			name:        "behind by 1: rebase runs",
			behind:      1,
			wantRebased: true,
			wantSkip:    "",
			wantCalls:   1,
		},
		{
			name:        "behind by 5: rebase runs",
			behind:      5,
			wantRebased: true,
			wantSkip:    "",
			wantCalls:   1,
		},
		{
			name:        "pre-verified: skip even when behind",
			behind:      3,
			preVerified: true,
			wantRebased: false,
			wantSkip:    "--pre-verified is set",
			wantCalls:   0,
		},
		{
			name:          "already pushed: skip to avoid divergence",
			behind:        3,
			alreadyPushed: true,
			wantRebased:   false,
			wantSkip:      "prior push checkpoint exists",
			wantCalls:     0,
		},
		{
			name:          "pre-verified takes precedence over already-pushed",
			behind:        3,
			preVerified:   true,
			alreadyPushed: true,
			wantRebased:   false,
			wantSkip:      "--pre-verified is set",
			wantCalls:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeRebaseGit{}
			rebased, skipReason, err := autoRebaseOnTarget(fake, "origin/main", tt.behind, tt.preVerified, tt.alreadyPushed)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rebased != tt.wantRebased {
				t.Errorf("rebased = %v, want %v", rebased, tt.wantRebased)
			}
			if skipReason != tt.wantSkip {
				t.Errorf("skipReason = %q, want %q", skipReason, tt.wantSkip)
			}
			if fake.rebaseCalls != tt.wantCalls {
				t.Errorf("rebase calls = %d, want %d", fake.rebaseCalls, tt.wantCalls)
			}
			if fake.abortCalls != 0 {
				t.Errorf("abort calls = %d on success path, want 0", fake.abortCalls)
			}
		})
	}
}

// TestAutoRebaseOnTarget_ConflictAborts verifies that a rebase failure causes
// AbortRebase to fire and the returned error includes remediation guidance.
func TestAutoRebaseOnTarget_ConflictAborts(t *testing.T) {
	fake := &fakeRebaseGit{rebaseErr: errors.New("CONFLICT (content): merge conflict in foo.txt")}

	rebased, skipReason, err := autoRebaseOnTarget(fake, "origin/main", 1, false, false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if rebased {
		t.Error("rebased should be false on conflict")
	}
	if skipReason != "" {
		t.Errorf("skipReason should be empty on conflict, got %q", skipReason)
	}
	if fake.rebaseCalls != 1 {
		t.Errorf("expected 1 rebase call, got %d", fake.rebaseCalls)
	}
	if fake.abortCalls != 1 {
		t.Errorf("expected AbortRebase to fire on conflict, got %d calls", fake.abortCalls)
	}
	// Remediation guidance is part of the contract — agents read this message.
	msg := err.Error()
	if !strings.Contains(msg, "auto-rebase onto origin/main failed") {
		t.Errorf("error missing context: %q", msg)
	}
	if !strings.Contains(msg, "git rebase origin/main") {
		t.Errorf("error missing remediation hint: %q", msg)
	}
	if !strings.Contains(msg, "rerun gt done") {
		t.Errorf("error missing rerun hint: %q", msg)
	}
}

// TestAutoRebaseOnTarget_RealRepoSuccess exercises the rebase against a real
// git working tree to confirm the wiring (Rebase call) actually replays the
// branch onto a moved base. (gh#3400, scenario (a) from the bead notes.)
func TestAutoRebaseOnTarget_RealRepoSuccess(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	testRunGit(t, tmp, "init", "--initial-branch", "main", repo)
	testRunGit(t, repo, "config", "user.email", "test@test.com")
	testRunGit(t, repo, "config", "user.name", "Test")

	// Initial commit on main.
	writeRepoFile(t, repo, "README.md", "# initial\n")
	testRunGit(t, repo, "add", ".")
	testRunGit(t, repo, "commit", "-m", "initial")

	// Branch off and add a polecat commit on a non-conflicting file.
	testRunGit(t, repo, "checkout", "-b", "feature")
	writeRepoFile(t, repo, "feature.txt", "feature work\n")
	testRunGit(t, repo, "add", ".")
	testRunGit(t, repo, "commit", "-m", "feature")

	// Move main forward independently — also non-conflicting with feature.
	testRunGit(t, repo, "checkout", "main")
	writeRepoFile(t, repo, "main-new.txt", "new on main\n")
	testRunGit(t, repo, "add", ".")
	testRunGit(t, repo, "commit", "-m", "advance main")

	testRunGit(t, repo, "checkout", "feature")

	g := gitpkg.NewGit(repo)
	rebased, skipReason, err := autoRebaseOnTarget(g, "main", 1, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rebased {
		t.Fatalf("expected rebased=true, got false (skip=%q)", skipReason)
	}

	// After rebase, both files must be present and the feature commit must sit
	// on top of the advance-main commit.
	if _, statErr := os.Stat(filepath.Join(repo, "main-new.txt")); statErr != nil {
		t.Errorf("main-new.txt missing after rebase: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(repo, "feature.txt")); statErr != nil {
		t.Errorf("feature.txt missing after rebase: %v", statErr)
	}
}

// TestAutoRebaseOnTarget_RealRepoConflictAborts exercises the conflict path
// against a real git working tree: feature and main both touch the same file,
// rebase fails with a CONFLICT, and AbortRebase must restore the working tree
// so the polecat can address the conflict manually. (gh#3400, scenario (b).)
func TestAutoRebaseOnTarget_RealRepoConflictAborts(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	testRunGit(t, tmp, "init", "--initial-branch", "main", repo)
	testRunGit(t, repo, "config", "user.email", "test@test.com")
	testRunGit(t, repo, "config", "user.name", "Test")

	writeRepoFile(t, repo, "shared.txt", "v0\n")
	testRunGit(t, repo, "add", ".")
	testRunGit(t, repo, "commit", "-m", "initial")

	// Feature changes shared.txt to v1.
	testRunGit(t, repo, "checkout", "-b", "feature")
	writeRepoFile(t, repo, "shared.txt", "v1-feature\n")
	testRunGit(t, repo, "add", ".")
	testRunGit(t, repo, "commit", "-m", "feature edit")

	// Main changes shared.txt to a different value — guaranteed conflict.
	testRunGit(t, repo, "checkout", "main")
	writeRepoFile(t, repo, "shared.txt", "v1-main\n")
	testRunGit(t, repo, "add", ".")
	testRunGit(t, repo, "commit", "-m", "main edit")

	testRunGit(t, repo, "checkout", "feature")

	g := gitpkg.NewGit(repo)
	rebased, skipReason, err := autoRebaseOnTarget(g, "main", 1, false, false)
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
	if rebased {
		t.Error("rebased should be false on conflict")
	}
	if skipReason != "" {
		t.Errorf("skipReason should be empty on conflict, got %q", skipReason)
	}

	// AbortRebase is required to leave the working tree in a clean state. After
	// abort, the rebase-merge dir must be gone — otherwise the polecat is stuck
	// in a half-rebased state.
	if _, statErr := os.Stat(filepath.Join(repo, ".git", "rebase-merge")); !os.IsNotExist(statErr) {
		t.Errorf(".git/rebase-merge should not exist after abort (stat err: %v)", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".git", "rebase-apply")); !os.IsNotExist(statErr) {
		t.Errorf(".git/rebase-apply should not exist after abort (stat err: %v)", statErr)
	}
}

func writeRepoFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
