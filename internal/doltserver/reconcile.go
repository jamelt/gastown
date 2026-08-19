package doltserver

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var (
	reconciliationQueryCSV  = QueryCSV
	reconciliationQueryJSON = QueryJSON
)

// ReconciliationReceipt records a non-destructive preservation bundle. The
// selected authority is a decision for a later, explicit reconstruction; this
// operation never resets, merges, deletes, or pushes either history.
type ReconciliationReceipt struct {
	Version   int               `json:"version"`
	CreatedAt time.Time         `json:"created_at"`
	Authority string            `json:"authority"`
	Lineage   LineageReport     `json:"lineage"`
	Files     map[string]string `json:"files_sha256"`
	Mutation  string            `json:"mutation"`
	NextStep  string            `json:"next_step"`
}

// ReconciliationApprovalToken returns the exact token required to create a
// preservation bundle for this pair of heads.
func ReconciliationApprovalToken(report LineageReport) string {
	return fmt.Sprintf("RECONCILE-%s-%s-%s", report.Database, shortDoltHash(report.LocalHead), shortDoltHash(report.RemoteHead))
}

// CreateReconciliationBundle exports complete table snapshots and commit logs
// for both histories, plus a hash-addressed audit receipt. It is deliberately
// read-only with respect to Dolt.
func CreateReconciliationBundle(townRoot string, report LineageReport, authority, approval, outputDir string) (string, error) {
	if authority != "local" && authority != "remote" {
		return "", fmt.Errorf("authoritative history must be explicitly set to local or remote")
	}
	if report.State != LineageDiverged {
		return "", fmt.Errorf("database %s is not in no-common-ancestor state", report.Database)
	}
	wantApproval := ReconciliationApprovalToken(report)
	if approval != wantApproval {
		return "", fmt.Errorf("approval required; rerun with --approve %s after reviewing both heads and unique-record counts", wantApproval)
	}
	if outputDir == "" {
		outputDir = filepath.Join(townRoot, ".runtime", "dolt-reconciliation",
			fmt.Sprintf("%s-%s", report.Database, time.Now().UTC().Format("20060102T150405Z")))
	}
	if _, err := os.Stat(outputDir); err == nil {
		return "", fmt.Errorf("output directory already exists: %s", outputDir)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("checking output directory: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return "", fmt.Errorf("creating preservation directory: %w", err)
	}

	remoteRef := "remotes/" + report.RemoteName + "/main"
	files := make(map[string]string)
	for label, ref := range map[string]string{"local": "main", "remote": remoteRef} {
		if err := exportDoltRevision(townRoot, report.Database, label, ref, outputDir, files); err != nil {
			return "", fmt.Errorf("exporting %s history: %w (partial preservation bundle retained at %s)", label, err, outputDir)
		}
	}

	receipt := ReconciliationReceipt{
		Version:   1,
		CreatedAt: time.Now().UTC(),
		Authority: authority,
		Lineage:   report,
		Files:     files,
		Mutation:  "none: read-only snapshot/export; no fetch, merge, reset, delete, or push performed",
		NextStep:  "Construct a new shared-lineage database from the selected authority, import every record absent from it using the exported snapshots, validate counts and IDs, then configure clients to the new database. Never force-push either preserved history.",
	}
	receiptData, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return "", err
	}
	receiptData = append(receiptData, '\n')
	if err := os.WriteFile(filepath.Join(outputDir, "receipt.json"), receiptData, 0o600); err != nil {
		return "", fmt.Errorf("writing audit receipt: %w", err)
	}
	return outputDir, nil
}

func exportDoltRevision(townRoot, db, label, ref, outputDir string, files map[string]string) error {
	tablesCSV, err := reconciliationQueryCSV(townRoot, fmt.Sprintf("USE `%s`; SHOW TABLES AS OF '%s'", db, ref))
	if err != nil {
		return err
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimSpace(tablesCSV))).ReadAll()
	if err != nil {
		return fmt.Errorf("parsing table list: %w", err)
	}
	var tables []string
	for _, row := range rows[1:] {
		if len(row) > 0 && validSQLName(row[0]) {
			tables = append(tables, row[0])
		}
	}
	sort.Strings(tables)
	dir := filepath.Join(outputDir, label)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, table := range tables {
		data, err := reconciliationQueryJSON(townRoot, fmt.Sprintf("USE `%s`; SELECT * FROM `%s` AS OF '%s'", db, table, ref))
		if err != nil {
			return fmt.Errorf("exporting table %s: %w", table, err)
		}
		if err := writePreservedFile(outputDir, filepath.Join(label, table+".json"), []byte(data), files); err != nil {
			return err
		}
	}
	history, err := reconciliationQueryJSON(townRoot, fmt.Sprintf("USE `%s`; SELECT * FROM dolt_log('%s')", db, ref))
	if err != nil {
		return fmt.Errorf("exporting commit history: %w", err)
	}
	return writePreservedFile(outputDir, filepath.Join(label, "dolt_log.json"), []byte(history), files)
}

// ReconciliationVerification reports whether every issue preserved in a
// reconciliation bundle is present in the live database after reconstruction.
type ReconciliationVerification struct {
	Database      string   `json:"database"`
	ExpectedCount int      `json:"expected_count"`
	CurrentCount  int      `json:"current_count"`
	MissingIDs    []string `json:"missing_ids"`
}

// OK reports whether no preserved issue was lost.
func (v ReconciliationVerification) OK() bool {
	return len(v.MissingIDs) == 0
}

// VerifyReconciliationImport compares the issue IDs preserved on both heads
// of a reconciliation bundle (written by CreateReconciliationBundle) against
// the issue IDs currently live in db, and reports any that are missing. It
// performs no writes.
//
// This is the check the 2026-08-17 incident lacked: 31 gastown issues,
// including 3 open P0s, were dropped during a manual post-bundle
// reconstruction with no automated verification to catch the loss.
func VerifyReconciliationImport(townRoot, bundleDir, db string) (ReconciliationVerification, error) {
	expected := make(map[string]bool)
	for _, label := range []string{"local", "remote"} {
		ids, err := readBundleIssueIDs(filepath.Join(bundleDir, label, "issues.json"))
		if err != nil {
			return ReconciliationVerification{}, fmt.Errorf("reading %s issue snapshot: %w", label, err)
		}
		for _, id := range ids {
			expected[id] = true
		}
	}
	if len(expected) == 0 {
		return ReconciliationVerification{}, fmt.Errorf("bundle %s has no preserved issues to verify against", bundleDir)
	}

	currentJSON, err := reconciliationQueryJSON(townRoot, fmt.Sprintf("USE `%s`; SELECT id FROM issues", db))
	if err != nil {
		return ReconciliationVerification{}, fmt.Errorf("querying live issues: %w", err)
	}
	current, err := parseIssueIDRows(currentJSON)
	if err != nil {
		return ReconciliationVerification{}, fmt.Errorf("parsing live issue IDs: %w", err)
	}
	currentSet := make(map[string]bool, len(current))
	for _, id := range current {
		currentSet[id] = true
	}

	var missing []string
	for id := range expected {
		if !currentSet[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)

	return ReconciliationVerification{
		Database:      db,
		ExpectedCount: len(expected),
		CurrentCount:  len(current),
		MissingIDs:    missing,
	}, nil
}

func readBundleIssueIDs(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return parseIssueIDRows(string(data))
}

func parseIssueIDRows(data string) ([]string, error) {
	var parsed struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := json.Unmarshal([]byte(data), &parsed); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(parsed.Rows))
	for _, row := range parsed.Rows {
		if id, ok := row["id"].(string); ok && id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func writePreservedFile(root, relative string, data []byte, files map[string]string) error {
	if !strings.HasSuffix(string(data), "\n") {
		data = append(data, '\n')
	}
	path := filepath.Join(root, relative)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", relative, err)
	}
	sum := sha256.Sum256(data)
	files[filepath.ToSlash(relative)] = hex.EncodeToString(sum[:])
	return nil
}
