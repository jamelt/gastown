package doltserver

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// LineageState describes whether a local main branch can safely exchange
// history with its configured remote main branch.
type LineageState string

const (
	LineageNoRemote         LineageState = "no-remote"
	LineageRemoteUnverified LineageState = "remote-unverified"
	LineageShared           LineageState = "shared"
	LineageDiverged         LineageState = "no-common-ancestor"
)

// LineageReport is a read-only diagnostic snapshot. RemoteHead is the local
// remote-tracking head; callers that require a freshly fetched head should run
// their normal fetch step before inspection.
type LineageReport struct {
	Database          string       `json:"database"`
	RemoteName        string       `json:"remote_name,omitempty"`
	RemoteURL         string       `json:"remote_url,omitempty"`
	LocalHead         string       `json:"local_head,omitempty"`
	RemoteHead        string       `json:"remote_head,omitempty"`
	MergeBase         string       `json:"merge_base,omitempty"`
	LocalOnlyCommits  int          `json:"local_only_commits"`
	RemoteOnlyCommits int          `json:"remote_only_commits"`
	LocalOnlyRecords  int          `json:"local_only_records"`
	RemoteOnlyRecords int          `json:"remote_only_records"`
	State             LineageState `json:"state"`
}

// SafeToPush reports whether a push can proceed without replacing unrelated
// remote history. A configured remote with no tracking head is allowed so an
// initial push can establish the remote branch. Dispatch uses Shared() instead.
func (r LineageReport) SafeToPush() bool {
	return r.State != LineageDiverged
}

// Shared reports whether both sides have a verified common ancestor.
func (r LineageReport) Shared() bool {
	return r.State == LineageShared
}

// Diagnostic returns a stable, actionable summary suitable for doctor, sync,
// dispatch, and completion errors.
func (r LineageReport) Diagnostic() string {
	return fmt.Sprintf(
		"database %s Dolt lineage %s (local=%s remote=%s merge-base=%s; local-only commits=%d records=%d; remote-only commits=%d records=%d)",
		r.Database, r.State, shortDoltHash(r.LocalHead), shortDoltHash(r.RemoteHead), shortDoltHash(r.MergeBase),
		r.LocalOnlyCommits, r.LocalOnlyRecords, r.RemoteOnlyCommits, r.RemoteOnlyRecords,
	)
}

func shortDoltHash(hash string) string {
	if hash == "" {
		return "none"
	}
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}

type lineageQuerier func(query string) (string, error)

// InspectLineageSQL inspects local and remote-tracking history through the
// running Dolt server. It performs SELECTs only: neither branch, working set,
// nor remote is fetched, merged, reset, pushed, or otherwise mutated.
func InspectLineageSQL(townRoot, db string) (LineageReport, error) {
	if !validSQLName(db) {
		return LineageReport{}, fmt.Errorf("invalid database name %q", db)
	}
	remoteName, remoteURL, err := FindRemoteSQL(townRoot, db)
	if err != nil {
		return LineageReport{}, err
	}
	q := func(query string) (string, error) {
		return QueryCSV(townRoot, fmt.Sprintf("USE `%s`; %s", db, query))
	}
	return inspectLineage(db, remoteName, remoteURL, q)
}

// InspectLineageCLI is the stopped-server counterpart to InspectLineageSQL.
// It opens the database read-only through Dolt CLI SELECT statements.
func InspectLineageCLI(dbDir, db string) (LineageReport, error) {
	remoteName, remoteURL, err := FindRemote(dbDir)
	if err != nil {
		return LineageReport{}, err
	}
	q := func(query string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "dolt", "sql", "-r", "csv", "-q", query)
		cmd.Dir = dbDir
		setProcessGroup(cmd)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("dolt sql: %w (%s)", err, strings.TrimSpace(string(out)))
		}
		return string(out), nil
	}
	return inspectLineage(db, remoteName, remoteURL, q)
}

func inspectLineage(db, remoteName, remoteURL string, query lineageQuerier) (LineageReport, error) {
	report := LineageReport{
		Database:   db,
		RemoteName: remoteName,
		RemoteURL:  remoteURL,
		State:      LineageNoRemote,
	}
	if remoteName == "" || remoteURL == "" {
		return report, nil
	}
	if !validSQLName(remoteName) {
		return LineageReport{}, fmt.Errorf("invalid remote name %q", remoteName)
	}

	var err error
	report.LocalHead, err = queryScalar(query, "SELECT hash FROM dolt_branches WHERE name = 'main' LIMIT 1")
	if err != nil {
		return report, fmt.Errorf("reading local main head: %w", err)
	}
	remoteRef := "remotes/" + remoteName + "/main"
	report.RemoteHead, err = queryScalar(query, fmt.Sprintf("SELECT hash FROM dolt_remote_branches WHERE name = '%s' LIMIT 1", remoteRef))
	if err != nil {
		return report, fmt.Errorf("reading remote main head: %w", err)
	}
	if report.LocalHead == "" || report.RemoteHead == "" {
		report.State = LineageRemoteUnverified
		return report, nil
	}

	mergeBaseQuery := fmt.Sprintf("SELECT DOLT_MERGE_BASE('main', '%s') AS merge_base", remoteRef)
	report.MergeBase, err = queryScalar(query, mergeBaseQuery)
	if err != nil {
		if !isNoCommonAncestorError(err) {
			return report, fmt.Errorf("checking merge base: %w", err)
		}
		report.State = LineageDiverged
	} else if report.MergeBase == "" {
		report.State = LineageDiverged
	} else {
		report.State = LineageShared
	}

	// Counts make the failure actionable and document records at risk. These
	// queries are intentionally best-effort so older schemas without issues do
	// not hide the more important lineage verdict.
	report.LocalOnlyCommits, _ = queryInt(query, fmt.Sprintf(
		"SELECT COUNT(*) FROM dolt_log('main') WHERE commit_hash NOT IN (SELECT commit_hash FROM dolt_log('%s'))", remoteRef))
	report.RemoteOnlyCommits, _ = queryInt(query, fmt.Sprintf(
		"SELECT COUNT(*) FROM dolt_log('%s') WHERE commit_hash NOT IN (SELECT commit_hash FROM dolt_log('main'))", remoteRef))
	report.LocalOnlyRecords, _ = queryInt(query, fmt.Sprintf(
		"SELECT COUNT(*) FROM issues AS OF 'main' l WHERE NOT EXISTS (SELECT 1 FROM issues AS OF '%s' r WHERE r.id = l.id)", remoteRef))
	report.RemoteOnlyRecords, _ = queryInt(query, fmt.Sprintf(
		"SELECT COUNT(*) FROM issues AS OF '%s' r WHERE NOT EXISTS (SELECT 1 FROM issues AS OF 'main' l WHERE l.id = r.id)", remoteRef))

	return report, nil
}

func queryScalar(query lineageQuerier, statement string) (string, error) {
	out, err := query(statement)
	if err != nil {
		return "", err
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimSpace(out))).ReadAll()
	if err != nil {
		return "", fmt.Errorf("parsing CSV: %w", err)
	}
	if len(rows) < 2 || len(rows[1]) == 0 {
		return "", nil
	}
	return strings.TrimSpace(rows[1][0]), nil
}

func queryInt(query lineageQuerier, statement string) (int, error) {
	value, err := queryScalar(query, statement)
	if err != nil || value == "" {
		return 0, err
	}
	count, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parsing count %q: %w", value, err)
	}
	return count, nil
}

func isNoCommonAncestorError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "no common ancestor") ||
		errors.Is(err, ErrNoCommonAncestor)
}

// ErrNoCommonAncestor is exposed for callers and test fakes that need to
// classify independent Dolt histories without relying on command text.
var ErrNoCommonAncestor = errors.New("no common ancestor")
