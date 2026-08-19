package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RoleClaudeMdCheck verifies that role-specific CLAUDE.md and AGENTS.md files
// are either absent or exactly point to @AGENTS.md (the shared pointer).
//
// Background: CLAUDE.md and AGENTS.md files should not be created in role
// directories (mayor/, refinery/, witness/, crew/, polecats/). Role-specific
// context is injected ephemerally by `gt prime` at session start, not from
// on-disk files. If bd init or other tools create these files with full
// "Conservative-profile" blocks, they pollute the source repo and can
// contradict AGENTS.md policy.
type RoleClaudeMdCheck struct {
	FixableCheck
	issues []string // Issues found (paths and descriptions)
}

// NewRoleClaudeMdCheck creates a new role CLAUDE.md guard check.
func NewRoleClaudeMdCheck() *RoleClaudeMdCheck {
	return &RoleClaudeMdCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "role-claude-md-guard",
				CheckDescription: "Verify role CLAUDE.md files are absent or exactly @AGENTS.md",
				CheckCategory:    CategoryConfig,
			},
		},
	}
}

// Run checks all role directories for unwanted CLAUDE.md/AGENTS.md files.
func (c *RoleClaudeMdCheck) Run(ctx *CheckContext) *CheckResult {
	c.issues = nil

	// Get the rig to check (default to gastown if not specified)
	rigName := ctx.RigName
	if rigName == "" {
		rigName = "gastown"
	}
	rigPath := filepath.Join(ctx.TownRoot, rigName)

	// Verify rig exists
	if _, err := os.Stat(rigPath); os.IsNotExist(err) {
		return &CheckResult{
			Name:     c.Name(),
			Status:   StatusOK,
			Message:  fmt.Sprintf("Rig %q does not exist", rigName),
			Category: c.Category(),
		}
	}

	// List of role directories to check
	roleNames := []string{"mayor", "refinery", "witness", "crew", "polecats"}

	for _, roleName := range roleNames {
		rolePath := filepath.Join(rigPath, roleName)

		// Check CLAUDE.md
		claudePath := filepath.Join(rolePath, "CLAUDE.md")
		if err := c.checkClaudeFile(claudePath, roleName); err != nil {
			c.issues = append(c.issues, err.Error())
		}

		// Check AGENTS.md
		agentsPath := filepath.Join(rolePath, "AGENTS.md")
		if err := c.checkAgentsFile(agentsPath, roleName); err != nil {
			c.issues = append(c.issues, err.Error())
		}
	}

	if len(c.issues) == 0 {
		return &CheckResult{
			Name:     c.Name(),
			Status:   StatusOK,
			Message:  "No unwanted role CLAUDE.md/AGENTS.md files found",
			Category: c.Category(),
		}
	}

	return &CheckResult{
		Name:     c.Name(),
		Status:   StatusWarning,
		Message:  fmt.Sprintf("Found %d role file issue(s) that may pollute source repos", len(c.issues)),
		Details:  c.issues,
		FixHint:  "Run 'gt doctor --fix' to remove unwanted files or revert to @AGENTS.md pointers",
		Category: c.Category(),
	}
}

// checkClaudeFile verifies a CLAUDE.md file is either absent or exactly "@AGENTS.md".
func (c *RoleClaudeMdCheck) checkClaudeFile(path, roleName string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist - that's fine
		}
		// Other errors (permission, etc.) are OK to ignore
		return nil
	}

	content := strings.TrimSpace(string(data))
	if content == "@AGENTS.md" || content == "@AGENTS.md\n" {
		return nil // File is the correct pointer
	}

	// File exists but has wrong content
	return fmt.Errorf("%s/CLAUDE.md exists with non-pointer content (should be absent or exactly '@AGENTS.md')", roleName)
}

// checkAgentsFile verifies an AGENTS.md file doesn't exist at role level.
// Agent-level AGENTS.md should not exist; the town-root AGENTS.md is the
// canonical source. Roles inherit it via @AGENTS.md or ephemeral injection.
func (c *RoleClaudeMdCheck) checkAgentsFile(path, roleName string) error {
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist - that's correct
		}
		return nil // Other errors - ignore
	}

	// File exists at role level - it shouldn't
	return fmt.Errorf("%s/AGENTS.md exists at role level (should be inherited, not duplicated)", roleName)
}

// Fix removes unwanted CLAUDE.md/AGENTS.md files or reverts them to @AGENTS.md.
func (c *RoleClaudeMdCheck) Fix(ctx *CheckContext) error {
	rigName := ctx.RigName
	if rigName == "" {
		rigName = "gastown"
	}
	rigPath := filepath.Join(ctx.TownRoot, rigName)

	roleNames := []string{"mayor", "refinery", "witness", "crew", "polecats"}

	for _, roleName := range roleNames {
		rolePath := filepath.Join(rigPath, roleName)

		// Fix CLAUDE.md
		claudePath := filepath.Join(rolePath, "CLAUDE.md")
		if data, err := os.ReadFile(claudePath); err == nil {
			content := strings.TrimSpace(string(data))
			if content != "@AGENTS.md" && content != "@AGENTS.md\n" {
				// File has wrong content - remove it so Git doesn't track it
				if err := os.Remove(claudePath); err != nil {
					return fmt.Errorf("removing %s: %w", claudePath, err)
				}
			}
		}

		// Remove AGENTS.md at role level
		agentsPath := filepath.Join(rolePath, "AGENTS.md")
		if _, err := os.Stat(agentsPath); err == nil {
			if err := os.Remove(agentsPath); err != nil {
				return fmt.Errorf("removing %s: %w", agentsPath, err)
			}
		}
	}

	return nil
}
