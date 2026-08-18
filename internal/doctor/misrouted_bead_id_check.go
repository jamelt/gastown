package doctor

import (
	"encoding/csv"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
)

// MisroutedBeadIDCheck detects beads whose ID prefix does not match any
// prefix routed to the database they physically live in.
//
// PrefixConflictCheck, PrefixMismatchCheck, and DatabasePrefixCheck only
// compare *config* sources (rigs.json, routes.jsonl, issue_prefix) against
// each other — none of them look at the actual row data. A bead can be
// misfiled into the wrong physical database (e.g. by a worktree redirect
// silently routing a write to the wrong place) while every config source
// still agrees with itself; that class of bug is invisible until an agent
// hits "no issue found" while searching from the correct side. ~196 hq-*
// beads sat undetected in the gt- database for 2.5 months this way (gt-tmd1).
// This check scans row data directly to close that gap.
//
// Not auto-fixable: moving a bead across databases changes its ID and can
// break references (dependencies, mail threads, molecule state). Report-only
// — an agent must investigate and re-file each bead deliberately.
type MisroutedBeadIDCheck struct {
	BaseCheck
	misrouted []misroutedBeadRow
}

type misroutedBeadRow struct {
	ID            string
	Title         string
	RoutePath     string // route path the database was reached through ("." for town root)
	ValidPrefixes []string
}

// NewMisroutedBeadIDCheck creates a new bead-ID-vs-database check.
func NewMisroutedBeadIDCheck() *MisroutedBeadIDCheck {
	return &MisroutedBeadIDCheck{
		BaseCheck: BaseCheck{
			CheckName:        "misrouted-bead-ids",
			CheckDescription: "Check that bead ID prefixes match the database they physically live in",
			CheckCategory:    CategoryConfig,
		},
	}
}

// beadIDDatabaseGroup is one physical database and every prefix routed to it.
// A single database can legitimately hold more than one prefix (e.g. "hq-"
// and "hq-cv-" both route to the town root's database).
type beadIDDatabaseGroup struct {
	routePath string
	rigPath   string
	prefixes  []string
}

// Run scans every routed database's issues table for rows whose ID prefix
// isn't among the prefixes routed to that database.
func (c *MisroutedBeadIDCheck) Run(ctx *CheckContext) *CheckResult {
	c.misrouted = nil

	routesDir := filepath.Join(ctx.TownRoot, ".beads")
	routes, err := beads.LoadRoutes(routesDir)
	if err != nil || len(routes) == 0 {
		return &CheckResult{
			Name:     c.Name(),
			Status:   StatusOK,
			Message:  "No routes.jsonl found (nothing to check)",
			Category: c.Category(),
		}
	}

	if _, err := exec.LookPath("bd"); err != nil {
		return &CheckResult{
			Name:     c.Name(),
			Status:   StatusOK,
			Message:  "beads not installed (skipped)",
			Category: c.Category(),
		}
	}

	groups, order := groupRoutesByDatabase(ctx.TownRoot, routes)

	for _, resolved := range order {
		g := groups[resolved]
		if _, statErr := os.Stat(resolved); statErr != nil {
			continue // Route configured but database not present locally - not this check's concern
		}

		rows, err := queryAllBeadIDs(g.rigPath)
		if err != nil {
			continue // Dolt unavailable / rig not bd-managed - non-fatal
		}

		for _, row := range rows {
			idPrefix := strings.TrimSuffix(beads.ExtractPrefix(row.ID), "-")
			if idPrefix == "" || containsString(g.prefixes, idPrefix) {
				continue
			}
			c.misrouted = append(c.misrouted, misroutedBeadRow{
				ID:            row.ID,
				Title:         row.Title,
				RoutePath:     g.routePath,
				ValidPrefixes: g.prefixes,
			})
		}
	}

	if len(c.misrouted) == 0 {
		return &CheckResult{
			Name:     c.Name(),
			Status:   StatusOK,
			Message:  "No misrouted bead IDs found",
			Category: c.Category(),
		}
	}

	sort.Slice(c.misrouted, func(i, j int) bool { return c.misrouted[i].ID < c.misrouted[j].ID })

	details := make([]string, 0, len(c.misrouted))
	for _, m := range c.misrouted {
		idPrefix := strings.TrimSuffix(beads.ExtractPrefix(m.ID), "-")
		home := beads.GetRigPathForPrefix(ctx.TownRoot, idPrefix+"-")
		if home == "" {
			home = "unknown (no route for prefix " + idPrefix + "-)"
		}
		details = append(details, fmt.Sprintf("%s %q — lives in %q (accepts %s) but ID prefix is %q; home: %s",
			m.ID, shortenTitle(m.Title, 60), m.RoutePath, strings.Join(m.ValidPrefixes, ", "), idPrefix, home))
	}

	return &CheckResult{
		Name:   c.Name(),
		Status: StatusWarning,
		Message: fmt.Sprintf(
			"%d bead(s) have an ID prefix that doesn't match their database — likely misfiled by a wrong-database write",
			len(c.misrouted)),
		Details: details,
		FixHint: "Not auto-fixable: moving a bead across databases changes its ID and can break references. " +
			"Investigate each bead (see gt-tmd1) and re-file it at its home database, then close the misfiled original.",
		Category: c.Category(),
	}
}

// groupRoutesByDatabase resolves each route to its physical .beads directory
// and groups routes that resolve to the same directory, since one database
// can be the target of more than one prefix.
func groupRoutesByDatabase(townRoot string, routes []beads.Route) (map[string]*beadIDDatabaseGroup, []string) {
	groups := map[string]*beadIDDatabaseGroup{}
	var order []string

	for _, route := range routes {
		rigPath := townRoot
		if route.Path != "." && route.Path != "" {
			rigPath = filepath.Join(townRoot, route.Path)
		}
		resolved, err := filepath.Abs(beads.ResolveBeadsDir(rigPath))
		if err != nil {
			continue
		}
		prefix := strings.TrimSuffix(route.Prefix, "-")
		if prefix == "" {
			continue
		}

		g, ok := groups[resolved]
		if !ok {
			g = &beadIDDatabaseGroup{routePath: route.Path, rigPath: rigPath}
			groups[resolved] = g
			order = append(order, resolved)
		}
		g.prefixes = append(g.prefixes, prefix)
	}

	return groups, order
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

const misroutedBeadIDSelectQuery = `SELECT id, title FROM issues`

type misroutedBeadIDRow struct {
	ID    string
	Title string
}

// queryAllBeadIDs returns every bead ID/title in a rig's database.
// Uses bd sql --csv (raw SQL passthrough, not affected by bd ORM deserialization).
func queryAllBeadIDs(rigDir string) ([]misroutedBeadIDRow, error) {
	cmd := exec.Command("bd", "sql", "--csv", misroutedBeadIDSelectQuery) //nolint:gosec // G204: query is a constant
	cmd.Dir = rigDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("bd sql: %w", err)
	}

	r := csv.NewReader(strings.NewReader(string(output)))
	records, err := r.ReadAll()
	if err != nil || len(records) < 2 {
		return nil, nil // No results or empty table
	}

	rows := make([]misroutedBeadIDRow, 0, len(records)-1)
	for _, rec := range records[1:] { // Skip CSV header
		if len(rec) < 2 {
			continue
		}
		rows = append(rows, misroutedBeadIDRow{
			ID:    strings.TrimSpace(rec[0]),
			Title: strings.TrimSpace(rec[1]),
		})
	}
	return rows, nil
}
