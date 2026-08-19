package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/steveyegge/gastown/internal/constants"
	"github.com/steveyegge/gastown/internal/session"
	"github.com/steveyegge/gastown/internal/tmux"
	"github.com/steveyegge/gastown/internal/workspace"
)

// findMailWorkDir returns the town root for all mail operations.
//
// Two-level beads architecture:
// - Town beads (~/gt/.beads/): ALL mail and coordination
// - Clone beads (<rig>/crew/*/.beads/): Project issues only
//
// Mail ALWAYS uses town beads, regardless of sender or recipient address.
// This ensures messages are visible to all agents in the town.
//
// GT_TOWN_ROOT is preferred over workspace detection because workspace.Find
// stops at the first mayor/town.json when not in a worktree path. Rigs that
// have their own mayor/town.json (e.g., gastown/) would be misidentified as
// the town root when running from the rig directory.
func findMailWorkDir() (string, error) {
	for _, envName := range []string{"GT_TOWN_ROOT", "GT_ROOT"} {
		if townRoot := os.Getenv(envName); townRoot != "" {
			if ok, _ := workspace.IsWorkspace(townRoot); ok {
				return townRoot, nil
			}
		}
	}
	return workspace.FindFromCwdOrError()
}

// findLocalBeadsDir finds the nearest .beads directory by walking up from CWD.
// Used for project work (molecules, issue creation) that uses clone beads.
//
// Priority:
//  1. BEADS_DIR environment variable (set by session manager for polecats)
//  2. Walk up from CWD looking for .beads directory
//
// Polecats use redirect-based beads access, so their worktree doesn't have a full
// .beads directory. The session manager sets BEADS_DIR to the correct location.
func findLocalBeadsDir() (string, error) {
	// Check BEADS_DIR environment variable first (set by session manager for polecats).
	// This is important for polecats that use redirect-based beads access.
	if beadsDir := os.Getenv("BEADS_DIR"); beadsDir != "" {
		// BEADS_DIR points directly to the .beads directory, return its parent
		if _, err := os.Stat(beadsDir); err == nil {
			return filepath.Dir(beadsDir), nil
		}
	}

	return findCwdBeadsWorkDir()
}

// detectSender determines the current context's address.
// Priority:
//  1. GT_ROLE env var → use the role-based identity (agent session)
//  2. No GT_ROLE → try cwd-based detection (witness/refinery/polecat/crew directories)
//  3. No match → return "overseer" (human at terminal)
//
// All Gas Town agents run in tmux sessions with GT_ROLE set at spawn.
// However, cwd-based detection is also tried to support running commands
// from agent directories without GT_ROLE set (e.g., debugging sessions).
func detectSender() string {
	// Check GT_ROLE first (authoritative for agent sessions)
	role := os.Getenv("GT_ROLE")
	if role != "" {
		// Agent session - build address from role and context
		return detectSenderFromRole(role)
	}

	// No GT_ROLE - try cwd-based detection, defaults to overseer if not in agent directory
	return detectSenderFromCwd()
}

// detectSenderFromRole builds an address from the GT_ROLE and related env vars.
// GT_ROLE can be either a simple role name ("crew", "polecat") or a full address
// ("greenplace/crew/joe") depending on how the session was started.
//
// If GT_ROLE is a simple name but required env vars (GT_RIG, GT_POLECAT, etc.)
// are missing, falls back to cwd-based detection. This could return "overseer"
// if cwd doesn't match any known agent path - a misconfigured agent session.
func detectSenderFromRole(role string) string {
	rig := os.Getenv("GT_RIG")

	// Check if role is already a full address (contains /)
	if strings.Contains(role, "/") {
		// GT_ROLE is already a full address, use it directly
		return role
	}

	// GT_ROLE is a simple role name, build the full address
	switch role {
	case constants.RoleMayor:
		return "mayor/"
	case constants.RoleDeacon:
		return "deacon/"
	case constants.RolePolecat:
		polecat := os.Getenv("GT_POLECAT")
		if rig != "" && polecat != "" {
			return fmt.Sprintf("%s/%s", rig, polecat)
		}
		// Fallback to cwd detection for polecats
		return detectSenderFromCwd()
	case constants.RoleCrew:
		crew := os.Getenv("GT_CREW")
		if rig != "" && crew != "" {
			return fmt.Sprintf("%s/crew/%s", rig, crew)
		}
		// Fallback to cwd detection for crew
		return detectSenderFromCwd()
	case constants.RoleWitness:
		if rig != "" {
			return fmt.Sprintf("%s/witness", rig)
		}
		return detectSenderFromCwd()
	case constants.RoleRefinery:
		if rig != "" {
			return fmt.Sprintf("%s/refinery", rig)
		}
		return detectSenderFromCwd()
	case "dog":
		dogName := os.Getenv("GT_DOG_NAME")
		if dogName != "" {
			return fmt.Sprintf("deacon/dogs/%s", dogName)
		}
		return detectSenderFromCwd()
	default:
		// Unknown role, try cwd detection
		return detectSenderFromCwd()
	}
}

// detectSenderVerified resolves the sender the same way detectSender does,
// but when GT_ROLE claims an identity, cross-checks the claim against the
// identity of the tmux session this process is actually a descendant of.
// tmux.ResolveCurrentSession walks the kernel process-ancestry chain to find
// that session, so it reflects where the process was really spawned, not
// what its own env vars say -- unlike GT_ROLE, which any local caller can
// set to an arbitrary value before invoking a subcommand (gt-9z0y: "GT_ROLE=
// overseer gt mail send ..." forged the exact sender/actor attribution the
// gt-p7gu --from fix was designed to block).
//
// Verification is skipped (the claim is trusted, matching detectSender's
// pre-existing behavior) when no tmux ancestry can be resolved -- tests, CI,
// and human terminals outside tmux are unaffected.
func detectSenderVerified() (string, error) {
	claimed := detectSender()
	if os.Getenv("GT_ROLE") == "" {
		return claimed, nil
	}
	verified, ok := verifiedSenderFromSession()
	return resolveVerifiedSender(claimed, verified, ok)
}

// resolveVerifiedSender is the pure decision behind detectSenderVerified,
// factored out so it can be unit tested without a live tmux session.
func resolveVerifiedSender(claimed, verified string, verifiedOK bool) (string, error) {
	if !verifiedOK || normalizeAddress(claimed) == normalizeAddress(verified) {
		return claimed, nil
	}
	if strings.HasPrefix(claimed, "convoy/") {
		// Sole recognized synthetic actor override -- same allowance already
		// established for --from by gt-p7gu.
		return claimed, nil
	}
	return "", fmt.Errorf("GT_ROLE claims sender %q but this process is running in a session that resolves to %q; refusing to send mail as an unverified identity (gt-9z0y)", claimed, verified)
}

// verifiedSenderFromSession derives an identity purely from the tmux session
// this process is actually running under (see detectSenderVerified), not
// from any environment variable. Returns ok=false when no tmux ancestry is
// resolvable, meaning verification is unavailable rather than failed.
func verifiedSenderFromSession() (string, bool) {
	name, err := tmux.NewTmux().ResolveCurrentSession()
	if err != nil || name == "" {
		return "", false
	}
	identity, err := session.ParseSessionName(name)
	if err != nil {
		return "", false
	}
	addr := identity.Address()
	if addr == "" {
		return "", false
	}
	return addr, true
}

// detectSenderFromCwd is the cwd-based detection, using segment-relative parsing.
// It unifies detectSenderFromCwd (mail_identity.go) and detectRole (role.go).
func detectSenderFromCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "overseer"
	}

	// Prefer explicit agent identity metadata when available.
	// This avoids brittle path parsing from nested agent dirs (for example witness/rig).
	if fromFile := detectSenderFromAgentFile(cwd); fromFile != "" {
		return fromFile
	}

	// Try to detect role from cwd using segment-relative parsing
	// (safer than substring matching, and consistent with role.go's detectRole).
	// If townRoot cannot be found, we default to "overseer" rather than
	// falling back to substring matching (per gt-7yy6 design).
	townRoot, err := workspace.FindFromCwd()
	if err == nil && townRoot != "" {
		return roleInfoToSenderAddress(detectRole(cwd, townRoot))
	}

	// No townRoot found and no .gt-agent file - default to overseer (human)
	return "overseer"
}

// roleInfoToSenderAddress converts a detected RoleInfo (from detectRole) into
// the string address format used by mail sending (e.g., "rig/polecats/name", "mayor/", etc.).
// This is the unified conversion point for role -> sender address.
func roleInfoToSenderAddress(info RoleInfo) string {
	switch info.Role {
	case RoleMayor:
		// Town-level mayor has trailing slash; rig-level mayor is also just "mayor/"
		return "mayor/"
	case RoleDeacon:
		return "deacon/"
	case RoleBoot:
		return "deacon/dogs/boot"
	case RoleDog:
		if info.Polecat != "" {
			return fmt.Sprintf("deacon/dogs/%s", info.Polecat)
		}
		return "deacon/dogs"
	case RoleWitness:
		if info.Rig != "" {
			return fmt.Sprintf("%s/witness", info.Rig)
		}
		return "witness"
	case RoleRefinery:
		if info.Rig != "" {
			return fmt.Sprintf("%s/refinery", info.Rig)
		}
		return "refinery"
	case RolePolecat:
		if info.Rig != "" && info.Polecat != "" {
			return fmt.Sprintf("%s/polecats/%s", info.Rig, info.Polecat)
		}
		return "polecat"
	case RoleCrew:
		if info.Rig != "" && info.Polecat != "" {
			return fmt.Sprintf("%s/crew/%s", info.Rig, info.Polecat)
		}
		return "crew"
	default:
		// RoleUnknown or unknown role
		return "overseer"
	}
}

type agentIdentityFile struct {
	Role string `json:"role"`
	Rig  string `json:"rig"`
	Name string `json:"name"`
}

func detectSenderFromAgentFile(startDir string) string {
	path := startDir
	for {
		agentPath := filepath.Join(path, ".gt-agent")
		data, err := os.ReadFile(agentPath)
		if err == nil {
			var parsed agentIdentityFile
			if json.Unmarshal(data, &parsed) == nil {
				if id := identityFromAgentFile(parsed); id != "" {
					return id
				}
			}
		}
		parent := filepath.Dir(path)
		if parent == path {
			break
		}
		path = parent
	}
	return ""
}

func identityFromAgentFile(parsed agentIdentityFile) string {
	role := strings.TrimSpace(strings.ToLower(parsed.Role))
	rig := strings.TrimSpace(parsed.Rig)
	name := strings.TrimSpace(parsed.Name)

	switch role {
	case constants.RoleMayor:
		return "mayor/"
	case constants.RoleDeacon:
		return "deacon/"
	case constants.RoleWitness:
		if rig != "" {
			return fmt.Sprintf("%s/witness", rig)
		}
	case constants.RoleRefinery:
		if rig != "" {
			return fmt.Sprintf("%s/refinery", rig)
		}
	case constants.RoleCrew:
		if rig != "" && name != "" {
			return fmt.Sprintf("%s/crew/%s", rig, name)
		}
	case constants.RolePolecat:
		if rig != "" && name != "" {
			return fmt.Sprintf("%s/polecats/%s", rig, name)
		}
	case "dog":
		if name != "" {
			return fmt.Sprintf("deacon/dogs/%s", name)
		}
	}

	return ""
}

// claimedActor returns the claimed actor/sender identity for audit/display purposes.
// It tries BD_ACTOR first (direct env var), then falls back to detectSender() cwd-based detection.
//
// WARNING: This returns an UNVERIFIED, spoofable, self-reported claim.
// For security-sensitive contexts requiring verified identity, use:
// - detectSenderVerified() - verifies sender identity against tmux session ancestry
// - verifiedSenderFromSession() - requires explicit session authentication
//
// This is safe for:
// - Display/attribution fields (CreatedBy, FiledBy, etc.)
// - Non-security audit logging
// - Self-exclusion filters (skip self in broadcasts)
//
// This is NOT safe for:
// - Access control decisions
// - State mutation (subscribe/unsubscribe operations)
// - Cross-check anti-spoofing invariants
func claimedActor() string {
	if actor := os.Getenv("BD_ACTOR"); actor != "" {
		return actor
	}
	return detectSender()
}
