package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/orchapi"
	"github.com/proboscis/orch/internal/testutil"
)

func TestNewIssueCreateCmdLongDescriptionGuidance(t *testing.T) {
	cmd := newIssueCreateCmd()

	if !strings.Contains(cmd.Long, "stdin (heredoc)") {
		t.Fatalf("expected issue create help to mention stdin usage")
	}
	if !strings.Contains(cmd.Long, "<<'EOF'") {
		t.Fatalf("expected issue create help to include heredoc example")
	}
	if !strings.Contains(cmd.Long, "ONLY context") {
		t.Fatalf("expected issue create help to teach that the body is the worker's only context")
	}
	if !strings.Contains(cmd.Long, "--edit") {
		t.Fatalf("expected issue create help to lead humans to $EDITOR via --edit")
	}
}

func TestResolveIssueCreateInputReadsStdinBody(t *testing.T) {
	title, opts, err := resolveIssueCreateInput("issue-1", &issueCreateOptions{
		Title: "Issue title",
	}, bytes.NewBufferString("line one\nline two\n"), false)
	if err != nil {
		t.Fatalf("resolveIssueCreateInput() error = %v, want nil", err)
	}
	if title != "Issue title" {
		t.Fatalf("title = %q, want %q", title, "Issue title")
	}
	if opts.Body != "line one\nline two" {
		t.Fatalf("body = %q, want %q", opts.Body, "line one\nline two")
	}
}

func TestResolveIssueCreateInputPrefersBodyFlagOverStdin(t *testing.T) {
	title, opts, err := resolveIssueCreateInput("issue-1", &issueCreateOptions{
		Title:        "Issue title",
		Body:         "flag body",
		bodyProvided: true,
	}, bytes.NewBufferString("stdin body\n"), false)
	if err != nil {
		t.Fatalf("resolveIssueCreateInput() error = %v, want nil", err)
	}
	if title != "Issue title" {
		t.Fatalf("title = %q, want %q", title, "Issue title")
	}
	if opts.Body != "flag body" {
		t.Fatalf("body = %q, want %q", opts.Body, "flag body")
	}
}

func TestResolveIssueCreateInputPrefersExplicitEmptyBodyOverStdin(t *testing.T) {
	title, opts, err := resolveIssueCreateInput("issue-1", &issueCreateOptions{
		Title:        "Issue title",
		Body:         "",
		bodyProvided: true,
	}, bytes.NewBufferString("stdin body\n"), false)
	if err != nil {
		t.Fatalf("resolveIssueCreateInput() error = %v, want nil", err)
	}
	if title != "Issue title" {
		t.Fatalf("title = %q, want %q", title, "Issue title")
	}
	if opts.Body != "" {
		t.Fatalf("body = %q, want empty", opts.Body)
	}
}

func TestResolveIssueCreateInputPromptsForTitleOnTTY(t *testing.T) {
	title, opts, err := resolveIssueCreateInput("", &issueCreateOptions{}, bytes.NewBufferString("Prompted title\n"), true)
	if err != nil {
		t.Fatalf("resolveIssueCreateInput() error = %v, want nil", err)
	}
	if title != "Prompted title" {
		t.Fatalf("title = %q, want %q", title, "Prompted title")
	}
	if opts.Body != "" {
		t.Fatalf("body = %q, want empty", opts.Body)
	}
}

func TestResolveIssueCreateInputRejectsMissingNonInteractiveTitle(t *testing.T) {
	_, _, err := resolveIssueCreateInput("", &issueCreateOptions{}, bytes.NewBufferString("body from stdin\n"), false)
	if err == nil {
		t.Fatal("resolveIssueCreateInput() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "title required") {
		t.Fatalf("expected title guidance, got %v", err)
	}
}

func TestValidateIssueCreateOptionsRejectsGitHubEdit(t *testing.T) {
	err := validateIssueCreateOptions(&issueCreateOptions{Edit: true}, &orchapi.Config{
		Issues: orchapi.IssuesConfig{Backend: "github"},
	})
	if err == nil {
		t.Fatal("validateIssueCreateOptions() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "--edit flag is not supported with GitHub backend") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunIssueCreateWithEditorUsesCreateAndUpdateVerbsOnly(t *testing.T) {
	const (
		issueID   = "create-edit-test"
		storePath = "/remote/store/Issues/create-edit-test.md"
	)
	var calls []string
	var updateBody *string
	api := &testutil.OrchAPIMock{
		CreateIssueFunc: func(_ context.Context, req *orchapi.CreateIssueRequest) (*orchapi.Issue, error) {
			calls = append(calls, "create")
			if req.ID != issueID || req.Title != "Create Edit" || req.Body != "seed body" {
				t.Fatalf("CreateIssue request = %#v", req)
			}
			return &orchapi.Issue{ID: req.ID, Path: storePath}, nil
		},
		GetIssueFunc: func(_ context.Context, gotID model.IssueID) (*orchapi.Issue, error) {
			calls = append(calls, "get")
			if gotID != issueID {
				t.Fatalf("GetIssue(%q), want %q", gotID, issueID)
			}
			return &orchapi.Issue{ID: gotID, Path: storePath, Body: "# Create Edit\n\nseed body\n"}, nil
		},
		UpdateIssueFunc: func(_ context.Context, gotID model.IssueID, req *orchapi.UpdateIssueRequest) (*orchapi.Issue, error) {
			calls = append(calls, "update")
			if gotID != issueID {
				t.Fatalf("UpdateIssue(%q), want %q", gotID, issueID)
			}
			updateBody = req.Body
			return &orchapi.Issue{ID: gotID}, nil
		},
		WriteFileFunc: func(context.Context, string, []byte, uint32) error {
			t.Fatal("WriteFile must not be used for issue create --edit")
			return nil
		},
	}

	previous := *globalOpts
	globalOpts.Quiet = true
	t.Cleanup(func() { *globalOpts = previous })
	editor := func(path string) error {
		if path == storePath {
			t.Fatalf("editor received store path %q", path)
		}
		return os.WriteFile(path, []byte("# Create Edit\n\nedited body\n"), 0600)
	}

	err := runIssueCreateWithEditorUsing(api, issueID, "Create Edit", &issueCreateOptions{
		Body: "seed body",
		Edit: true,
	}, editor)
	if err != nil {
		t.Fatalf("runIssueCreateWithEditorUsing() error = %v", err)
	}
	if strings.Join(calls, ",") != "create,get,update" {
		t.Fatalf("API calls = %v, want CreateIssue -> GetIssue -> UpdateIssue", calls)
	}
	if updateBody == nil || *updateBody != "# Create Edit\n\nedited body\n" {
		t.Fatalf("UpdateIssue body = %#v", updateBody)
	}
}

func setIssueRootConfig(t *testing.T, issuesRoot string) {
	t.Helper()

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".orch"), 0o755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	configBody := []byte("issues:\n  path: " + issuesRoot + "\n")
	if err := os.WriteFile(filepath.Join(repo, ".orch", "config.yaml"), configBody, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
}

func TestRunIssueCreatePrefersExistingIssuesDir(t *testing.T) {
	vault := t.TempDir()
	issuesDir := filepath.Join(vault, "Issues")
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		t.Fatalf("mkdir Issues: %v", err)
	}

	prev := *globalOpts
	globalOpts.JSON = false
	globalOpts.Quiet = true
	setIssueRootConfig(t, vault)
	testBypassDaemon = true
	t.Cleanup(func() {
		*globalOpts = prev
		testBypassDaemon = false
	})

	issueID := "issue-123"
	opts := &issueCreateOptions{Title: "Test Issue"}
	if err := runIssueCreate(issueID, opts); err != nil {
		t.Fatalf("runIssueCreate: %v", err)
	}

	expected := filepath.Join(issuesDir, issueID+".md")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected issue at %q: %v", expected, err)
	}
}

func TestRunIssueCreateUsesVaultIssuesDir(t *testing.T) {
	vault := t.TempDir()
	issuesDir := filepath.Join(vault, "Issues")
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		t.Fatalf("mkdir Issues: %v", err)
	}

	prev := *globalOpts
	globalOpts.JSON = false
	globalOpts.Quiet = true
	setIssueRootConfig(t, issuesDir)
	testBypassDaemon = true
	t.Cleanup(func() {
		*globalOpts = prev
		testBypassDaemon = false
	})

	issueID := "issue-456"
	opts := &issueCreateOptions{Title: "Test Issue"}
	if err := runIssueCreate(issueID, opts); err != nil {
		t.Fatalf("runIssueCreate: %v", err)
	}

	expected := filepath.Join(issuesDir, issueID+".md")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected issue at %q: %v", expected, err)
	}
}

func TestRunIssueCreateWithInputUsesRedirectedStdinAsBody(t *testing.T) {
	vault := t.TempDir()
	issuesDir := filepath.Join(vault, "Issues")
	if err := os.MkdirAll(issuesDir, 0o755); err != nil {
		t.Fatalf("mkdir Issues: %v", err)
	}

	prev := *globalOpts
	globalOpts.JSON = false
	globalOpts.Quiet = true
	setIssueRootConfig(t, vault)
	testBypassDaemon = true
	t.Cleanup(func() {
		*globalOpts = prev
		testBypassDaemon = false
	})

	issueID := "issue-stdin"
	if err := runIssueCreateWithInput(issueID, &issueCreateOptions{}, bytes.NewBufferString("line one\nline two\n"), false); err != nil {
		t.Fatalf("runIssueCreateWithInput: %v", err)
	}

	issuePath := filepath.Join(issuesDir, issueID+".md")
	content, err := os.ReadFile(issuePath)
	if err != nil {
		t.Fatalf("read issue file: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "title: issue-stdin") {
		t.Fatalf("issue file missing default title\ngot:\n%s", text)
	}
	if !strings.Contains(text, "line one\nline two\n") {
		t.Fatalf("issue file missing redirected stdin body\ngot:\n%s", text)
	}
}

func TestIssueCreateCmdExplicitEmptyBodySkipsRedirectedStdin(t *testing.T) {
	vault := t.TempDir()
	issuesDir := filepath.Join(vault, "Issues")
	if err := os.MkdirAll(issuesDir, 0o755); err != nil {
		t.Fatalf("mkdir Issues: %v", err)
	}

	stdinFile, err := os.CreateTemp(t.TempDir(), "issue-create-stdin-*.txt")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := stdinFile.WriteString("stdin body should be ignored\n"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if _, err := stdinFile.Seek(0, 0); err != nil {
		t.Fatalf("Seek: %v", err)
	}

	prevStdin := os.Stdin
	os.Stdin = stdinFile
	t.Cleanup(func() { os.Stdin = prevStdin })
	t.Cleanup(func() { _ = stdinFile.Close() })

	prev := *globalOpts
	globalOpts.JSON = false
	globalOpts.Quiet = true
	setIssueRootConfig(t, vault)
	testBypassDaemon = true
	t.Cleanup(func() {
		*globalOpts = prev
		testBypassDaemon = false
	})

	cmd := newIssueCreateCmd()
	cmd.SetArgs([]string{"issue-empty-body", "--title", "Issue title", "--body", ""})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cmd.Execute: %v", err)
	}

	issuePath := filepath.Join(issuesDir, "issue-empty-body.md")
	content, err := os.ReadFile(issuePath)
	if err != nil {
		t.Fatalf("read issue file: %v", err)
	}

	text := string(content)
	if strings.Contains(text, "stdin body should be ignored") {
		t.Fatalf("issue file unexpectedly consumed redirected stdin\ngot:\n%s", text)
	}
	if !strings.Contains(text, "# Issue title\n\n") {
		t.Fatalf("issue file missing title header\ngot:\n%s", text)
	}
}

func TestRunIssueEditAppendBodyFromStdinUsesDaemonUpdate(t *testing.T) {
	var gotIssueID model.IssueID
	var gotRequest *orchapi.UpdateIssueRequest
	api := &testutil.OrchAPIMock{
		UpdateIssueFunc: func(_ context.Context, issueID model.IssueID, req *orchapi.UpdateIssueRequest) (*orchapi.Issue, error) {
			gotIssueID = issueID
			gotRequest = req
			return &orchapi.Issue{ID: issueID}, nil
		},
	}
	editorCalled := false
	err := runIssueEditWithInput("local-issue", &issueEditOptions{
		AppendBody:         "-",
		appendBodyProvided: true,
	}, bytes.NewBufferString("\n## Note\nremote-safe\n"), api, func(string) error {
		editorCalled = true
		return nil
	})
	if err != nil {
		t.Fatalf("runIssueEditWithInput() error = %v", err)
	}
	if editorCalled {
		t.Fatal("editor was called for non-interactive append")
	}
	if gotIssueID != "local-issue" || gotRequest == nil || gotRequest.AppendBody == nil {
		t.Fatalf("daemon update = issue %q request %#v", gotIssueID, gotRequest)
	}
	if got := *gotRequest.AppendBody; got != "\n## Note\nremote-safe\n" {
		t.Fatalf("append body = %q", got)
	}
}

func TestRunIssueEditInteractiveUsesTemporaryCopyAndDaemonUpdate(t *testing.T) {
	issue := &orchapi.Issue{
		ID:   "local-issue",
		Body: "original body\n",
		Path: "file:///remote/store/Issues/local-issue.md",
	}
	var gotBody *string
	api := &testutil.OrchAPIMock{
		GetIssueFunc: func(_ context.Context, issueID model.IssueID) (*orchapi.Issue, error) {
			if issueID != issue.ID {
				t.Fatalf("GetIssue(%q), want %q", issueID, issue.ID)
			}
			return issue, nil
		},
		UpdateIssueFunc: func(_ context.Context, issueID model.IssueID, req *orchapi.UpdateIssueRequest) (*orchapi.Issue, error) {
			if issueID != issue.ID {
				t.Fatalf("UpdateIssue(%q), want %q", issueID, issue.ID)
			}
			gotBody = req.Body
			return issue, nil
		},
	}

	var tempPath string
	editor := func(path string) error {
		tempPath = path
		if path == strings.TrimPrefix(issue.Path, "file://") {
			t.Fatalf("editor received store path %q", path)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if string(content) != issue.Body {
			t.Fatalf("temporary body = %q, want %q", content, issue.Body)
		}
		return os.WriteFile(path, []byte("edited body\n"), 0600)
	}

	if err := runIssueEditWithInput("local-issue", &issueEditOptions{}, strings.NewReader(""), api, editor); err != nil {
		t.Fatalf("runIssueEditWithInput() error = %v", err)
	}
	if gotBody == nil || *gotBody != "edited body\n" {
		t.Fatalf("daemon replacement body = %#v", gotBody)
	}
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Fatalf("temporary file still exists at %q: %v", tempPath, err)
	}
}

func TestMatchTagsAnd(t *testing.T) {
	tests := []struct {
		name       string
		issueTags  []string
		filterTags []string
		want       bool
	}{
		{"empty filter", []string{"bug", "feature"}, nil, true},
		{"match all", []string{"bug", "feature", "urgent"}, []string{"bug", "feature"}, true},
		{"missing one", []string{"bug"}, []string{"bug", "feature"}, false},
		{"case insensitive", []string{"Bug", "FEATURE"}, []string{"bug", "feature"}, true},
		{"empty issue tags", nil, []string{"bug"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchTagsAnd(tt.issueTags, tt.filterTags); got != tt.want {
				t.Errorf("matchTagsAnd() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchTagsOr(t *testing.T) {
	tests := []struct {
		name       string
		issueTags  []string
		filterTags []string
		want       bool
	}{
		{"empty filter", []string{"bug", "feature"}, nil, true},
		{"match one", []string{"bug"}, []string{"bug", "feature"}, true},
		{"match none", []string{"urgent"}, []string{"bug", "feature"}, false},
		{"case insensitive", []string{"BUG"}, []string{"bug", "feature"}, true},
		{"empty issue tags", nil, []string{"bug"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchTagsOr(tt.issueTags, tt.filterTags); got != tt.want {
				t.Errorf("matchTagsOr() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunIssueCreateQuotesSpecialCharacters(t *testing.T) {
	vault := t.TempDir()
	issuesDir := filepath.Join(vault, "issues")
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		t.Fatalf("mkdir issues: %v", err)
	}

	prev := *globalOpts
	globalOpts.JSON = false
	globalOpts.Quiet = true
	setIssueRootConfig(t, vault)
	testBypassDaemon = true
	t.Cleanup(func() {
		*globalOpts = prev
		testBypassDaemon = false
	})

	tests := []struct {
		name    string
		title   string
		summary string
		wantIn  []string
	}{
		{
			name:   "colon in title",
			title:  "Test: with colon",
			wantIn: []string{`title: "Test: with colon"`},
		},
		{
			name:    "colon in summary",
			title:   "Simple title",
			summary: "Summary: has colon",
			wantIn:  []string{"title: Simple title", `summary: "Summary: has colon"`},
		},
		{
			name:   "hash in title",
			title:  "Test # with hash",
			wantIn: []string{`title: "Test # with hash"`},
		},
		{
			name:   "simple title needs no quotes",
			title:  "Simple title",
			wantIn: []string{"title: Simple title"},
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issueID := filepath.Base(t.Name()) + "-" + string(rune('a'+i))
			opts := &issueCreateOptions{Title: tt.title, Summary: tt.summary}
			if err := runIssueCreate(issueID, opts); err != nil {
				t.Fatalf("runIssueCreate: %v", err)
			}

			issuePath := filepath.Join(issuesDir, issueID+".md")
			content, err := os.ReadFile(issuePath)
			if err != nil {
				t.Fatalf("read issue file: %v", err)
			}

			for _, want := range tt.wantIn {
				if !strings.Contains(string(content), want) {
					t.Errorf("issue file missing %q\ngot:\n%s", want, content)
				}
			}
		})
	}
}

func TestMatchIssueFilters(t *testing.T) {
	tests := []struct {
		name   string
		status string
		tags   []string
		opts   *issueListOptions
		want   bool
	}{
		{"no filters", "open", []string{"bug"}, &issueListOptions{}, true},
		{"status match", "open", nil, &issueListOptions{Status: "open"}, true},
		{"status no match", "closed", nil, &issueListOptions{Status: "open"}, false},
		{"status case insensitive", "OPEN", nil, &issueListOptions{Status: "open"}, true},
		{"tag and match", "open", []string{"bug", "urgent"}, &issueListOptions{Tags: []string{"bug", "urgent"}}, true},
		{"tag and no match", "open", []string{"bug"}, &issueListOptions{Tags: []string{"bug", "urgent"}}, false},
		{"tag or match", "open", []string{"bug"}, &issueListOptions{TagsAny: []string{"bug", "feature"}}, true},
		{"tag or no match", "open", []string{"urgent"}, &issueListOptions{TagsAny: []string{"bug", "feature"}}, false},
		{"combined filters", "open", []string{"bug", "urgent"}, &issueListOptions{Status: "open", Tags: []string{"bug"}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchIssueFilters(tt.status, tt.tags, tt.opts); got != tt.want {
				t.Errorf("matchIssueFilters() = %v, want %v", got, tt.want)
			}
		})
	}
}
