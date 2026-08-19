package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/activation"
	"github.com/steveyegge/gastown/internal/workspace"
)

var (
	activateRepo         string
	activateTown         string
	activateBinary       string
	activateDashboard    string
	activateStateDir     string
	activateSmokeTimeout time.Duration
	activateBuildTimeout time.Duration
)

var activateCmd = &cobra.Command{
	Use:     "activate [exact-main-sha]",
	GroupID: GroupServices,
	Short:   "Build and activate an exact integrated main revision",
	Args:    cobra.MaximumNArgs(1),
	Long: `Build and activate one exact revision from jamelt/gastown origin/main.

The command fetches origin/main, rejects unintegrated revisions, materializes a
clean detached worktree, runs the bounded required smoke gate, and builds gt
once. It atomically installs that same verified binary for the CLI and dashboard,
then gracefully refreshes and verifies only the running daemon and dashboard.
Hooks, queues, Dolt, agent worktrees, and the canonical tmux socket are untouched.

Omit the SHA (or pass "main") to resolve the current exact origin/main SHA.
Every attempt writes a redacted receipt under <town>/.runtime/activation.

Examples:
  gt activate --repo ~/src/gastown 0123456789abcdef0123456789abcdef01234567
  gt activate --repo ~/src/gastown main
  gt activate rollback`,
	RunE: runActivate,
}

var activateRollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Restore the prior known-good activated binary and component set",
	Args:  cobra.NoArgs,
	RunE:  runActivateRollback,
}

func init() {
	activateCmd.AddCommand(activateRollbackCmd)
	activateCmd.PersistentFlags().StringVar(&activateRepo, "repo", "", "Authoritative jamelt/gastown git clone (or GT_ACTIVATION_REPO)")
	activateCmd.PersistentFlags().StringVar(&activateTown, "town", "", "Canonical town root (auto-detected by default)")
	activateCmd.PersistentFlags().StringVar(&activateBinary, "binary", "", "Active gt binary path (current executable by default)")
	activateCmd.PersistentFlags().StringVar(&activateDashboard, "dashboard-binary", "", "Dashboard helper binary path (default ~/.local/libexec/gt-dashboard)")
	activateCmd.PersistentFlags().StringVar(&activateStateDir, "state-dir", "", "Receipt/backup directory (default <town>/.runtime/activation)")
	activateCmd.Flags().DurationVar(&activateSmokeTimeout, "smoke-timeout", 5*time.Minute, "Required smoke gate timeout")
	activateCmd.Flags().DurationVar(&activateBuildTimeout, "build-timeout", 5*time.Minute, "Exact binary build timeout")
	rootCmd.AddCommand(activateCmd)
}

func runActivate(cmd *cobra.Command, args []string) error {
	opts, err := activationOptions(true)
	if err != nil {
		return err
	}
	if len(args) == 1 {
		opts.Revision = args[0]
	}
	fmt.Printf("Activating %s from %s...\n", revisionLabel(opts.Revision), opts.RepoDir)
	receipt, err := activation.Activate(cmd.Context(), opts)
	if receipt != nil && receipt.ReceiptFile != "" {
		fmt.Printf("Receipt: %s\n", receipt.ReceiptFile)
	}
	if err != nil {
		return err
	}
	fmt.Printf("✓ Active revision: %s\n", receipt.NewSHA)
	for _, component := range receipt.Components {
		fmt.Printf("  %s: %s\n", component.Name, component.Detail)
	}
	return nil
}

func runActivateRollback(cmd *cobra.Command, args []string) error {
	opts, err := activationOptions(false)
	if err != nil {
		return err
	}
	fmt.Println("Restoring prior known-good activation...")
	receipt, err := activation.Rollback(cmd.Context(), opts)
	if receipt != nil && receipt.ReceiptFile != "" {
		fmt.Printf("Receipt: %s\n", receipt.ReceiptFile)
	}
	if err != nil {
		return err
	}
	fmt.Printf("✓ Restored revision: %s\n", receipt.NewSHA)
	return nil
}

func activationOptions(requireRepo bool) (activation.Options, error) {
	townRoot := activateTown
	if townRoot == "" {
		var err error
		townRoot, err = workspace.FindFromCwdOrError()
		if err != nil {
			return activation.Options{}, fmt.Errorf("finding canonical town (use --town): %w", err)
		}
	}
	repo := activateRepo
	if repo == "" {
		repo = os.Getenv("GT_ACTIVATION_REPO")
	}
	if repo == "" {
		cwd, err := os.Getwd()
		if err == nil {
			if output, gitErr := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output(); gitErr == nil {
				repo = string(output)
			}
		}
	}
	repo = filepath.Clean(repo)
	if (repo == "." || repo == "") && requireRepo {
		return activation.Options{}, fmt.Errorf("authoritative source clone not found; pass --repo or GT_ACTIVATION_REPO")
	}
	if repo == "." || repo == "" {
		repo = townRoot // Rollback does not consume source; keep options absolute.
	}
	repo, _ = filepath.Abs(repo)
	townRoot, _ = filepath.Abs(townRoot)

	binary := activateBinary
	if binary == "" {
		var err error
		binary, err = os.Executable()
		if err != nil {
			return activation.Options{}, fmt.Errorf("finding active executable: %w", err)
		}
	}
	binary, _ = filepath.Abs(binary)
	dashboard := activateDashboard
	if dashboard == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return activation.Options{}, err
		}
		dashboard = filepath.Join(home, ".local", "libexec", "gt-dashboard")
	}
	dashboard, _ = filepath.Abs(dashboard)
	stateDir := activateStateDir
	if stateDir == "" {
		stateDir = filepath.Join(townRoot, ".runtime", "activation")
	}
	stateDir, _ = filepath.Abs(stateDir)

	return activation.Options{
		RepoDir: repo, InstallPath: binary, DashboardPath: dashboard,
		StateDir: stateDir, TownRoot: townRoot, SmokeTimeout: activateSmokeTimeout,
		BuildTimeout: activateBuildTimeout, VerifyPATH: true,
	}, nil
}

func revisionLabel(revision string) string {
	if revision == "" || revision == "main" {
		return "resolved origin/main"
	}
	return revision
}
