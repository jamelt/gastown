package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/formula"
)

// TestDeaconPatrolStaticGTCommandsStayCompatible keeps the embedded patrol
// instructions and the CLI command tree in one compatibility contract. It
// validates command paths and flags without running operational side effects.
func TestDeaconPatrolStaticGTCommandsStayCompatible(t *testing.T) {
	content, err := formula.GetEmbeddedFormulaContent("mol-deacon-patrol")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := formula.Parse(content)
	if err != nil {
		t.Fatal(err)
	}

	invocations := deaconPatrolGTInvocations(parsed)
	if len(invocations) == 0 {
		t.Fatal("no static gt invocations found in mol-deacon-patrol")
	}
	for _, invocation := range invocations {
		t.Run(strings.ReplaceAll(invocation, " ", "_"), func(t *testing.T) {
			validateStaticGTInvocation(t, invocation)
		})
	}
}

func deaconPatrolGTInvocations(parsed *formula.Formula) []string {
	var invocations []string
	for _, step := range parsed.Steps {
		inBashBlock := false
		var logicalLine string
		for _, rawLine := range strings.Split(step.Description, "\n") {
			line := strings.TrimSpace(rawLine)
			if strings.HasPrefix(line, "```") {
				if inBashBlock && logicalLine != "" {
					invocations = appendStaticGTInvocation(invocations, logicalLine)
					logicalLine = ""
				}
				inBashBlock = line == "```bash" || line == "```sh" || line == "```shell"
				continue
			}
			if !inBashBlock || line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if logicalLine != "" {
				logicalLine += " " + line
			} else {
				logicalLine = line
			}
			if strings.HasSuffix(logicalLine, "\\") {
				logicalLine = strings.TrimSpace(strings.TrimSuffix(logicalLine, "\\"))
				continue
			}
			invocations = appendStaticGTInvocation(invocations, logicalLine)
			logicalLine = ""
		}
	}
	return invocations
}

func appendStaticGTInvocation(invocations []string, line string) []string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "gt ") {
		return invocations
	}
	fields := strings.Fields(line)
	if len(fields) < 2 || strings.ContainsAny(fields[1], "$<{(") {
		return invocations
	}
	return append(invocations, line)
}

func validateStaticGTInvocation(t *testing.T, invocation string) {
	t.Helper()
	fields := strings.Fields(invocation)
	command, _, err := rootCmd.Find(fields[1:])
	if err != nil {
		t.Fatalf("unsupported command in %q: %v", invocation, err)
	}

	for _, token := range fields[1:] {
		if token == "|" || token == "||" || token == "&&" || token == ";" {
			break
		}
		if strings.HasPrefix(token, "--") {
			name := strings.TrimPrefix(strings.SplitN(token, "=", 2)[0], "--")
			if name != "" && !commandHasFlag(command, name, "") {
				t.Errorf("unsupported flag --%s on %q", name, invocation)
			}
			continue
		}
		if strings.HasPrefix(token, "-") && len(token) == 2 {
			if !commandHasFlag(command, "", token[1:]) {
				t.Errorf("unsupported shorthand flag %s on %q", token, invocation)
			}
		}
	}
}

func commandHasFlag(command *cobra.Command, name, shorthand string) bool {
	for current := command; current != nil; current = current.Parent() {
		if name != "" && (current.Flags().Lookup(name) != nil || current.PersistentFlags().Lookup(name) != nil) {
			return true
		}
		if shorthand != "" && (current.Flags().ShorthandLookup(shorthand) != nil || current.PersistentFlags().ShorthandLookup(shorthand) != nil) {
			return true
		}
	}
	return false
}
