package activation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
var versionSHA = regexp.MustCompile(`(?:@|: )([0-9a-f]{7,40})(?:\)|\s|$)`)

type installPlan struct {
	record FileRecord
	target string
	backup string
}

// Activate validates, builds, installs, refreshes, verifies, and records one
// exact integrated main revision. Any failure after the swap restores the
// prior binary set before returning.
func Activate(ctx context.Context, opts Options) (*Receipt, error) {
	if err := normalizeOptions(&opts); err != nil {
		return nil, err
	}
	unlock, err := acquireActivationLock(opts.StateDir)
	if err != nil {
		return nil, err
	}
	defer unlock()

	receipt := &Receipt{
		Schema:        1,
		Action:        "activate",
		Result:        "failed",
		ActivatedAt:   time.Now().UTC(),
		SourceRemote:  opts.Authority,
		SourceMainRef: opts.MainRef,
		SmokeGate:     "pending",
	}
	finishFailure := func(cause error) (*Receipt, error) {
		receipt.CompletedAt = time.Now().UTC()
		receipt.Failure = safeReceiptFailure(cause)
		_ = writeNamedReceipt(opts.StateDir, receipt, false)
		return receipt, cause
	}

	remoteURL, err := gitOutput(ctx, opts.RepoDir, 15*time.Second, "remote", "get-url", opts.Remote)
	if err != nil {
		return finishFailure(fmt.Errorf("reading %s remote: %w", opts.Remote, err))
	}
	if got := canonicalRemote(remoteURL); got != opts.Authority {
		return finishFailure(fmt.Errorf("source authority %q is not required %q", got, opts.Authority))
	}
	refspec := "+refs/heads/main:" + opts.MainRef
	if _, err := gitOutput(ctx, opts.RepoDir, 45*time.Second, "fetch", "--quiet", opts.Remote, refspec); err != nil {
		return finishFailure(fmt.Errorf("fetching integrated %s/main: %w", opts.Remote, err))
	}

	revision := opts.Revision
	if revision == "" || revision == "main" {
		revision = opts.MainRef
	} else if !fullSHA.MatchString(revision) {
		return finishFailure(errors.New("revision must be an exact 40-character SHA, 'main', or empty to resolve integrated main"))
	}
	sha, err := gitOutput(ctx, opts.RepoDir, 10*time.Second, "rev-parse", "--verify", revision+"^{commit}")
	if err != nil || !fullSHA.MatchString(sha) {
		return finishFailure(fmt.Errorf("resolving exact revision %q: %w", revision, err))
	}
	receipt.NewSHA = sha
	if _, err := gitOutput(ctx, opts.RepoDir, 10*time.Second, "merge-base", "--is-ancestor", sha, opts.MainRef); err != nil {
		return finishFailure(fmt.Errorf("revision %s is not integrated in %s", sha, opts.MainRef))
	}

	oldSHA := installedRevision(ctx, opts.InstallPath)
	if oldSHA != "" {
		if resolvedOld, resolveErr := gitOutput(ctx, opts.RepoDir, 10*time.Second, "rev-parse", "--verify", oldSHA+"^{commit}"); resolveErr == nil {
			oldSHA = resolvedOld
			if oldSHA == sha {
				return finishFailure(fmt.Errorf("revision %s is already active", sha))
			}
			if _, ancestorErr := gitOutput(ctx, opts.RepoDir, 10*time.Second, "merge-base", "--is-ancestor", oldSHA, sha); ancestorErr != nil {
				return finishFailure(fmt.Errorf("refusing non-forward activation %s -> %s", oldSHA, sha))
			}
		}
	}
	receipt.OldSHA = oldSHA

	tempRoot, err := os.MkdirTemp("", "gt-activate-")
	if err != nil {
		return finishFailure(fmt.Errorf("creating isolated build root: %w", err))
	}
	defer os.RemoveAll(tempRoot)
	sourceDir := filepath.Join(tempRoot, "source")
	if _, err := gitOutput(ctx, opts.RepoDir, 30*time.Second, "worktree", "add", "--quiet", "--detach", sourceDir, sha); err != nil {
		return finishFailure(fmt.Errorf("materializing isolated revision: %w", err))
	}
	defer func() {
		_, _ = gitOutput(context.Background(), opts.RepoDir, 30*time.Second, "worktree", "remove", "--force", sourceDir)
	}()
	isolatedHead, err := gitOutput(ctx, sourceDir, 10*time.Second, "rev-parse", "HEAD")
	if err != nil || isolatedHead != sha {
		return finishFailure(fmt.Errorf("isolated source resolved to %q, expected %s", isolatedHead, sha))
	}
	status, err := gitOutput(ctx, sourceDir, 10*time.Second, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil || status != "" {
		return finishFailure(fmt.Errorf("isolated source is dirty: %s", status))
	}
	receipt.SourceClean = true

	smokeCtx, cancelSmoke := context.WithTimeout(ctx, opts.SmokeTimeout)
	err = opts.Smoke(smokeCtx, sourceDir)
	cancelSmoke()
	if err != nil {
		receipt.SmokeGate = "failed"
		return finishFailure(fmt.Errorf("required bounded smoke gate: %w", err))
	}
	receipt.SmokeGate = "passed"
	status, err = gitOutput(ctx, sourceDir, 10*time.Second, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil || status != "" {
		return finishFailure(fmt.Errorf("smoke gate dirtied isolated source: %s", status))
	}

	version, err := gitOutput(ctx, sourceDir, 10*time.Second, "describe", "--tags", "--always", sha)
	if err != nil {
		return finishFailure(fmt.Errorf("resolving build version: %w", err))
	}
	builtPath := filepath.Join(tempRoot, "gt")
	buildCtx, cancelBuild := context.WithTimeout(ctx, opts.BuildTimeout)
	err = opts.Build(buildCtx, sourceDir, builtPath, sha, version)
	cancelBuild()
	if err != nil {
		return finishFailure(fmt.Errorf("building exact revision once: %w", err))
	}
	if err := verifyBuiltBinary(ctx, builtPath, sha); err != nil {
		return finishFailure(err)
	}

	snapshot, err := opts.Components.Snapshot(ctx)
	if err != nil {
		return finishFailure(fmt.Errorf("capturing component set: %w", err))
	}
	plans, err := prepareInstallPlans(opts, receipt, builtPath)
	if err != nil {
		return finishFailure(err)
	}
	receipt.Files = recordsFromPlans(plans)

	installed := 0
	for i := range plans {
		if err := atomicInstall(builtPath, plans[i].target); err != nil {
			receipt.Files = recordsFromPlans(plans)
			restoreErr := restorePlans(plans[:installed])
			return finishFailure(errors.Join(fmt.Errorf("installing %s: %w", plans[i].record.Role, err), restoreErr))
		}
		plans[i].record.Installed = true
		installed++
		receipt.Files = recordsFromPlans(plans)
	}

	rollback := func(cause error) (*Receipt, error) {
		receipt.Files = recordsFromPlans(plans)
		restoreErr := restorePlans(plans)
		_, componentErr := opts.Components.Refresh(ctx, snapshot, opts.InstallPath, opts.DashboardPath)
		return finishFailure(errors.Join(cause, restoreErr, componentErr))
	}
	for i := range plans {
		got, hashErr := fileSHA256(plans[i].target)
		if hashErr != nil || got != plans[i].record.NewSHA256 {
			return rollback(fmt.Errorf("post-install checksum mismatch for %s", plans[i].record.Role))
		}
		plans[i].record.Verified = true
	}
	if opts.VerifyPATH {
		if err := verifyCommandPath(opts.InstallPath); err != nil {
			return rollback(err)
		}
	}
	componentResults, err := opts.Components.Refresh(ctx, snapshot, opts.InstallPath, opts.DashboardPath)
	receipt.Components = componentResults
	if err != nil {
		return rollback(fmt.Errorf("refreshing activated components: %w", err))
	}

	receipt.Files = recordsFromPlans(plans)
	receipt.Result = "success"
	receipt.Verification = "PATH and installed binary checksums match; prior running components restarted and verified"
	receipt.CompletedAt = time.Now().UTC()
	if err := writeNamedReceipt(opts.StateDir, receipt, true); err != nil {
		return rollback(fmt.Errorf("writing activation receipt: %w", err))
	}
	return receipt, nil
}

func normalizeOptions(opts *Options) error {
	if opts.RepoDir == "" || opts.InstallPath == "" || opts.StateDir == "" || opts.TownRoot == "" {
		return errors.New("repo, install path, state directory, and town root are required")
	}
	if opts.Remote == "" {
		opts.Remote = "origin"
	}
	if opts.Authority == "" {
		opts.Authority = DefaultAuthority
	}
	if opts.MainRef == "" {
		opts.MainRef = "refs/remotes/" + opts.Remote + "/main"
	}
	if opts.SmokeTimeout <= 0 {
		opts.SmokeTimeout = 5 * time.Minute
	}
	if opts.BuildTimeout <= 0 {
		opts.BuildTimeout = 5 * time.Minute
	}
	if opts.DashboardPath == "" {
		return errors.New("dashboard install path is required")
	}
	if opts.Smoke == nil {
		opts.Smoke = defaultSmoke(opts.SmokeTimeout)
	}
	if opts.Build == nil {
		opts.Build = defaultBuild
	}
	if opts.Components == nil {
		opts.Components = &processController{townRoot: opts.TownRoot, dashboardPath: opts.DashboardPath}
	}
	for _, path := range []string{opts.RepoDir, opts.InstallPath, opts.DashboardPath, opts.StateDir, opts.TownRoot} {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("activation path must be absolute: %s", path)
		}
	}
	return os.MkdirAll(opts.StateDir, 0o700)
}

func acquireActivationLock(stateDir string) (func(), error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	lock := flock.New(filepath.Join(stateDir, "activation.lock"))
	locked, err := lock.TryLock()
	if err != nil {
		return nil, fmt.Errorf("acquiring activation lock: %w", err)
	}
	if !locked {
		return nil, errors.New("another activation or rollback is already running")
	}
	return func() { _ = lock.Unlock() }, nil
}

func gitOutput(ctx context.Context, dir string, timeout time.Duration, args ...string) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	output, err := runCombined(commandCtx, dir, append(os.Environ(), "GIT_TERMINAL_PROMPT=0"), "git", args...)
	if commandCtx.Err() != nil {
		return output, commandCtx.Err()
	}
	if err != nil {
		if len(output) > 1000 {
			output = output[len(output)-1000:]
		}
		return output, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(output), nil
}

func canonicalRemote(remote string) string {
	remote = strings.TrimSpace(strings.TrimSuffix(remote, ".git"))
	if strings.HasPrefix(remote, "git@") {
		remote = strings.TrimPrefix(remote, "git@")
		remote = strings.Replace(remote, ":", "/", 1)
		return remote
	}
	for _, prefix := range []string{"https://", "http://", "ssh://git@", "ssh://"} {
		remote = strings.TrimPrefix(remote, prefix)
	}
	if at := strings.Index(remote, "@"); at >= 0 {
		remote = remote[at+1:]
	}
	return remote
}

func installedRevision(ctx context.Context, path string) string {
	output, err := runCombined(ctx, filepath.Dir(path), nil, path, "version")
	if err != nil {
		return ""
	}
	match := versionSHA.FindStringSubmatch(output)
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func defaultSmoke(timeout time.Duration) SmokeFunc {
	return func(ctx context.Context, sourceDir string) error {
		// Keep the fast activation gate hermetic. Several broader command/daemon
		// suites intentionally exercise real tmux and Dolt lifecycle paths; those
		// belong in pre-merge CI, not a post-merge production activation lane.
		args := []string{"test", "-count=1", "-timeout", timeout.String(), "./internal/activation", "./internal/atomicfile", "./internal/version", "./internal/workspace"}
		output, err := runCombined(ctx, sourceDir, nil, "go", args...)
		if err != nil {
			if len(output) > 4000 {
				output = output[len(output)-4000:]
			}
			return fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), err, output)
		}
		return nil
	}
}

func defaultBuild(ctx context.Context, sourceDir, outputPath, sha, version string) error {
	ldflags := strings.Join([]string{
		"-s", "-w",
		"-X", "github.com/steveyegge/gastown/internal/cmd.Version=" + version,
		"-X", "github.com/steveyegge/gastown/internal/cmd.Commit=" + sha,
		"-X", "github.com/steveyegge/gastown/internal/cmd.Branch=main",
		"-X", "github.com/steveyegge/gastown/internal/cmd.BuiltProperly=1",
	}, " ")
	output, err := runCombined(ctx, sourceDir, nil, "go", "build", "-trimpath", "-buildvcs=true", "-ldflags", ldflags, "-o", outputPath, "./cmd/gt")
	if err != nil {
		if len(output) > 4000 {
			output = output[len(output)-4000:]
		}
		return fmt.Errorf("go build: %w\n%s", err, output)
	}
	return nil
}

func verifyBuiltBinary(ctx context.Context, path, sha string) error {
	output, err := runCombined(ctx, filepath.Dir(path), nil, path, "version")
	if err != nil {
		return fmt.Errorf("running built binary: %w: %s", err, output)
	}
	if !strings.Contains(output, sha[:12]) {
		return fmt.Errorf("built binary reports wrong revision: %s", output)
	}
	metadata, err := runCombined(ctx, filepath.Dir(path), nil, "go", "version", "-m", path)
	if err != nil {
		return fmt.Errorf("reading built binary metadata: %w", err)
	}
	if strings.Contains(metadata, "+dirty") || strings.Contains(metadata, "vcs.modified=true") {
		return errors.New("built binary metadata is dirty")
	}
	return nil
}

func prepareInstallPlans(opts Options, receipt *Receipt, builtPath string) ([]installPlan, error) {
	newHash, err := fileSHA256(builtPath)
	if err != nil {
		return nil, err
	}
	targets := []struct{ role, path string }{{"dashboard", opts.DashboardPath}, {"cli", opts.InstallPath}}
	if samePath(opts.DashboardPath, opts.InstallPath) {
		targets = targets[1:]
	}
	plans := make([]installPlan, 0, len(targets))
	stamp := receipt.ActivatedAt.Format("20060102T150405.000000000Z")
	for _, target := range targets {
		plan := installPlan{target: target.path, record: FileRecord{Role: target.role, Path: redactPath(target.path), NewSHA256: newHash}}
		info, statErr := os.Stat(target.path)
		if statErr == nil {
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("%s target is not a regular file: %s", target.role, target.path)
			}
			plan.record.Existed = true
			oldHash, err := fileSHA256(target.path)
			if err != nil {
				return nil, err
			}
			plan.record.OldSHA256 = oldHash
			backupName := fmt.Sprintf("%s-%s-%s", stamp, target.role, oldHash[:12])
			backupPath, err := ensureUnderStateDir(opts.StateDir, backupName)
			if err != nil {
				return nil, err
			}
			if err := os.MkdirAll(filepath.Dir(backupPath), 0o700); err != nil {
				return nil, err
			}
			if err := copyFile(target.path, backupPath, 0o500); err != nil {
				return nil, fmt.Errorf("backing up %s: %w", target.role, err)
			}
			plan.backup = backupPath
			plan.record.BackupFile = filepath.Base(backupPath)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return nil, statErr
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

func restorePlans(plans []installPlan) error {
	var errs []error
	for i := len(plans) - 1; i >= 0; i-- {
		plan := plans[i]
		if !plan.record.Installed {
			continue
		}
		if plan.record.Existed {
			if err := atomicInstall(plan.backup, plan.target); err != nil {
				errs = append(errs, fmt.Errorf("restoring %s: %w", plan.record.Role, err))
			}
		} else if err := os.Remove(plan.target); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("removing newly-created %s: %w", plan.record.Role, err))
		}
	}
	return errors.Join(errs...)
}

func recordsFromPlans(plans []installPlan) []FileRecord {
	records := make([]FileRecord, 0, len(plans))
	for _, plan := range plans {
		records = append(records, plan.record)
	}
	return records
}

func writeNamedReceipt(stateDir string, receipt *Receipt, current bool) error {
	sha := receipt.NewSHA
	if len(sha) > 12 {
		sha = sha[:12]
	}
	if sha == "" {
		sha = "unknown"
	}
	name := fmt.Sprintf("%s-%s-%s.json", receipt.ActivatedAt.Format("20060102T150405.000000000Z"), receipt.Action, sha)
	path := filepath.Join(stateDir, "receipts", name)
	if err := writeJSONAtomic(path, receipt); err != nil {
		return err
	}
	receipt.ReceiptFile = path
	if current {
		return writeJSONAtomic(filepath.Join(stateDir, "current.json"), receipt)
	}
	return nil
}

func verifyCommandPath(target string) error {
	resolved, err := exec.LookPath("gt")
	if err != nil {
		return fmt.Errorf("PATH does not resolve gt: %w", err)
	}
	targetInfo, err := os.Stat(target)
	if err != nil {
		return err
	}
	resolvedInfo, err := os.Stat(resolved)
	if err != nil {
		return err
	}
	if !os.SameFile(targetInfo, resolvedInfo) {
		return fmt.Errorf("PATH resolves gt to %s, not activated target %s", resolved, target)
	}
	return nil
}

func samePath(a, b string) bool {
	aa, _ := filepath.Abs(a)
	bb, _ := filepath.Abs(b)
	return aa == bb
}
