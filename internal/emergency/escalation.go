package emergency

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// EmergencyEscalation represents an escalation that bypasses Beads when it's unavailable.
// This is written to a file-based queue for the Witness to process when Beads recovers.
type EmergencyEscalation struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Severity    string    `json:"severity"`
	Source      string    `json:"source"`
	Timestamp   time.Time `json:"timestamp"`
	ReportedBy  string    `json:"reported_by"`
}

// LogFilePath returns the path where emergency escalations are stored.
func LogFilePath(townRoot string) string {
	return filepath.Join(townRoot, "daemon", "emergency-escalations.jsonl")
}

// Write appends an emergency escalation to the log file.
// The Witness will read this file and convert escalations to Beads when Dolt recovers.
func Write(townRoot string, escalation *EmergencyEscalation) error {
	if escalation.ID == "" {
		return fmt.Errorf("escalation ID is required")
	}
	if escalation.Timestamp.IsZero() {
		escalation.Timestamp = time.Now()
	}

	logPath := LogFilePath(townRoot)

	// Create directory if needed
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return fmt.Errorf("creating escalation directory: %w", err)
	}

	// Append to JSONL file (each escalation is one line)
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening escalation log: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(escalation)
	if err != nil {
		return fmt.Errorf("marshaling escalation: %w", err)
	}

	if _, err := fmt.Fprintf(f, "%s\n", data); err != nil {
		return fmt.Errorf("writing escalation: %w", err)
	}

	return nil
}

// ReadAll reads all emergency escalations from the log file.
// This is used by the Witness to process escalations when Beads recovers.
func ReadAll(townRoot string) ([]*EmergencyEscalation, error) {
	logPath := LogFilePath(townRoot)

	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening escalation log: %w", err)
	}
	defer f.Close()

	var escalations []*EmergencyEscalation
	decoder := json.NewDecoder(f)
	for {
		var e EmergencyEscalation
		err := decoder.Decode(&e)
		if err == io.EOF {
			break
		}
		if err != nil {
			// Skip malformed lines to avoid losing entire log
			continue
		}
		escalations = append(escalations, &e)
	}

	return escalations, nil
}

// ClearProcessed removes processed escalations from the log file by truncating it.
// This should be called after successfully converting escalations to Beads.
func ClearProcessed(townRoot string) error {
	logPath := LogFilePath(townRoot)

	// Simply truncate the file to clear all processed escalations
	if err := os.Truncate(logPath, 0); err != nil {
		if os.IsNotExist(err) {
			return nil // Already doesn't exist
		}
		return fmt.Errorf("clearing escalation log: %w", err)
	}

	return nil
}
