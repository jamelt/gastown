package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseLastTranscriptContextUsage(t *testing.T) {
	transcript := filepath.Join(t.TempDir(), "session.jsonl")
	contents := "" +
		`{"type":"assistant","message":{"model":"claude-sonnet","usage":{"input_tokens":1000,"cache_creation_input_tokens":2000,"cache_read_input_tokens":3000}}}` + "\n" +
		`{"type":"human","message":{"content":"next"}}` + "\n" +
		`{"type":"assistant","message":{"model":"claude-opus","usage":{"input_tokens":8000,"cache_creation_input_tokens":50000,"cache_read_input_tokens":120000}}}` + "\n"
	if err := os.WriteFile(transcript, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}

	tokens, model, err := parseLastTranscriptContextUsage(transcript)
	if err != nil {
		t.Fatalf("parseLastTranscriptContextUsage() error: %v", err)
	}
	if tokens != 178000 {
		t.Fatalf("tokens = %d, want 178000", tokens)
	}
	if model != "claude-opus" {
		t.Fatalf("model = %q, want claude-opus", model)
	}
}

func TestMakeContextUsageOutputRecommendsHandoff(t *testing.T) {
	output := makeContextUsageOutput(160000, 200000, 0.80, "test", "", "")
	if output.UsagePercent != 80 {
		t.Fatalf("UsagePercent = %d, want 80", output.UsagePercent)
	}
	if !output.HandoffRecommended {
		t.Fatal("HandoffRecommended = false, want true at threshold")
	}
}

func TestContextCommandCompatibilityFlags(t *testing.T) {
	if contextCmd.Flags().Lookup("usage") == nil {
		t.Fatal("gt context is missing --usage")
	}
	if contextCmd.Flags().Lookup("json") == nil {
		t.Fatal("gt context is missing --json")
	}
}
