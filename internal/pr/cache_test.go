package pr

import (
	"testing"

	"github.com/s22625/orch/internal/model"
)

type fakeGitHubClient struct {
	available bool
	output    []byte
	err       error

	lastDir  string
	lastArgs []string
}

func (f *fakeGitHubClient) IsAvailable() bool {
	return f.available
}

func (f *fakeGitHubClient) Run(args ...string) ([]byte, error) {
	f.lastArgs = append([]string(nil), args...)
	return f.output, f.err
}

func (f *fakeGitHubClient) RunInDir(dir string, args ...string) ([]byte, error) {
	f.lastDir = dir
	f.lastArgs = append([]string(nil), args...)
	return f.output, f.err
}

func TestLookupInfoWithClient_UsesInjectedClient(t *testing.T) {
	client := &fakeGitHubClient{
		available: true,
		output:    []byte(`[{"url":"https://github.com/acme/repo/pull/7","number":7,"state":"OPEN"}]`),
	}

	info, err := LookupInfoWithClient(client, "/tmp/repo", "feature/test")
	if err != nil {
		t.Fatalf("LookupInfoWithClient returned error: %v", err)
	}
	if info == nil {
		t.Fatalf("LookupInfoWithClient returned nil info")
	}
	if info.URL != "https://github.com/acme/repo/pull/7" {
		t.Fatalf("info.URL = %q", info.URL)
	}
	if info.Number != 7 {
		t.Fatalf("info.Number = %d", info.Number)
	}
	if info.State != "OPEN" {
		t.Fatalf("info.State = %q", info.State)
	}
	if client.lastDir != "/tmp/repo" {
		t.Fatalf("RunInDir dir = %q", client.lastDir)
	}
}

func TestPopulateRunInfoWithClient_UsesInjectedClient(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	client := &fakeGitHubClient{
		available: true,
		output:    []byte(`[{"url":"https://github.com/acme/repo/pull/19","number":19,"state":"OPEN"}]`),
	}

	runs := []*model.Run{
		{
			IssueID: "orch-446",
			RunID:   "run-1",
			Branch:  "feature/cache",
		},
	}

	infoMap := PopulateRunInfoWithClient(client, runs)
	info := infoMap["feature/cache"]
	if info == nil {
		t.Fatalf("expected PR info for branch")
	}
	if info.URL != "https://github.com/acme/repo/pull/19" {
		t.Fatalf("info.URL = %q", info.URL)
	}
	if runs[0].PRUrl != "https://github.com/acme/repo/pull/19" {
		t.Fatalf("run.PRUrl = %q", runs[0].PRUrl)
	}
}
