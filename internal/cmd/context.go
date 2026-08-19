package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"
)

const (
	defaultContextMaxTokens        = 200000
	defaultContextHandoffThreshold = 0.80
)

var (
	contextShowUsage bool
	contextJSON      bool
)

var contextCmd = &cobra.Command{
	Use:     "context",
	GroupID: GroupDiag,
	Short:   "Show the current agent context budget",
	Long: `Inspect the current Claude Code session's context-window usage.

The token count uses the latest assistant message in the active transcript and
includes input, cache-creation, and cache-read tokens. Set
GT_CONTEXT_BUDGET_MAX_TOKENS to override the default 200000-token window and
GT_CONTEXT_HANDOFF_THRESHOLD to override the default 80% handoff threshold.`,
	Example: `  gt context --usage
  gt context --usage --json`,
	Args: cobra.NoArgs,
	RunE: runContext,
}

type contextUsageOutput struct {
	Tokens             int     `json:"tokens"`
	MaxTokens          int     `json:"max_tokens"`
	UsageRatio         float64 `json:"usage_ratio"`
	UsagePercent       int     `json:"usage_percent"`
	HandoffThreshold   float64 `json:"handoff_threshold"`
	HandoffRecommended bool    `json:"handoff_recommended"`
	Source             string  `json:"source"`
	Transcript         string  `json:"transcript,omitempty"`
	Model              string  `json:"model,omitempty"`
}

func init() {
	contextCmd.Flags().BoolVar(&contextShowUsage, "usage", false, "Show current context-window usage")
	contextCmd.Flags().BoolVar(&contextJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(contextCmd)
}

func runContext(cmd *cobra.Command, args []string) error {
	if !contextShowUsage {
		return fmt.Errorf("specify --usage to inspect the current context budget")
	}

	usage, err := currentContextUsage()
	if err != nil {
		return err
	}

	if contextJSON {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(usage)
	}

	recommendation := "no"
	if usage.HandoffRecommended {
		recommendation = "yes"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Context usage: %d%% (%d/%d tokens)\n", usage.UsagePercent, usage.Tokens, usage.MaxTokens)
	fmt.Fprintf(cmd.OutOrStdout(), "Handoff recommended: %s (threshold: %.0f%%)\n", recommendation, usage.HandoffThreshold*100)
	return nil
}

func currentContextUsage() (contextUsageOutput, error) {
	maxTokens, err := positiveEnvInt("GT_CONTEXT_BUDGET_MAX_TOKENS", defaultContextMaxTokens)
	if err != nil {
		return contextUsageOutput{}, err
	}
	handoffThreshold, err := ratioEnvFloat("GT_CONTEXT_HANDOFF_THRESHOLD", defaultContextHandoffThreshold)
	if err != nil {
		return contextUsageOutput{}, err
	}

	if rawTokens := os.Getenv("GT_CONTEXT_BUDGET_TOKENS"); rawTokens != "" {
		tokens, err := strconv.Atoi(rawTokens)
		if err != nil || tokens < 0 {
			return contextUsageOutput{}, fmt.Errorf("GT_CONTEXT_BUDGET_TOKENS must be a non-negative integer, got %q", rawTokens)
		}
		return makeContextUsageOutput(tokens, maxTokens, handoffThreshold, "environment", "", ""), nil
	}

	transcriptPath, err := currentContextTranscript()
	if err != nil {
		return contextUsageOutput{}, err
	}
	tokens, model, err := parseLastTranscriptContextUsage(transcriptPath)
	if err != nil {
		return contextUsageOutput{}, fmt.Errorf("reading context usage from transcript: %w", err)
	}
	return makeContextUsageOutput(tokens, maxTokens, handoffThreshold, "claude_transcript", transcriptPath, model), nil
}

func currentContextTranscript() (string, error) {
	if path := os.Getenv("GT_CONTEXT_TRANSCRIPT"); path != "" {
		return path, nil
	}

	workDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting current working directory: %w", err)
	}
	projectDir, err := getClaudeProjectDir(workDir)
	if err != nil {
		return "", fmt.Errorf("locating Claude project directory: %w", err)
	}

	for _, sessionID := range []string{os.Getenv("GT_SESSION_ID"), os.Getenv("CLAUDE_SESSION_ID"), ReadPersistedSessionID()} {
		if sessionID == "" {
			continue
		}
		candidate := filepath.Join(projectDir, sessionID+".jsonl")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	path, err := findLatestTranscript(projectDir)
	if err != nil {
		return "", fmt.Errorf("locating active Claude transcript: %w", err)
	}
	return path, nil
}

func parseLastTranscriptContextUsage(transcriptPath string) (int, string, error) {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return 0, "", err
	}
	defer file.Close()

	found := false
	tokens := 0
	model := ""
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
	for scanner.Scan() {
		var message TranscriptMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil || message.Type != "assistant" || message.Message == nil || message.Message.Usage == nil {
			continue
		}
		usage := message.Message.Usage
		tokens = usage.InputTokens + usage.CacheCreationInputTokens + usage.CacheReadInputTokens
		model = message.Message.Model
		found = true
	}
	if err := scanner.Err(); err != nil {
		return 0, "", err
	}
	if !found {
		return 0, "", fmt.Errorf("no assistant usage record found in %s", transcriptPath)
	}
	return tokens, model, nil
}

func makeContextUsageOutput(tokens, maxTokens int, threshold float64, source, transcript, model string) contextUsageOutput {
	ratio := float64(tokens) / float64(maxTokens)
	return contextUsageOutput{
		Tokens:             tokens,
		MaxTokens:          maxTokens,
		UsageRatio:         ratio,
		UsagePercent:       int(ratio * 100),
		HandoffThreshold:   threshold,
		HandoffRecommended: ratio >= threshold,
		Source:             source,
		Transcript:         transcript,
		Model:              model,
	}
}

func positiveEnvInt(name string, fallback int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", name, raw)
	}
	return value, nil
}

func ratioEnvFloat(name string, fallback float64) (float64, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 || value > 1 {
		return 0, fmt.Errorf("%s must be in the range (0, 1], got %q", name, raw)
	}
	return value, nil
}
