package activation

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/util"
)

type fakeComponents struct {
	snapshot  ComponentSnapshot
	refreshes int
}

func (f *fakeComponents) Snapshot(context.Context) (ComponentSnapshot, error) {
	return f.snapshot, nil
}

func (f *fakeComponents) Refresh(context.Context, ComponentSnapshot, string, string) ([]ComponentResult, error) {
	f.refreshes++
	return []ComponentResult{
		{Name: "daemon", WasRunning: true, Restarted: true, Verified: true, Detail: "isolated fake restarted"},
		{Name: "dashboard", WasRunning: true, Restarted: true, Verified: true, Detail: "isolated fake restarted"},
	}, nil
}

func TestActivateAndRollbackEndToEndIsolated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	repo, authority, oldSHA, newSHA := makeIntegratedRepo(t, root)
	runtimeDir := filepath.Join(root, "runtime")
	installPath := filepath.Join(runtimeDir, "bin", "gt")
	dashboardPath := filepath.Join(runtimeDir, "libexec", "gt-dashboard")
	stateDir := filepath.Join(runtimeDir, "state")
	if err := os.MkdirAll(filepath.Dir(installPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dashboardPath), 0o755); err != nil {
		t.Fatal(err)
	}
	oldBuild := filepath.Join(root, "old-gt")
	buildTestBinary(t, ctx, repo, oldBuild, oldSHA, "old")
	if err := atomicInstall(oldBuild, installPath); err != nil {
		t.Fatal(err)
	}
	if err := atomicInstall(oldBuild, dashboardPath); err != nil {
		t.Fatal(err)
	}
	oldHash, err := fileSHA256(installPath)
	if err != nil {
		t.Fatal(err)
	}

	components := &fakeComponents{snapshot: ComponentSnapshot{
		DaemonRunning: true,
		Dashboard:     []DashboardProcess{{PID: 1234, Args: []string{"dashboard", "--port", "18080"}}},
	}}
	smokeRuns := 0
	buildRuns := 0
	opts := Options{
		RepoDir: repo, Revision: newSHA, Authority: authority,
		InstallPath: installPath, DashboardPath: dashboardPath,
		StateDir: stateDir, TownRoot: runtimeDir,
		SmokeTimeout: time.Minute, BuildTimeout: time.Minute,
		Components: components,
		Smoke: func(context.Context, string) error {
			smokeRuns++
			return nil
		},
		Build: func(ctx context.Context, sourceDir, outputPath, sha, version string) error {
			buildRuns++
			return buildTestBinaryE(ctx, sourceDir, outputPath, sha, "new")
		},
	}

	receipt, err := Activate(ctx, opts)
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if receipt.Result != "success" || receipt.NewSHA != newSHA || receipt.OldSHA != oldSHA {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
	if smokeRuns != 1 || buildRuns != 1 {
		t.Fatalf("smoke/build runs = %d/%d, want 1/1", smokeRuns, buildRuns)
	}
	if components.refreshes != 1 {
		t.Fatalf("component refreshes = %d, want 1", components.refreshes)
	}
	newHash, err := fileSHA256(installPath)
	if err != nil {
		t.Fatal(err)
	}
	if newHash == oldHash {
		t.Fatal("activation did not change the binary")
	}
	if dashboardHash, _ := fileSHA256(dashboardPath); dashboardHash != newHash {
		t.Fatalf("dashboard hash %s != CLI hash %s", dashboardHash, newHash)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "current.json")); err != nil {
		t.Fatalf("current receipt: %v", err)
	}

	rollbackReceipt, err := Rollback(ctx, opts)
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if rollbackReceipt.Result != "success" || rollbackReceipt.NewSHA != oldSHA {
		t.Fatalf("unexpected rollback receipt: %+v", rollbackReceipt)
	}
	if got, _ := fileSHA256(installPath); got != oldHash {
		t.Fatalf("CLI rollback hash = %s, want %s", got, oldHash)
	}
	if got, _ := fileSHA256(dashboardPath); got != oldHash {
		t.Fatalf("dashboard rollback hash = %s, want %s", got, oldHash)
	}
	if components.refreshes != 2 {
		t.Fatalf("component refreshes after rollback = %d, want 2", components.refreshes)
	}
	if matches, _ := filepath.Glob(filepath.Join(stateDir, "receipts", "*.json")); len(matches) != 2 {
		t.Fatalf("receipt count = %d, want 2", len(matches))
	}
}

func TestActivateRejectsUnintegratedRevision(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	repo, authority, _, _ := makeIntegratedRepo(t, root)
	runGit(t, repo, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(repo, "feature.txt"), []byte("not integrated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "feature.txt")
	runGit(t, repo, "commit", "-m", "unintegrated")
	unintegrated := runGit(t, repo, "rev-parse", "HEAD")

	opts := Options{
		RepoDir: repo, Revision: unintegrated, Authority: authority,
		InstallPath:   filepath.Join(root, "bin", "gt"),
		DashboardPath: filepath.Join(root, "libexec", "gt-dashboard"),
		StateDir:      filepath.Join(root, "state"), TownRoot: root,
		Smoke:      func(context.Context, string) error { t.Fatal("smoke must not run"); return nil },
		Build:      func(context.Context, string, string, string, string) error { t.Fatal("build must not run"); return nil },
		Components: &fakeComponents{},
	}
	_, err := Activate(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "not integrated") {
		t.Fatalf("Activate error = %v, want not integrated", err)
	}
}

func TestActivateRejectsWrongAuthorityAndAbbreviatedSHA(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	repo, authority, _, newSHA := makeIntegratedRepo(t, root)
	base := Options{
		RepoDir: repo, Revision: newSHA,
		InstallPath:   filepath.Join(root, "bin", "gt"),
		DashboardPath: filepath.Join(root, "libexec", "gt-dashboard"),
		StateDir:      filepath.Join(root, "state-a"), TownRoot: root,
		Components: &fakeComponents{},
	}
	wrong := base
	wrong.Authority = "github.com/someone/upstream"
	if _, err := Activate(context.Background(), wrong); err == nil || !strings.Contains(err.Error(), "source authority") {
		t.Fatalf("authority error = %v", err)
	}
	abbreviated := base
	abbreviated.Authority = authority
	abbreviated.Revision = newSHA[:12]
	abbreviated.StateDir = filepath.Join(root, "state-b")
	if _, err := Activate(context.Background(), abbreviated); err == nil || !strings.Contains(err.Error(), "exact 40-character") {
		t.Fatalf("abbreviated error = %v", err)
	}
}

func TestActivationLockSerializesConcurrentRuns(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	unlock, err := acquireActivationLock(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := acquireActivationLock(stateDir); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("second lock error = %v, want already running", err)
	}
}

func makeIntegratedRepo(t *testing.T, root string) (repo, authority, oldSHA, newSHA string) {
	t.Helper()
	bare := filepath.Join(root, "origin.git")
	repo = filepath.Join(root, "repo")
	runGit(t, root, "init", "--bare", bare)
	runGit(t, root, "init", "-b", "main", repo)
	runGit(t, repo, "config", "user.email", "activation-test@example.invalid")
	runGit(t, repo, "config", "user.name", "Activation Test")
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module activation.test\n\ngo 1.26.2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTestMain(t, repo, "old")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "old main")
	oldSHA = runGit(t, repo, "rev-parse", "HEAD")
	runGit(t, repo, "remote", "add", "origin", bare)
	runGit(t, repo, "push", "-u", "origin", "main")
	writeTestMain(t, repo, "new")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "new main")
	newSHA = runGit(t, repo, "rev-parse", "HEAD")
	runGit(t, repo, "push", "origin", "main")
	authority = util.CanonicalRemote(bare)
	return repo, authority, oldSHA, newSHA
}

func writeTestMain(t *testing.T, repo, marker string) {
	t.Helper()
	source := fmt.Sprintf(`package main

import (
	"fmt"
	"os"
)

var commit = "unknown"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("gt version test (%s: main@%%s)\\n", commit)
	}
}
`, marker)
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}

func buildTestBinary(t *testing.T, ctx context.Context, repo, output, sha, marker string) {
	t.Helper()
	if err := buildTestBinaryE(ctx, repo, output, sha, marker); err != nil {
		t.Fatal(err)
	}
}

func buildTestBinaryE(ctx context.Context, repo, output, sha, marker string) error {
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-buildvcs=true", "-ldflags", "-X main.commit="+sha, "-o", output, ".")
	cmd.Dir = repo
	outputBytes, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("building %s test binary: %w: %s", marker, err, outputBytes)
	}
	return nil
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}
