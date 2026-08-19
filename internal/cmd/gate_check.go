package cmd

import (
	"fmt"
	"os"
	"strings"
)

// enforceGateMarkerInBinary verifies the binary contains the hard-prohibition gate enforcement marker.
// context identifies where the check is running (e.g., "scheduler", "daemon") for error messages.
func enforceGateMarkerInBinary(binaryPath string) error {
	data, err := os.ReadFile(binaryPath)
	if err != nil {
		return fmt.Errorf("cannot enforce gate marker: cannot read binary %s: %w", binaryPath, err)
	}

	if !strings.Contains(string(data), "confirm-human-approved") {
		return fmt.Errorf("REFUSAL: cannot start with this binary\n" +
			"The binary at %s does not contain the hard-prohibition gate enforcement marker.\n" +
			"The gate commits are on main; the binary must contain evidence of their enforcement.\n" +
			"This is a deployment-time control per decision gt-i4bn (ACCEPT option A).\n" +
			"Do NOT rebuild from diverged or uncommitted state. Use: git checkout main && make install",
			binaryPath)
	}

	return nil
}
