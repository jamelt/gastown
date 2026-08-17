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
	tablesCSV, err := QueryCSV(townRoot, fmt.Sprintf("USE `%s`; SHOW TABLES AS OF '%s'", db, ref))
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
		data, err := QueryJSON(townRoot, fmt.Sprintf("USE `%s`; SELECT * FROM `%s` AS OF '%s'", db, table, ref))
		if err != nil {
			return fmt.Errorf("exporting table %s: %w", table, err)
		}
		if err := writePreservedFile(outputDir, filepath.Join(label, table+".json"), []byte(data), files); err != nil {
			return err
		}
	}
	history, err := QueryJSON(townRoot, fmt.Sprintf("USE `%s`; SELECT * FROM dolt_log('%s')", db, ref))
	if err != nil {
		return fmt.Errorf("exporting commit history: %w", err)
	}
	return writePreservedFile(outputDir, filepath.Join(label, "dolt_log.json"), []byte(history), files)
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
