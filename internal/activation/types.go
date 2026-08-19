// Package activation implements the verified post-merge runtime activation lane.
package activation

import (
	"context"
	"time"

	"github.com/steveyegge/gastown/internal/util"
)

const DefaultAuthority = util.DefaultGtAuthority

// Options controls an activation. Paths are explicit so tests can use a fully
// isolated runtime without touching the operator's town, tmux, or Dolt state.
type Options struct {
	RepoDir       string
	Revision      string
	Remote        string
	Authority     string
	MainRef       string
	InstallPath   string
	DashboardPath string
	StateDir      string
	TownRoot      string
	SmokeTimeout  time.Duration
	BuildTimeout  time.Duration
	VerifyPATH    bool

	// Test hooks. Production callers leave these nil.
	Build      BuildFunc
	Smoke      SmokeFunc
	Components ComponentController
}

type BuildFunc func(ctx context.Context, sourceDir, outputPath, sha, version string) error
type SmokeFunc func(ctx context.Context, sourceDir string) error

// ComponentController refreshes only long-lived processes which retain the old
// executable inode. Shell-based agents intentionally are not part of this set.
type ComponentController interface {
	Snapshot(context.Context) (ComponentSnapshot, error)
	Refresh(context.Context, ComponentSnapshot, string, string) ([]ComponentResult, error)
}

type ComponentSnapshot struct {
	DaemonRunning bool               `json:"daemon_running"`
	Dashboard     []DashboardProcess `json:"dashboard,omitempty"`
}

type DashboardProcess struct {
	PID  int      `json:"-"`
	Args []string `json:"args,omitempty"`
	// Env is held only in memory to preserve the process environment. It is
	// deliberately excluded from receipts because it may contain credentials.
	Env []string `json:"-"`
}

type ComponentResult struct {
	Name       string `json:"name"`
	WasRunning bool   `json:"was_running"`
	Restarted  bool   `json:"restarted"`
	Verified   bool   `json:"verified"`
	Detail     string `json:"detail,omitempty"`
}

type FileRecord struct {
	Role       string `json:"role"`
	Path       string `json:"path"`
	Existed    bool   `json:"existed"`
	OldSHA256  string `json:"old_sha256,omitempty"`
	NewSHA256  string `json:"new_sha256"`
	BackupFile string `json:"backup_file,omitempty"`
	Installed  bool   `json:"installed"`
	Verified   bool   `json:"verified"`
}

type Receipt struct {
	Schema        int               `json:"schema"`
	Action        string            `json:"action"`
	Result        string            `json:"result"`
	ActivatedAt   time.Time         `json:"activated_at"`
	CompletedAt   time.Time         `json:"completed_at"`
	OldSHA        string            `json:"old_sha,omitempty"`
	NewSHA        string            `json:"new_sha"`
	SourceRemote  string            `json:"source_remote"`
	SourceMainRef string            `json:"source_main_ref"`
	SourceClean   bool              `json:"source_clean"`
	SmokeGate     string            `json:"smoke_gate"`
	Files         []FileRecord      `json:"files"`
	Components    []ComponentResult `json:"components"`
	Verification  string            `json:"verification"`
	Failure       string            `json:"failure,omitempty"`
	ReceiptFile   string            `json:"-"`
}
