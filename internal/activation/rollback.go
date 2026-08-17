package activation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Rollback restores the exact binary set recorded by the last successful
// activation and refreshes the same currently-running component set.
func Rollback(ctx context.Context, opts Options) (*Receipt, error) {
	if err := normalizeOptions(&opts); err != nil {
		return nil, err
	}
	unlock, err := acquireActivationLock(opts.StateDir)
	if err != nil {
		return nil, err
	}
	defer unlock()

	active, err := readReceipt(filepath.Join(opts.StateDir, "current.json"))
	if err != nil {
		return nil, fmt.Errorf("reading current activation receipt: %w", err)
	}
	if active.Action != "activate" || active.Result != "success" {
		return nil, errors.New("current receipt is not a successful activation; refusing ambiguous rollback")
	}
	receipt := &Receipt{
		Schema:        1,
		Action:        "rollback",
		Result:        "failed",
		ActivatedAt:   time.Now().UTC(),
		OldSHA:        active.NewSHA,
		NewSHA:        active.OldSHA,
		SourceRemote:  active.SourceRemote,
		SourceMainRef: active.SourceMainRef,
		SourceClean:   true,
		SmokeGate:     "not-required: restoring prior verified binary",
	}
	finishFailure := func(cause error) (*Receipt, error) {
		receipt.CompletedAt = time.Now().UTC()
		receipt.Failure = safeReceiptFailure(cause)
		_ = writeNamedReceipt(opts.StateDir, receipt, false)
		return receipt, cause
	}

	targetForRole := map[string]string{"cli": opts.InstallPath, "dashboard": opts.DashboardPath}
	tempRoot, err := os.MkdirTemp("", "gt-rollback-")
	if err != nil {
		return finishFailure(err)
	}
	defer os.RemoveAll(tempRoot)

	type rollbackFile struct {
		active FileRecord
		target string
		prior  string
		undo   string
	}
	files := make([]rollbackFile, 0, len(active.Files))
	for _, record := range active.Files {
		target, ok := targetForRole[record.Role]
		if !ok {
			return finishFailure(fmt.Errorf("unknown receipt role %q", record.Role))
		}
		currentHash, hashErr := fileSHA256(target)
		if hashErr != nil || currentHash != record.NewSHA256 {
			return finishFailure(fmt.Errorf("active %s checksum differs from receipt; partial or subsequent activation is present", record.Role))
		}
		undo := filepath.Join(tempRoot, record.Role+"-activated")
		if err := copyFile(target, undo, 0o500); err != nil {
			return finishFailure(fmt.Errorf("staging rollback undo for %s: %w", record.Role, err))
		}
		prior := ""
		if record.Existed {
			prior, err = ensureUnderStateDir(opts.StateDir, record.BackupFile)
			if err != nil {
				return finishFailure(err)
			}
			priorHash, hashErr := fileSHA256(prior)
			if hashErr != nil || priorHash != record.OldSHA256 {
				return finishFailure(fmt.Errorf("prior %s backup checksum differs from receipt", record.Role))
			}
		}
		files = append(files, rollbackFile{active: record, target: target, prior: prior, undo: undo})
		receipt.Files = append(receipt.Files, FileRecord{
			Role: record.Role, Path: redactPath(target), Existed: true,
			OldSHA256: record.NewSHA256, NewSHA256: record.OldSHA256,
		})
	}

	currentSnapshot, err := opts.Components.Snapshot(ctx)
	if err != nil {
		return finishFailure(fmt.Errorf("capturing component set: %w", err))
	}
	// Restore the component set that existed before activation, not components
	// started later. Current process details supply restart args/env only.
	desiredSnapshot := currentSnapshot
	for _, component := range active.Components {
		switch component.Name {
		case "daemon":
			desiredSnapshot.DaemonRunning = component.WasRunning
		case "dashboard":
			if !component.WasRunning {
				desiredSnapshot.Dashboard = nil
			}
		}
	}
	restored := 0
	undoRollback := func(cause error) (*Receipt, error) {
		var undoErrs []error
		for i := restored - 1; i >= 0; i-- {
			if restoreErr := atomicInstall(files[i].undo, files[i].target); restoreErr != nil {
				undoErrs = append(undoErrs, restoreErr)
			}
		}
		_, refreshErr := opts.Components.Refresh(ctx, currentSnapshot, opts.InstallPath, opts.DashboardPath)
		return finishFailure(errors.Join(cause, errors.Join(undoErrs...), refreshErr))
	}

	for i := range files {
		if files[i].active.Existed {
			err = atomicInstall(files[i].prior, files[i].target)
		} else {
			err = os.Remove(files[i].target)
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		}
		if err != nil {
			return undoRollback(fmt.Errorf("restoring prior %s: %w", files[i].active.Role, err))
		}
		restored++
	}

	componentResults, err := opts.Components.Refresh(ctx, desiredSnapshot, opts.InstallPath, opts.DashboardPath)
	receipt.Components = componentResults
	if err != nil {
		return undoRollback(fmt.Errorf("refreshing rolled-back components: %w", err))
	}
	for i := range files {
		if files[i].active.Existed {
			got, hashErr := fileSHA256(files[i].target)
			if hashErr != nil || got != files[i].active.OldSHA256 {
				return undoRollback(fmt.Errorf("rolled-back %s checksum mismatch", files[i].active.Role))
			}
			receipt.Files[i].Installed = true
			receipt.Files[i].Verified = true
		} else if _, statErr := os.Stat(files[i].target); !errors.Is(statErr, os.ErrNotExist) {
			return undoRollback(fmt.Errorf("rolled-back %s should be absent", files[i].active.Role))
		}
	}
	if opts.VerifyPATH {
		if err := verifyCommandPath(opts.InstallPath); err != nil {
			return undoRollback(err)
		}
	}

	receipt.Result = "success"
	receipt.Verification = "prior binary checksums restored; prior running components restarted and verified"
	receipt.CompletedAt = time.Now().UTC()
	if err := writeNamedReceipt(opts.StateDir, receipt, true); err != nil {
		return undoRollback(fmt.Errorf("writing rollback receipt: %w", err))
	}
	return receipt, nil
}
