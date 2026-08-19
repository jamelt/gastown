package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveVerifiedSender(t *testing.T) {
	cases := []struct {
		name       string
		claimed    string
		verified   string
		verifiedOK bool
		want       string
		wantErrHas string
	}{
		{"verification unavailable trusts claim", "overseer", "", false, "overseer", ""},
		{"claim matches verified session", "gastown/polecats/toast", "gastown/polecats/toast", true, "gastown/polecats/toast", ""},
		{"case/slash-insensitive match", "mayor/", "mayor", true, "mayor/", ""},
		{"convoy synthetic actor allowed despite mismatch", "convoy/gt-1234", "gastown/refinery", true, "convoy/gt-1234", ""},
		{"forged GT_ROLE rejected", "overseer", "gastown/polecats/toast", true, "", "gt-9z0y"},
		{"forged mayor claim rejected", "mayor/", "gastown/polecats/toast", true, "", "unverified identity"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveVerifiedSender(c.claimed, c.verified, c.verifiedOK)
			if c.wantErrHas != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErrHas) {
					t.Fatalf("resolveVerifiedSender(%q, %q, %v) error = %v, want error containing %q", c.claimed, c.verified, c.verifiedOK, err, c.wantErrHas)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveVerifiedSender(%q, %q, %v) unexpected error: %v", c.claimed, c.verified, c.verifiedOK, err)
			}
			if got != c.want {
				t.Errorf("resolveVerifiedSender(%q, %q, %v) = %q, want %q", c.claimed, c.verified, c.verifiedOK, got, c.want)
			}
		})
	}
}

func TestDetectSenderVerifiedTrustsClaimWhenGTRoleUnset(t *testing.T) {
	t.Setenv("GT_ROLE", "")
	t.Setenv("GT_RIG", "")
	t.Setenv("GT_POLECAT", "")
	t.Setenv("GT_CREW", "")

	tmp := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	got, err := detectSenderVerified()
	if err != nil {
		t.Fatalf("detectSenderVerified() unexpected error: %v", err)
	}
	if got != "overseer" {
		t.Fatalf("detectSenderVerified() = %q, want %q", got, "overseer")
	}
}

func TestDetectSenderFromCwdUsesAgentFileWitnessIdentity(t *testing.T) {
	t.Setenv("GT_ROLE", "")
	t.Setenv("GT_RIG", "")
	t.Setenv("GT_POLECAT", "")
	t.Setenv("GT_CREW", "")

	tmp := t.TempDir()
	witnessDir := filepath.Join(tmp, "x267", "witness")
	if err := os.MkdirAll(filepath.Join(witnessDir, "rig"), 0o755); err != nil {
		t.Fatalf("mkdir witness dir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(witnessDir, ".gt-agent"),
		[]byte(`{"role":"witness","rig":"x267"}`),
		0o644,
	); err != nil {
		t.Fatalf("write .gt-agent: %v", err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()
	if err := os.Chdir(filepath.Join(witnessDir, "rig")); err != nil {
		t.Fatalf("chdir witness rig dir: %v", err)
	}

	got := detectSender()
	if got != "x267/witness" {
		t.Fatalf("detectSender() = %q, want %q", got, "x267/witness")
	}
}

func TestDetectSenderFromCwdUsesAgentFileRefineryIdentity(t *testing.T) {
	t.Setenv("GT_ROLE", "")
	t.Setenv("GT_RIG", "")
	t.Setenv("GT_POLECAT", "")
	t.Setenv("GT_CREW", "")

	tmp := t.TempDir()
	refineryDir := filepath.Join(tmp, "x267", "refinery")
	if err := os.MkdirAll(filepath.Join(refineryDir, "rig"), 0o755); err != nil {
		t.Fatalf("mkdir refinery dir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(refineryDir, ".gt-agent"),
		[]byte(`{"role":"refinery","rig":"x267"}`),
		0o644,
	); err != nil {
		t.Fatalf("write .gt-agent: %v", err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()
	if err := os.Chdir(filepath.Join(refineryDir, "rig")); err != nil {
		t.Fatalf("chdir refinery rig dir: %v", err)
	}

	got := detectSender()
	if got != "x267/refinery" {
		t.Fatalf("detectSender() = %q, want %q", got, "x267/refinery")
	}
}
