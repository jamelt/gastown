package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/style"
)

var (
	prSheriffCheckBase      string
	prSheriffCheckMergeGate bool
	prSheriffCheckJSON      bool
)

var prSheriffCheckCmd = &cobra.Command{
	Use:   "pr-sheriff-check",
	Short: "Check branch divergence before opening or recommending a maintainer replacement PR",
	Long: `Computes how far the current branch has diverged from its base (commits
behind and commits ahead) and reports whether it is safe to open or
recommend a PR Sheriff maintainer replacement PR.

A branch that is far behind its base, or carries far more commits ahead
than a small replacement/fixup should, risks pulling unrelated changes
into the replacement PR's diff -- exactly what happened with PR #4238
(~553 behind / ~86 ahead) and PR #4257 (~553 behind / ~98 ahead).

Run this before creating or recommending a PR Sheriff maintainer
replacement PR:

  gt pr-sheriff-check --merge-gate

Without --merge-gate, this prints a report and always exits 0. With
--merge-gate, it exits non-zero when the branch is contaminated past the
block threshold.`,
	RunE: runPRSheriffCheck,
}

func init() {
	rootCmd.AddCommand(prSheriffCheckCmd)
	prSheriffCheckCmd.Flags().StringVar(&prSheriffCheckBase, "base", "", "Base ref to compare against (default: detected default branch, fork-aware)")
	prSheriffCheckCmd.Flags().BoolVar(&prSheriffCheckMergeGate, "merge-gate", false, "Exit non-zero when contamination hits the block threshold")
	prSheriffCheckCmd.Flags().BoolVar(&prSheriffCheckJSON, "json", false, "Output machine-readable JSON")
}

type prSheriffCheckResult struct {
	Base             string   `json:"base"`
	Ahead            int      `json:"ahead"`
	Behind           int      `json:"behind"`
	Severity         string   `json:"severity"` // clean, warn, block
	Reasons          []string `json:"reasons"`
	MergeGate        bool     `json:"merge_gate"`
	MergePathAllowed bool     `json:"merge_path_allowed"`
}

func runPRSheriffCheck(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving working directory: %w", err)
	}
	g := git.NewGit(cwd)

	base, contam, fetchErr, checkErr := g.ResolveContaminationCheck(prSheriffCheckBase)
	if fetchErr != nil {
		style.PrintWarning("could not fetch before branch hygiene check: %v (proceeding with local refs)", fetchErr)
	}
	if checkErr != nil {
		return fmt.Errorf("checking branch contamination against %s: %w", base, checkErr)
	}

	severity, reasons := contam.Evaluate()
	result := prSheriffCheckResult{
		Base:             base,
		Ahead:            contam.Ahead,
		Behind:           contam.Behind,
		Severity:         severityLabel(severity),
		Reasons:          reasons,
		MergeGate:        prSheriffCheckMergeGate,
		MergePathAllowed: !(prSheriffCheckMergeGate && severity == git.SeverityBlock),
	}

	if prSheriffCheckJSON {
		data, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return marshalErr
		}
		fmt.Println(string(data))
	} else {
		printPRSheriffCheckReport(result)
	}

	if prSheriffCheckMergeGate && severity == git.SeverityBlock {
		return fmt.Errorf("branch hygiene check blocked: %s", strings.Join(reasons, "; "))
	}
	return nil
}

func severityLabel(s git.ContaminationSeverity) string {
	switch s {
	case git.SeverityBlock:
		return "block"
	case git.SeverityWarn:
		return "warn"
	default:
		return "clean"
	}
}

func printPRSheriffCheckReport(result prSheriffCheckResult) {
	fmt.Printf("Base: %s\n", result.Base)
	fmt.Printf("Ahead: %d  Behind: %d\n", result.Ahead, result.Behind)
	switch result.Severity {
	case "block":
		fmt.Printf("%s PR Sheriff branch hygiene: BLOCK\n", style.Error.Render("✗"))
	case "warn":
		fmt.Printf("%s PR Sheriff branch hygiene: WARN\n", style.Warning.Render("⚠"))
	default:
		fmt.Printf("%s PR Sheriff branch hygiene: PASS\n", style.Bold.Render("✓"))
	}
	for _, reason := range result.Reasons {
		fmt.Printf("  - %s\n", reason)
	}
	fmt.Printf("merge_path_allowed: %v\n", result.MergePathAllowed)
}
