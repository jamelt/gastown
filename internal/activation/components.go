package activation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/steveyegge/gastown/internal/daemon"
	"github.com/steveyegge/gastown/internal/util"
)

type processController struct {
	townRoot      string
	dashboardPath string
}

func (p *processController) Snapshot(ctx context.Context) (ComponentSnapshot, error) {
	running, _, err := daemon.IsRunning(p.townRoot)
	if err != nil {
		return ComponentSnapshot{}, fmt.Errorf("checking daemon: %w", err)
	}
	dashboards, err := findProcessesByExecutable(p.dashboardPath)
	if err != nil {
		return ComponentSnapshot{}, fmt.Errorf("discovering dashboard: %w", err)
	}
	return ComponentSnapshot{DaemonRunning: running, Dashboard: dashboards}, nil
}

func (p *processController) Refresh(ctx context.Context, snapshot ComponentSnapshot, binaryPath, dashboardPath string) ([]ComponentResult, error) {
	results := make([]ComponentResult, 0, 2)
	var errs []error

	daemonResult := ComponentResult{Name: "daemon", WasRunning: snapshot.DaemonRunning}
	daemonReady := true
	currentlyRunning, _, statusErr := daemon.IsRunning(p.townRoot)
	if statusErr != nil {
		daemonReady = false
		daemonResult.Detail = "status check failed"
		errs = append(errs, fmt.Errorf("checking daemon before refresh: %w", statusErr))
	} else if currentlyRunning {
		if err := daemon.StopDaemon(p.townRoot); err != nil {
			daemonReady = false
			daemonResult.Detail = "stop failed"
			errs = append(errs, fmt.Errorf("stopping daemon: %w", err))
		}
	}
	if snapshot.DaemonRunning {
		if daemonReady {
			output, err := runCombined(ctx, p.townRoot, nil, binaryPath, "daemon", "start")
			if err != nil {
				daemonResult.Detail = "start failed"
				errs = append(errs, fmt.Errorf("starting daemon: %w: %s", err, output))
				daemonReady = false
			}
		}
		if daemonReady {
			daemonResult.Restarted = true
			running, pid, err := daemon.IsRunning(p.townRoot)
			if err == nil && running {
				verified, verifyErr := processMatchesFile(pid, binaryPath)
				daemonResult.Verified = verified
				if verifyErr != nil {
					daemonResult.Detail = "running; executable verification failed"
					errs = append(errs, fmt.Errorf("verifying daemon executable: %w", verifyErr))
				} else if !verified {
					daemonResult.Detail = "running stale executable"
					errs = append(errs, errors.New("daemon restarted with a stale executable"))
				} else {
					daemonResult.Detail = "restarted with activated executable"
				}
			} else {
				daemonResult.Detail = "not running after restart"
				errs = append(errs, errors.New("daemon is not running after restart"))
			}
		}
	} else {
		daemonResult.Verified = daemonReady
		if daemonReady {
			daemonResult.Detail = "preserved stopped state"
		}
	}
	results = append(results, daemonResult)

	dashboardResult := ComponentResult{Name: "dashboard", WasRunning: len(snapshot.Dashboard) > 0}
	dashboardReady := true
	currentDashboards, discoverErr := findProcessesByExecutable(p.dashboardPath)
	if discoverErr != nil {
		dashboardReady = false
		errs = append(errs, fmt.Errorf("rediscovering dashboard: %w", discoverErr))
	}
	for _, proc := range currentDashboards {
		if err := stopProcess(ctx, proc.PID); err != nil {
			dashboardReady = false
			errs = append(errs, fmt.Errorf("stopping dashboard PID %d: %w", proc.PID, err))
		}
	}
	if len(snapshot.Dashboard) == 0 {
		dashboardResult.Verified = dashboardReady
		if dashboardReady {
			dashboardResult.Detail = "preserved stopped state"
		} else {
			dashboardResult.Detail = "failed to restore stopped state"
		}
		results = append(results, dashboardResult)
		return results, errors.Join(errs...)
	}

	if dashboardReady {
		allVerified := true
		for _, proc := range snapshot.Dashboard {
			env := proc.Env
			if len(env) == 0 {
				env = os.Environ()
			}
			cmd := exec.CommandContext(ctx, dashboardPath, proc.Args...)
			cmd.Dir = p.townRoot
			cmd.Env = env
			cmd.Stdin = nil
			cmd.Stdout = nil
			cmd.Stderr = nil
			util.SetDetachedProcessGroup(cmd)
			if err := cmd.Start(); err != nil {
				allVerified = false
				errs = append(errs, fmt.Errorf("starting dashboard: %w", err))
				continue
			}
			time.Sleep(250 * time.Millisecond)
			verified, err := processMatchesFile(cmd.Process.Pid, dashboardPath)
			if err != nil || !verified {
				allVerified = false
				if err == nil {
					err = errors.New("process executable does not match activated dashboard")
				}
				errs = append(errs, fmt.Errorf("verifying dashboard PID %d: %w", cmd.Process.Pid, err))
			}
		}
		dashboardResult.Restarted = true
		dashboardResult.Verified = allVerified
		if allVerified {
			dashboardResult.Detail = "restarted with activated executable"
		} else {
			dashboardResult.Detail = "restart or executable verification failed"
		}
	} else {
		dashboardResult.Detail = "failed to stop prior dashboard process set"
	}
	results = append(results, dashboardResult)
	return results, errors.Join(errs...)
}

func runCombined(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

func findProcessesByExecutable(path string) ([]DashboardProcess, error) {
	if path == "" {
		return nil, nil
	}
	want, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS == "linux" {
		entries, err := os.ReadDir("/proc")
		if err == nil {
			var found []DashboardProcess
			for _, entry := range entries {
				pid, convErr := strconv.Atoi(entry.Name())
				if convErr != nil {
					continue
				}
				exePath, readErr := os.Readlink(filepath.Join("/proc", entry.Name(), "exe"))
				if readErr != nil || strings.TrimSuffix(exePath, " (deleted)") != want {
					continue
				}
				cmdline, readErr := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
				if readErr != nil {
					continue
				}
				parts := bytes.Split(bytes.TrimRight(cmdline, "\x00"), []byte{0})
				args := make([]string, 0, len(parts)-1)
				for _, part := range parts[1:] {
					args = append(args, string(part))
				}
				environ, _ := os.ReadFile(filepath.Join("/proc", entry.Name(), "environ"))
				envParts := bytes.Split(bytes.TrimRight(environ, "\x00"), []byte{0})
				env := make([]string, 0, len(envParts))
				for _, part := range envParts {
					if len(part) > 0 {
						env = append(env, string(part))
					}
				}
				found = append(found, DashboardProcess{PID: pid, Args: args, Env: env})
			}
			return found, nil
		}
	}
	// Portable fallback: only accept rows whose first whitespace-delimited token
	// exactly matches the configured dashboard path.
	output, err := exec.Command("ps", "-ax", "-o", "pid=", "-o", "command=").Output()
	if err != nil {
		return nil, err
	}
	var found []DashboardProcess
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[1] != want {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		found = append(found, DashboardProcess{PID: pid, Args: fields[2:], Env: os.Environ()})
	}
	return found, nil
}

func stopProcess(ctx context.Context, pid int) error {
	if pid <= 1 {
		return fmt.Errorf("refusing invalid PID %d", pid)
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		if err := process.Signal(syscall.Signal(0)); err != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return errors.New("process did not stop within 5s")
		case <-ticker.C:
		}
	}
}

func processMatchesFile(pid int, path string) (bool, error) {
	want, err := fileSHA256(path)
	if err != nil {
		return false, err
	}
	if runtime.GOOS == "linux" {
		got, err := fileSHA256(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
		if err != nil {
			return false, err
		}
		return got == want, nil
	}
	// On non-Linux systems, verify the running path then compare the installed
	// file. The process was spawned only after the atomic swap completed.
	output, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		return false, err
	}
	fields := strings.Fields(string(output))
	if len(fields) == 0 {
		return false, errors.New("empty process command")
	}
	gotPath, err := filepath.Abs(fields[0])
	if err != nil {
		return false, err
	}
	wantPath, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	return gotPath == wantPath, nil
}
