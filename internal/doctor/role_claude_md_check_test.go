package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoleClaudeMdCheck_NoFiles(t *testing.T) {
	tmpDir := t.TempDir()
	townRoot := tmpDir
	rigName := "gastown"
	rigPath := filepath.Join(townRoot, rigName)

	// Create minimal rig structure without CLAUDE.md/AGENTS.md files
	for _, roleName := range []string{"mayor", "refinery", "witness", "crew", "polecats"} {
		rolePath := filepath.Join(rigPath, roleName)
		if err := os.MkdirAll(rolePath, 0755); err != nil {
			t.Fatal(err)
		}
	}

	ctx := &CheckContext{TownRoot: townRoot, RigName: rigName}
	check := NewRoleClaudeMdCheck()
	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK when no files exist, got %v: %s", result.Status, result.Message)
	}
}

func TestRoleClaudeMdCheck_CorrectPointer(t *testing.T) {
	tmpDir := t.TempDir()
	townRoot := tmpDir
	rigName := "gastown"
	rigPath := filepath.Join(townRoot, rigName)

	// Create role directories with correct @AGENTS.md pointers
	for _, roleName := range []string{"mayor", "refinery", "witness"} {
		rolePath := filepath.Join(rigPath, roleName)
		if err := os.MkdirAll(rolePath, 0755); err != nil {
			t.Fatal(err)
		}
		claudePath := filepath.Join(rolePath, "CLAUDE.md")
		if err := os.WriteFile(claudePath, []byte("@AGENTS.md\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	ctx := &CheckContext{TownRoot: townRoot, RigName: rigName}
	check := NewRoleClaudeMdCheck()
	result := check.Run(ctx)

	if result.Status != StatusOK {
		t.Errorf("expected StatusOK for correct pointers, got %v: %s", result.Status, result.Message)
	}
}

func TestRoleClaudeMdCheck_FullContent(t *testing.T) {
	tmpDir := t.TempDir()
	townRoot := tmpDir
	rigName := "gastown"
	rigPath := filepath.Join(townRoot, rigName)

	// Create a role with a full CLAUDE.md (not a pointer)
	majorRolePath := filepath.Join(rigPath, "mayor")
	if err := os.MkdirAll(majorRolePath, 0755); err != nil {
		t.Fatal(err)
	}
	claudePath := filepath.Join(majorRolePath, "CLAUDE.md")
	content := `# Mayor Role Context

This is full role-specific context that should not be here.

## Some Section

This would be "Conservative-profile" content injected by bd init.
`
	if err := os.WriteFile(claudePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := &CheckContext{TownRoot: townRoot, RigName: rigName}
	check := NewRoleClaudeMdCheck()
	result := check.Run(ctx)

	if result.Status != StatusWarning {
		t.Errorf("expected StatusWarning for full content, got %v", result.Status)
	}
	if len(check.issues) == 0 {
		t.Error("expected at least one issue found")
	}
}

func TestRoleClaudeMdCheck_UnwantedAgentsFile(t *testing.T) {
	tmpDir := t.TempDir()
	townRoot := tmpDir
	rigName := "gastown"
	rigPath := filepath.Join(townRoot, rigName)

	// Create a role with AGENTS.md (which shouldn't exist)
	refinerRolePath := filepath.Join(rigPath, "refinery")
	if err := os.MkdirAll(refinerRolePath, 0755); err != nil {
		t.Fatal(err)
	}
	agentsPath := filepath.Join(refinerRolePath, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("# Refinery AGENTS\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := &CheckContext{TownRoot: townRoot, RigName: rigName}
	check := NewRoleClaudeMdCheck()
	result := check.Run(ctx)

	if result.Status != StatusWarning {
		t.Errorf("expected StatusWarning for unwanted AGENTS.md, got %v", result.Status)
	}
	if len(check.issues) == 0 {
		t.Error("expected at least one issue found")
	}
}

func TestRoleClaudeMdCheck_Fix(t *testing.T) {
	tmpDir := t.TempDir()
	townRoot := tmpDir
	rigName := "gastown"
	rigPath := filepath.Join(townRoot, rigName)

	// Create a role with both unwanted files
	majorRolePath := filepath.Join(rigPath, "mayor")
	if err := os.MkdirAll(majorRolePath, 0755); err != nil {
		t.Fatal(err)
	}

	claudePath := filepath.Join(majorRolePath, "CLAUDE.md")
	if err := os.WriteFile(claudePath, []byte("Full content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	agentsPath := filepath.Join(majorRolePath, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("Agents content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := &CheckContext{TownRoot: townRoot, RigName: rigName}
	check := NewRoleClaudeMdCheck()

	// Run and verify issues are found
	result := check.Run(ctx)
	if result.Status != StatusWarning {
		t.Errorf("expected StatusWarning before fix, got %v", result.Status)
	}

	// Apply fix
	if err := check.Fix(ctx); err != nil {
		t.Fatalf("Fix failed: %v", err)
	}

	// Verify files are removed
	if _, err := os.Stat(claudePath); err == nil {
		t.Error("CLAUDE.md should be removed after fix")
	}
	if _, err := os.Stat(agentsPath); err == nil {
		t.Error("AGENTS.md should be removed after fix")
	}

	// Run check again - should pass now
	result = check.Run(ctx)
	if result.Status != StatusOK {
		t.Errorf("expected StatusOK after fix, got %v", result.Status)
	}
}

func TestRoleClaudeMdCheck_PreservesCorrectPointer(t *testing.T) {
	tmpDir := t.TempDir()
	townRoot := tmpDir
	rigName := "gastown"
	rigPath := filepath.Join(townRoot, rigName)

	// Create a role with correct pointer and unwanted AGENTS.md
	majorRolePath := filepath.Join(rigPath, "mayor")
	if err := os.MkdirAll(majorRolePath, 0755); err != nil {
		t.Fatal(err)
	}

	claudePath := filepath.Join(majorRolePath, "CLAUDE.md")
	if err := os.WriteFile(claudePath, []byte("@AGENTS.md\n"), 0644); err != nil {
		t.Fatal(err)
	}

	agentsPath := filepath.Join(majorRolePath, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("Agents content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := &CheckContext{TownRoot: townRoot, RigName: rigName}
	check := NewRoleClaudeMdCheck()

	// Apply fix
	if err := check.Fix(ctx); err != nil {
		t.Fatalf("Fix failed: %v", err)
	}

	// CLAUDE.md pointer should be preserved
	if data, err := os.ReadFile(claudePath); err != nil {
		t.Error("CLAUDE.md pointer should be preserved")
	} else if string(data) != "@AGENTS.md\n" {
		t.Errorf("CLAUDE.md pointer changed, got: %q", string(data))
	}

	// AGENTS.md should be removed
	if _, err := os.Stat(agentsPath); err == nil {
		t.Error("AGENTS.md should be removed even if CLAUDE.md pointer is correct")
	}
}
