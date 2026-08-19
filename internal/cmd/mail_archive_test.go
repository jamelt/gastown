package cmd

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/mail"
)

type recordingArchiver struct {
	errors map[string]error
	seen   []string
}

func (a *recordingArchiver) Archive(id string) error {
	a.seen = append(a.seen, id)
	return a.errors[id]
}

func TestArchiveMessageIDsSurfacesPartialFailureWithoutExpandingTargets(t *testing.T) {
	wantIDs := []string{"mail-ok-1", "mail-failed", "mail-gcd", "mail-ok-2"}
	archiver := &recordingArchiver{errors: map[string]error{
		"mail-failed": errors.New("mutation failed"),
		"mail-gcd":    mail.ErrMessageNotFound,
	}}

	archived, gcd, errMsgs := archiveMessageIDs(archiver, wantIDs)
	if archived != 2 || gcd != 1 {
		t.Fatalf("archive counts = archived:%d gcd:%d, want 2/1", archived, gcd)
	}
	if len(errMsgs) != 1 || errMsgs[0] != "mail-failed: mutation failed" {
		t.Fatalf("errors = %#v, want surfaced mail-failed error", errMsgs)
	}
	if !reflect.DeepEqual(archiver.seen, wantIDs) {
		t.Fatalf("archive targets = %#v, want exact requested IDs %#v", archiver.seen, wantIDs)
	}
}

func TestStaleMessagesForSession(t *testing.T) {
	sessionStart := time.Date(2026, 1, 24, 2, 0, 0, 0, time.UTC)
	messages := []*mail.Message{
		{ID: "msg-1", Subject: "Older", Timestamp: sessionStart.Add(-2 * time.Minute)},
		{ID: "msg-2", Subject: "Newer", Timestamp: sessionStart.Add(2 * time.Minute)},
		{ID: "msg-3", Subject: "Equal", Timestamp: sessionStart},
	}

	stale := staleMessagesForSession(messages, sessionStart)
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale message, got %d", len(stale))
	}
	if stale[0].Message.ID != "msg-1" {
		t.Fatalf("expected msg-1 stale, got %s", stale[0].Message.ID)
	}
}
