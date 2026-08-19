package mail

import (
	"context"
	"fmt"
	"testing"

	beadsdk "github.com/steveyegge/beads"
)

type actorCheckingMailStore struct {
	beadsdk.Storage
	issue            *beadsdk.Issue
	successfulActors []string
	deniedActors     []string
}

func (s *actorCheckingMailStore) GetIssue(_ context.Context, id string) (*beadsdk.Issue, error) {
	if s.issue == nil || s.issue.ID != id {
		return nil, fmt.Errorf("issue %s not found", id)
	}
	return s.issue, nil
}

func (s *actorCheckingMailStore) AddLabel(_ context.Context, id, label, actor string) error {
	if s.issue == nil || s.issue.ID != id {
		return fmt.Errorf("issue %s not found", id)
	}
	if actor != s.issue.Assignee {
		s.deniedActors = append(s.deniedActors, actor)
		return fmt.Errorf("assignee is %s, actor is %s", s.issue.Assignee, actor)
	}
	s.successfulActors = append(s.successfulActors, actor)
	s.issue.Labels = append(s.issue.Labels, label)
	return nil
}

func (s *actorCheckingMailStore) RemoveLabel(_ context.Context, id, label, actor string) error {
	if s.issue == nil || s.issue.ID != id {
		return fmt.Errorf("issue %s not found", id)
	}
	if actor != s.issue.Assignee {
		s.deniedActors = append(s.deniedActors, actor)
		return fmt.Errorf("assignee is %s, actor is %s", s.issue.Assignee, actor)
	}
	s.successfulActors = append(s.successfulActors, actor)
	for i, existing := range s.issue.Labels {
		if existing == label {
			s.issue.Labels = append(s.issue.Labels[:i], s.issue.Labels[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("does not have label %s", label)
}

func (s *actorCheckingMailStore) CloseIssue(_ context.Context, id, _, actor, _ string) error {
	if s.issue == nil || s.issue.ID != id {
		return fmt.Errorf("issue %s not found", id)
	}
	if actor != s.issue.Assignee {
		s.deniedActors = append(s.deniedActors, actor)
		return fmt.Errorf("assignee is %s, actor is %s", s.issue.Assignee, actor)
	}
	s.successfulActors = append(s.successfulActors, actor)
	return nil
}

func TestMailboxStoreUsesCanonicalMutationActor(t *testing.T) {
	store := &actorCheckingMailStore{issue: &beadsdk.Issue{
		ID:       "mail-deacon",
		Assignee: "deacon/",
		Status:   beadsdk.StatusOpen,
		Labels:   []string{"gt:message", DeliveryLabelPending},
	}}

	deacon := NewMailboxWithBeadsDirAndStore("deacon", t.TempDir(), t.TempDir(), store)
	if err := deacon.Archive("mail-deacon"); err != nil {
		t.Fatalf("Deacon Archive error: %v", err)
	}
	if err := deacon.Delete("mail-deacon"); err != nil {
		t.Fatalf("Deacon Delete error: %v", err)
	}
	for _, actor := range store.successfulActors {
		if actor != "deacon/" {
			t.Fatalf("successful mutation actor = %q, want deacon/", actor)
		}
	}

	unrelated := NewMailboxWithBeadsDirAndStore("gastown/witness", t.TempDir(), t.TempDir(), store)
	if err := unrelated.Archive("mail-deacon"); err == nil {
		t.Fatal("unrelated Archive succeeded")
	}
	if err := unrelated.Delete("mail-deacon"); err == nil {
		t.Fatal("unrelated Delete succeeded")
	}
	if len(store.deniedActors) != 1 || store.deniedActors[0] != "gastown/witness" {
		t.Fatalf("denied actors = %v, want [gastown/witness]", store.deniedActors)
	}
}
