package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// InstallHangingBinaryStub writes a fake `binName` executable that hangs
// (via exec, so killing the tracked pid actually stops it) on any invocation
// whose arguments contain hangOnSubstring, and passes everything else
// through to the real binary of that name captured before PATH is swapped.
// Skips the test on Windows (unix shell stub).
//
// Used to reproduce a caller hanging on an external subprocess that has no
// timeout of its own — see gt-vyik, where this pattern proved that
// previously-unbounded git and gt subprocess calls in the scheduler dispatch
// path are now bounded.
func InstallHangingBinaryStub(t *testing.T, binName, hangOnSubstring string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix shell stub")
	}
	realBin, err := exec.LookPath(binName)
	if err != nil {
		t.Fatalf("finding real %s: %v", binName, err)
	}
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, binName)
	script := "#!/bin/sh\ncase \"$*\" in\n  *" + hangOnSubstring + "*)\n    exec sleep 300\n    ;;\n  *)\n    exec " + realBin + " \"$@\"\n    ;;\nesac\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake %s: %v", binName, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
