package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
)

// BeadPrefixResidencyCheck detects beads whose ID prefix does not match the
// routes.jsonl prefix for the database they are actually stored in. A
// misfiled bead is not corrupted — it is merely invisible: 'bd ready' and
// prefix-based routing both operate against the database its prefix should
// route to, so a bead stored under the wrong prefix never surfaces there.
// See gt-d2qp.
type BeadPrefixResidencyCheck struct {
	BaseCheck
	lister residencyIssueLister
}

// NewBeadPrefixResidencyCheck creates a new bead prefix residency check.
func NewBeadPrefixResidencyCheck() *BeadPrefixResidencyCheck {
	return &BeadPrefixResidencyCheck{
		BaseCheck: BaseCheck{
			CheckName:        "bead-prefix-residency",
			CheckDescription: "Check that bead ID prefixes match the database they're stored in",
			CheckCategory:    CategoryConfig,
		},
	}
}

// residencyIssueLister abstracts listing every issue in a database, so tests
// can inject canned results without shelling out to bd.
type residencyIssueLister interface {
	ListIssues(workDir string) ([]*beads.Issue, error)
}

// realResidencyIssueLister shells out to bd via the beads package.
type realResidencyIssueLister struct{}

func (r *realResidencyIssueLister) ListIssues(workDir string) ([]*beads.Issue, error) {
	return beads.New(workDir).List(beads.ListOptions{Status: "all"})
}

// residencyDBGroup is one physical database: a representative working
// directory to query it from, and every route prefix that legitimately
// belongs there. Multiple routes.jsonl entries can resolve to the same
// physical database (e.g. a rig that redirects to the shared town DB), so
// grouping is keyed by the canonical (redirect-resolved) beads directory.
type residencyDBGroup struct {
	workDir  string
	prefixes []string
}

// Run checks whether any bead's ID prefix disagrees with the database it's
// stored in.
func (c *BeadPrefixResidencyCheck) Run(ctx *CheckContext) *CheckResult {
	beadsDir := filepath.Join(ctx.TownRoot, ".beads")
	routes, err := beads.LoadRoutes(beadsDir)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: "Could not load routes.jsonl",
		}
	}
	if len(routes) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No routes configured (nothing to check)",
		}
	}

	lister := c.lister
	if lister == nil {
		if _, err := exec.LookPath("bd"); err != nil {
			return &CheckResult{
				Name:    c.Name(),
				Status:  StatusOK,
				Message: "beads not installed (skipped)",
			}
		}
		lister = &realResidencyIssueLister{}
	}

	// Group routes by the physical database they resolve to (following
	// redirects). Databases shared by multiple routes co-own all of those
	// routes' prefixes.
	dbs := make(map[string]*residencyDBGroup) // canonical beads dir -> group
	var order []string

	for _, r := range routes {
		workDir := ctx.TownRoot
		if r.Path != "." && r.Path != "" {
			workDir = filepath.Join(ctx.TownRoot, r.Path)
		}
		canonical, err := filepath.Abs(beads.ResolveBeadsDir(workDir))
		if err != nil {
			continue
		}
		g, ok := dbs[canonical]
		if !ok {
			g = &residencyDBGroup{workDir: workDir}
			dbs[canonical] = g
			order = append(order, canonical)
		}
		g.prefixes = append(g.prefixes, r.Prefix)
	}

	// Map every known route prefix to the canonical database that owns it,
	// so a misfiled bead's correct home can be named in the report.
	prefixOwner := make(map[string]string) // prefix -> canonical beads dir
	for canonical, g := range dbs {
		for _, p := range g.prefixes {
			prefixOwner[p] = canonical
		}
	}

	var details []string
	var misfiledCount int

	for _, canonical := range order {
		g := dbs[canonical]
		if _, err := os.Stat(canonical); err != nil {
			continue // database doesn't exist locally — nothing to scan
		}

		issues, err := lister.ListIssues(g.workDir)
		if err != nil {
			continue // unreachable database — other checks report this
		}

		for _, issue := range issues {
			if hasAnyPrefix(issue.ID, g.prefixes) {
				continue
			}

			ownerCanonical := longestPrefixOwner(issue.ID, prefixOwner)
			if ownerCanonical == "" {
				continue // prefix not in routes.jsonl at all — not this check's concern
			}

			misfiledCount++
			if len(details) < 25 {
				details = append(details, fmt.Sprintf("%s stored in %s but routes to %s",
					issue.ID,
					displayDBLabel(ctx.TownRoot, g.workDir),
					displayDBLabel(ctx.TownRoot, dbs[ownerCanonical].workDir)))
			}
		}
	}

	if misfiledCount == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "All bead prefixes match the database they're stored in",
		}
	}

	sort.Strings(details)
	if misfiledCount > len(details) {
		details = append(details, fmt.Sprintf("... and %d more", misfiledCount-len(details)))
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: fmt.Sprintf("%d bead(s) misfiled: ID prefix doesn't match the database they're stored in", misfiledCount),
		Details: details,
		FixHint: "Misfiled beads are invisible to 'bd ready' and routing in their correct database — move each bead to the database its prefix routes to.",
	}
}

// hasAnyPrefix reports whether id begins with any of the given prefixes.
func hasAnyPrefix(id string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(id, p) {
			return true
		}
	}
	return false
}

// longestPrefixOwner returns the canonical database whose route prefix is
// the longest match for id (e.g. preferring "hq-cv-" over "hq-" when both
// match), or "" if no route prefix matches at all.
func longestPrefixOwner(id string, prefixOwner map[string]string) string {
	best := ""
	bestOwner := ""
	for prefix, owner := range prefixOwner {
		if len(prefix) > len(best) && strings.HasPrefix(id, prefix) {
			best = prefix
			bestOwner = owner
		}
	}
	return bestOwner
}

// displayDBLabel renders a database's working directory as a short label
// for check output.
func displayDBLabel(townRoot, workDir string) string {
	if workDir == townRoot {
		return "town root"
	}
	if rel, err := filepath.Rel(townRoot, workDir); err == nil {
		return rel
	}
	return workDir
}
