package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	convoyops "github.com/steveyegge/gastown/internal/convoy"
)

func writeBdStub(binDir, convoyID string) (statePath, updateLogPath string) {
	statePath = filepath.Join(binDir, "notified.state")
	updateLogPath = filepath.Join(binDir, "update.log")

	bdScript := `#!/bin/sh
STATE="` + statePath + `"
UPDATE_LOG="` + updateLogPath + `"
case "$1" in
  show)
    if [ -f "$STATE" ]; then
      printf '%s\n' '[{"id":"` + convoyID + `","description":"Owner: mayor/\ncompletion_notified_at: 2026-05-25T02:30:00Z"}]'
    else
      printf '%s\n' '[{"id":"` + convoyID + `","description":"Owner: mayor/"}]'
    fi
    exit 0
    ;;
  update)
    echo update >> "$UPDATE_LOG"
    touch "$STATE"
    exit 0
    ;;
  export)
    exit 0
    ;;
esac
exit 0
`
	bdPath := filepath.Join(binDir, "bd")
	if err := os.WriteFile(bdPath, []byte(bdScript), 0755); err != nil {
		panic(err)
	}
	os.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return
}

func updateCount(updateLogPath string) int {
	data, err := os.ReadFile(updateLogPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		panic(err)
	}
	return strings.Count(string(data), "update")
}

func main() {
	fail := false

	// Test 1: second call is a no-op.
	func() {
		binDir1, _ := os.MkdirTemp("", "zzcheck1")
		defer os.RemoveAll(binDir1)
		townRoot1, _ := os.MkdirTemp("", "zzcheck1town")
		defer os.RemoveAll(townRoot1)
		_, updateLog1 := writeBdStub(binDir1, "hq-cv-claim")

		claimed, fields, err := convoyops.ClaimCompletionNotification(townRoot1, "hq-cv-claim")
		if err != nil {
			fmt.Println("FAIL test1: first claim error:", err)
			fail = true
			return
		}
		if !claimed {
			fmt.Println("FAIL test1: first claim should have succeeded")
			fail = true
			return
		}
		if fields == nil || fields.CompletionNotifiedAt == "" {
			fmt.Println("FAIL test1: first claim should return fields with CompletionNotifiedAt set")
			fail = true
			return
		}

		claimed2, _, err := convoyops.ClaimCompletionNotification(townRoot1, "hq-cv-claim")
		if err != nil {
			fmt.Println("FAIL test1: second claim error:", err)
			fail = true
			return
		}
		if claimed2 {
			fmt.Println("FAIL test1: second claim should be a no-op")
			fail = true
			return
		}
		if got := updateCount(updateLog1); got != 1 {
			fmt.Println("FAIL test1: bd update calls =", got, "want exactly 1")
			fail = true
			return
		}
		fmt.Println("PASS test1: second-call-is-noop")
	}()

	// Test 2: concurrent calls, exactly one wins.
	func() {
		binDir2, _ := os.MkdirTemp("", "zzcheck2")
		defer os.RemoveAll(binDir2)
		townRoot2, _ := os.MkdirTemp("", "zzcheck2town")
		defer os.RemoveAll(townRoot2)
		writeBdStub(binDir2, "hq-cv-race")

		const count = 10
		var wg sync.WaitGroup
		results := make([]bool, count)
		errs := make(chan error, count)

		for i := 0; i < count; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				claimed, _, err := convoyops.ClaimCompletionNotification(townRoot2, "hq-cv-race")
				if err != nil {
					errs <- err
					return
				}
				results[i] = claimed
			}(i)
		}
		wg.Wait()
		close(errs)

		for err := range errs {
			fmt.Println("FAIL test2: concurrent claim error:", err)
			fail = true
		}

		winners := 0
		for _, c := range results {
			if c {
				winners++
			}
		}
		if winners != 1 {
			fmt.Println("FAIL test2: winners =", winners, "want exactly 1")
			fail = true
			return
		}
		fmt.Println("PASS test2: concurrent-exactly-one-wins")
	}()

	if fail {
		os.Exit(1)
	}
	fmt.Println("ALL MANUAL CHECKS PASSED")
}
