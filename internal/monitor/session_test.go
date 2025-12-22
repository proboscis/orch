package monitor

import (
	"strings"
	"testing"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
)

type stubStore struct {
	vault string
}

func (s *stubStore) ResolveIssue(issueID string) (*model.Issue, error) {
	return nil, nil
}

func (s *stubStore) ListIssues() ([]*model.Issue, error) {
	return nil, nil
}

func (s *stubStore) SetIssueStatus(issueID string, status model.IssueStatus) error {
	return nil
}

func (s *stubStore) CreateRun(issueID, runID string, metadata map[string]string) (*model.Run, error) {
	return nil, nil
}

func (s *stubStore) AppendEvent(ref *model.RunRef, event *model.Event) error {
	return nil
}

func (s *stubStore) ListRuns(filter *store.ListRunsFilter) ([]*model.Run, error) {
	return nil, nil
}

func (s *stubStore) GetRun(ref *model.RunRef) (*model.Run, error) {
	return nil, nil
}

func (s *stubStore) GetRunByShortID(shortID string) (*model.Run, error) {
	return nil, nil
}

func (s *stubStore) GetLatestRun(issueID string) (*model.Run, error) {
	return nil, nil
}

func (s *stubStore) VaultPath() string {
	return s.vault
}

func TestMonitorSessionNameUsesVaultHash(t *testing.T) {
	st := &stubStore{vault: "/tmp/orch/vault"}
	m := New(st, Options{})

	if !strings.HasPrefix(m.session, defaultSessionName+"-") {
		t.Fatalf("session = %q, expected prefix %q", m.session, defaultSessionName+"-")
	}
	if m.session != monitorSessionNameForVault(st.vault) {
		t.Fatalf("session = %q, want %q", m.session, monitorSessionNameForVault(st.vault))
	}
}

func TestMonitorSessionNameFallsBackToDefault(t *testing.T) {
	st := &stubStore{vault: ""}
	m := New(st, Options{})

	if m.session != defaultSessionName {
		t.Fatalf("session = %q, want %q", m.session, defaultSessionName)
	}
}

func TestMonitorSessionNameRespectsOverride(t *testing.T) {
	st := &stubStore{vault: "/tmp/orch/vault"}
	m := New(st, Options{Session: "custom-session"})

	if m.session != "custom-session" {
		t.Fatalf("session = %q, want %q", m.session, "custom-session")
	}
}
