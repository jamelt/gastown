package nudge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/constants"
)

// ReapResult summarizes a nudge-queue garbage-collection pass.
type ReapResult struct {
	ExpiredRemoved  int // queued nudges past their ExpiresAt
	OrphanedClaims  int // .claimed files left behind by a crashed drainer
	DeadSessionDirs int // queue directories for sessions that no longer exist
}

// Reap removes nudge-queue residue that Drain has no opportunity to clean up:
// an idle session that takes no further turns never calls Drain, so its
// expired entries and orphaned claims sit in the queue directory forever
// (gt-lp89).
//
// liveSessions, when non-nil, gates removal of whole session directories
// that hold nothing but expired/orphaned residue and no longer correspond
// to a live session. Pass nil to skip dead-session directory cleanup (e.g.
// when the caller cannot cheaply enumerate live sessions).
func Reap(townRoot string, liveSessions map[string]bool) (ReapResult, error) {
	var result ReapResult

	queueRoot := filepath.Join(townRoot, constants.DirRuntime, "nudge_queue")
	sessionDirs, err := os.ReadDir(queueRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return result, fmt.Errorf("reading nudge queue root: %w", err)
	}

	now := time.Now()

	for _, sessionEntry := range sessionDirs {
		if !sessionEntry.IsDir() {
			continue
		}
		name := sessionEntry.Name()
		dir := filepath.Join(queueRoot, name)

		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		remaining := 0
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			fname := f.Name()
			path := filepath.Join(dir, fname)

			switch {
			case strings.Contains(fname, ".claimed"):
				info, infoErr := f.Info()
				if infoErr == nil && now.Sub(info.ModTime()) > staleClaimThreshold {
					if os.Remove(path) == nil {
						result.OrphanedClaims++
						continue
					}
				}
				remaining++

			case strings.HasSuffix(fname, ".json"):
				data, readErr := os.ReadFile(path)
				if readErr != nil {
					remaining++
					continue
				}
				var n QueuedNudge
				if json.Unmarshal(data, &n) == nil && !n.ExpiresAt.IsZero() && now.After(n.ExpiresAt) {
					if os.Remove(path) == nil {
						result.ExpiredRemoved++
						continue
					}
				}
				remaining++

			default:
				// Lock files (.lock, .unique.lock) are infrastructure, not
				// queued nudges — leave them out of the "remaining" count so
				// a session with nothing but a lock file is still eligible
				// for dead-session cleanup below.
			}
		}

		if liveSessions != nil && remaining == 0 && !liveSessions[name] {
			if os.RemoveAll(dir) == nil {
				result.DeadSessionDirs++
			}
		}
	}

	return result, nil
}
