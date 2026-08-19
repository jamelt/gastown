package daemon

import (
	"fmt"
	"reflect"
	"testing"
)

// nopLogger satisfies the dogMol logger interface without emitting output.
type nopLogger struct{}

func (nopLogger) Printf(string, ...interface{}) {}

// TestCloseRemainingStepsForcesClose is the regression guard for gt-adaz:
// mol-dog-reaper feeds itself because sequenced step wisps are blocked-by their
// predecessors, so a plain `bd close` is refused and the wisp leaks, keeping the
// root open and triggering the next pour. The teardown backstop must close its
// own ephemeral children unconditionally (--force) so the residue never forms.
func TestCloseRemainingStepsForcesClose(t *testing.T) {
	var closeCalls [][]string
	dm := &dogMol{
		rootID:  "gt-wisp-root",
		stepIDs: make(map[string]string),
		logger:  nopLogger{},
		runner: func(args ...string) (string, error) {
			switch {
			case len(args) >= 1 && args[0] == "show":
				// Two sequenced, still-open step wisps under the root.
				return `{"gt-wisp-root":[` +
					`{"id":"gt-wisp-a","title":"Scan","status":"open"},` +
					`{"id":"gt-wisp-b","title":"Report","status":"open"}` +
					`],"schema_version":1}`, nil
			case len(args) >= 1 && args[0] == "close":
				closeCalls = append(closeCalls, args)
				return "", nil
			}
			return "", fmt.Errorf("unexpected bd args: %v", args)
		},
	}

	dm.closeRemainingSteps()

	if len(closeCalls) != 2 {
		t.Fatalf("expected 2 close calls, got %d: %v", len(closeCalls), closeCalls)
	}
	for _, call := range closeCalls {
		if !hasArg(call, "--force") {
			t.Errorf("close call %v is missing --force; a sequenced step wisp blocked by its predecessor would leak", call)
		}
	}
}

func hasArg(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}

// TestDogMolCloseClosesRootAfterChildren verifies the full teardown: children
// are force-closed, then the root is closed, leaving no residue.
func TestDogMolCloseClosesRootAfterChildren(t *testing.T) {
	var closed []string
	dm := &dogMol{
		rootID:  "gt-wisp-root",
		stepIDs: make(map[string]string),
		logger:  nopLogger{},
		runner: func(args ...string) (string, error) {
			switch args[0] {
			case "show":
				return `{"gt-wisp-root":[{"id":"gt-wisp-a","title":"Scan","status":"open"}],"schema_version":1}`, nil
			case "close":
				closed = append(closed, args[1])
				return "", nil
			}
			return "", fmt.Errorf("unexpected bd args: %v", args)
		},
	}

	dm.close()

	want := []string{"gt-wisp-a", "gt-wisp-root"}
	if !reflect.DeepEqual(closed, want) {
		t.Errorf("close order = %v, want child then root %v", closed, want)
	}
}

func TestParseWispID(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantID string
	}{
		{
			name:   "standard wisp output",
			input:  "✓ Spawned wisp: gt-wisp-abc123 — Reap stale wisps",
			wantID: "gt-wisp-abc123",
		},
		{
			name:   "wisp ID with ANSI codes",
			input:  "\033[32m✓\033[0m Spawned wisp: \033[1mgt-wisp-xyz789\033[0m — Title",
			wantID: "gt-wisp-xyz789",
		},
		{
			name:   "empty output",
			input:  "",
			wantID: "",
		},
		{
			name:   "no wisp ID in output",
			input:  "Error: something went wrong",
			wantID: "",
		},
		{
			name:   "wisp ID at end of line",
			input:  "Created gt-wisp-def456",
			wantID: "gt-wisp-def456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseWispID(tt.input)
			if got != tt.wantID {
				t.Errorf("parseWispID(%q) = %q, want %q", tt.input, got, tt.wantID)
			}
		})
	}
}

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no ANSI", "hello", "hello"},
		{"color code", "\033[32mgreen\033[0m", "green"},
		{"bold", "\033[1mbold\033[0m", "bold"},
		{"multiple codes", "\033[32m✓\033[0m \033[1mtext\033[0m", "✓ text"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripANSI(tt.input)
			if got != tt.want {
				t.Errorf("stripANSI(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseChildrenJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantIDs []string
		wantErr bool
	}{
		{
			name:    "bare array",
			input:   `[{"id":"a","title":"Probe","status":"open"}]`,
			wantIDs: []string{"a"},
		},
		{
			name:    "map wrapper from bd show",
			input:   `{"hq-wisp-root":[{"id":"hq-wisp-a","title":"Probe","status":"open"},{"id":"hq-wisp-b","title":"Report","status":"open"}]}`,
			wantIDs: []string{"hq-wisp-a", "hq-wisp-b"},
		},
		{
			name:    "empty map wrapper",
			input:   `{"hq-wisp-root":[]}`,
			wantIDs: []string{},
		},
		{
			name:    "schema metadata with children",
			input:   `{"hq-wisp-root":[{"id":"hq-wisp-a","title":"Probe","status":"open"}],"schema_version":1}`,
			wantIDs: []string{"hq-wisp-a"},
		},
		{
			name:    "schema metadata with empty children",
			input:   `{"hq-wisp-root":[],"schema_version":1}`,
			wantIDs: []string{},
		},
		{
			name:    "multiple child arrays are deterministic",
			input:   `{"hq-wisp-b":[{"id":"b-step","title":"Report","status":"open"}],"schema_version":1,"hq-wisp-a":[{"id":"a-step","title":"Probe","status":"open"}]}`,
			wantIDs: []string{"a-step", "b-step"},
		},
		{
			name:    "schema key is metadata even if array-valued",
			input:   `{"schema_version":[{"id":"metadata","title":"Ignore","status":"open"}],"hq-wisp-root":[{"id":"hq-wisp-a","title":"Probe","status":"open"}]}`,
			wantIDs: []string{"hq-wisp-a"},
		},
		{
			name:    "empty array",
			input:   `[]`,
			wantIDs: []string{},
		},
		{
			name:    "empty input",
			input:   `   `,
			wantErr: true,
		},
		{
			name:    "malformed bare array",
			input:   `[`,
			wantErr: true,
		},
		{
			name:    "malformed object envelope",
			input:   `{"hq-wisp-root":[`,
			wantErr: true,
		},
		{
			name:    "invalid json",
			input:   `not json`,
			wantErr: true,
		},
		{
			name:    "malformed child array",
			input:   `{"hq-wisp-root":[{"id":1}],"schema_version":1}`,
			wantErr: true,
		},
		{
			name:    "non-array child payload",
			input:   `{"hq-wisp-root":1,"schema_version":1}`,
			wantErr: true,
		},
		{
			name:    "metadata only is not silent skip-all",
			input:   `{"schema_version":1}`,
			wantErr: true,
		},
		{
			name:    "empty object is not silent skip-all",
			input:   `{}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseChildrenJSON(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			gotIDs := make([]string, 0, len(got))
			for _, child := range got {
				gotIDs = append(gotIDs, child.ID)
			}
			if !reflect.DeepEqual(gotIDs, tt.wantIDs) {
				t.Errorf("got child IDs %v, want %v", gotIDs, tt.wantIDs)
			}
		})
	}
}

func TestDogMolGracefulDegradation(t *testing.T) {
	// A dogMol with empty rootID should be a no-op for all operations.
	dm := &dogMol{
		rootID:  "",
		stepIDs: make(map[string]string),
	}

	// These should not panic or error — graceful degradation.
	dm.closeStep("scan")
	dm.failStep("scan", "test failure")
	dm.close()
}
