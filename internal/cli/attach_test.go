package cli

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/multiplexer"
	"github.com/s22625/orch/internal/orchapi"
)

type mockAttachAPI struct {
	orchapi.OrchAPI
	info      *orchapi.AttachInfo
	attachErr error
	cfg       *orchapi.Config
}

func (m *mockAttachAPI) GetAttachInfo(ctx context.Context, ref orchapi.RunRef) (*orchapi.AttachInfo, error) {
	if m.attachErr != nil {
		return nil, m.attachErr
	}
	return m.info, nil
}

func (m *mockAttachAPI) GetConfig(ctx context.Context) (*orchapi.Config, error) {
	if m.cfg != nil {
		return m.cfg, nil
	}
	return &orchapi.Config{}, nil
}

type mockAttachMux struct {
	inside    bool
	switched  string
	attached  string
	switchErr error
	attachErr error
}

func (m *mockAttachMux) IsInsideSession() bool {
	return m.inside
}

func (m *mockAttachMux) SwitchClient(session string) error {
	m.switched = session
	return m.switchErr
}

func (m *mockAttachMux) AttachSession(session string) error {
	m.attached = session
	return m.attachErr
}

func newAttachDepsForTest(api orchapi.OrchAPI) (*attachDeps, *bytes.Buffer, *[]int) {
	stderr := &bytes.Buffer{}
	stdout := &bytes.Buffer{}
	exitCodes := []int{}

	deps := &attachDeps{
		getAPI: func() (orchapi.OrchAPI, error) { return api, nil },
		parseRunRef: func(string) (orchapi.RunRef, error) {
			return orchapi.RunRef{IssueID: "orch-1", RunID: "20260101-010101"}, nil
		},
		parseMuxType: multiplexer.ParseType,
		getMuxAuto: func() (attachSessionMux, error) {
			return nil, errors.New("mux auto not configured")
		},
		getMuxWithFallback: func(t multiplexer.Type) (attachSessionMux, string, error) {
			return nil, "", errors.New("mux fallback not configured")
		},
		attachOpenCode: attachOpenCodeFromInfoWithExecutor,
		attachRemote:   attachRemoteFromInfoWithExecutor,
		streams: attachStreams{
			stdin:  bytes.NewReader(nil),
			stdout: stdout,
			stderr: stderr,
		},
		exit: func(code int) {
			exitCodes = append(exitCodes, code)
		},
	}

	return deps, stderr, &exitCodes
}

func TestRunAttachWithDeps_RunNotFoundExits(t *testing.T) {
	deps, stderr, exitCodes := newAttachDepsForTest(&mockAttachAPI{attachErr: orchapi.ErrNotFound})

	err := runAttachWithDeps("orch-1#missing", &attachOptions{}, deps)
	if !errors.Is(err, orchapi.ErrNotFound) {
		t.Fatalf("runAttachWithDeps() error = %v, want ErrNotFound", err)
	}

	if got := *exitCodes; len(got) != 1 || got[0] != ExitRunNotFound {
		t.Fatalf("exit codes = %v, want [%d]", got, ExitRunNotFound)
	}

	if got := stderr.String(); !strings.Contains(got, "run not found: orch-1#missing") {
		t.Fatalf("stderr = %q, want run-not-found message", got)
	}
}

func TestRunAttachWithDeps_AttachSessionOutsideMux(t *testing.T) {
	api := &mockAttachAPI{
		info: &orchapi.AttachInfo{
			IssueID:       "orch-2",
			RunID:         "20260101-020202",
			Agent:         "claude",
			SessionName:   "orch-2-20260101-020202",
			SessionExists: true,
			Multiplexer:   orchapi.MultiplexerTmux,
		},
	}
	deps, _, exitCodes := newAttachDepsForTest(api)
	mux := &mockAttachMux{inside: false}
	deps.getMuxWithFallback = func(t multiplexer.Type) (attachSessionMux, string, error) {
		return mux, "", nil
	}

	err := runAttachWithDeps("orch-2#20260101-020202", &attachOptions{}, deps)
	if err != nil {
		t.Fatalf("runAttachWithDeps() error = %v, want nil", err)
	}
	if len(*exitCodes) != 0 {
		t.Fatalf("exit codes = %v, want none", *exitCodes)
	}
	if mux.attached != "orch-2-20260101-020202" {
		t.Fatalf("attached session = %q, want session name", mux.attached)
	}
}

func TestRunAttachWithDeps_SwitchesInsideMuxWithGeneratedSession(t *testing.T) {
	issueID := "orch-3"
	runID := "20260101-030303"
	api := &mockAttachAPI{
		info: &orchapi.AttachInfo{
			IssueID:       issueID,
			RunID:         runID,
			Agent:         "claude",
			SessionName:   "",
			SessionExists: true,
			Multiplexer:   orchapi.MultiplexerTmux,
		},
	}
	deps, _, exitCodes := newAttachDepsForTest(api)
	mux := &mockAttachMux{inside: true}
	deps.getMuxWithFallback = func(t multiplexer.Type) (attachSessionMux, string, error) {
		return mux, "", nil
	}

	err := runAttachWithDeps(issueID+"#"+runID, &attachOptions{}, deps)
	if err != nil {
		t.Fatalf("runAttachWithDeps() error = %v, want nil", err)
	}
	if len(*exitCodes) != 0 {
		t.Fatalf("exit codes = %v, want none", *exitCodes)
	}
	wantSession := model.GenerateSessionName(issueID, runID)
	if mux.switched != wantSession {
		t.Fatalf("switched session = %q, want %q", mux.switched, wantSession)
	}
}

func TestRunAttachWithDeps_NoMultiplexerExits(t *testing.T) {
	api := &mockAttachAPI{
		cfg: &orchapi.Config{AgentMultiplexer: "tmux"},
		info: &orchapi.AttachInfo{
			IssueID:       "orch-4",
			RunID:         "20260101-040404",
			Agent:         "claude",
			SessionName:   "orch-4-20260101-040404",
			SessionExists: true,
		},
	}
	deps, stderr, exitCodes := newAttachDepsForTest(api)
	deps.getMuxWithFallback = func(t multiplexer.Type) (attachSessionMux, string, error) {
		return nil, "", errors.New("tmux unavailable")
	}

	err := runAttachWithDeps("orch-4#20260101-040404", &attachOptions{}, deps)
	if err == nil || !strings.Contains(err.Error(), "tmux unavailable") {
		t.Fatalf("runAttachWithDeps() error = %v, want tmux unavailable", err)
	}
	if got := *exitCodes; len(got) != 1 || got[0] != ExitTmuxError {
		t.Fatalf("exit codes = %v, want [%d]", got, ExitTmuxError)
	}
	if got := stderr.String(); !strings.Contains(got, "no multiplexer available") {
		t.Fatalf("stderr = %q, want no multiplexer message", got)
	}
}

func TestAttachOpenCodeFromInfoWithExecutor_NoServerOrSession(t *testing.T) {
	stderr := &bytes.Buffer{}
	code, err := attachOpenCodeFromInfoWithExecutor(&orchapi.AttachInfo{
		IssueID: "orch-5",
		RunID:   "20260101-050505",
	}, attachStreams{stderr: stderr})

	if err == nil {
		t.Fatal("attachOpenCodeFromInfoWithExecutor() expected error, got nil")
	}
	if code != ExitRunNotFound {
		t.Fatalf("exit code = %d, want %d", code, ExitRunNotFound)
	}
	if got := stderr.String(); !strings.Contains(got, "no server port or session found") {
		t.Fatalf("stderr = %q, want no server/session message", got)
	}
}

func TestAttachOpenCodeFromInfoWithExecutor_AttachesToRunningServer(t *testing.T) {
	port := 43123

	orig := runOpenCodeCommand
	t.Cleanup(func() { runOpenCodeCommand = orig })

	var gotArgs []string
	var gotDir string
	runOpenCodeCommand = func(args []string, dir string, streams attachStreams) error {
		gotArgs = append([]string(nil), args...)
		gotDir = dir
		return nil
	}

	stderr := &bytes.Buffer{}
	code, err := attachOpenCodeFromInfoWithExecutor(&orchapi.AttachInfo{
		IssueID:           "orch-6",
		RunID:             "20260101-060606",
		Agent:             "opencode",
		ServerPort:        port,
		OpenCodeSessionID: "session-123",
		WorktreePath:      "/tmp/worktree",
	}, attachStreams{stderr: stderr})

	if err != nil {
		t.Fatalf("attachOpenCodeFromInfoWithExecutor() error = %v, want nil", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if gotDir != "" {
		t.Fatalf("command dir = %q, want empty", gotDir)
	}
	wantArgs := []string{
		"attach",
		"http://127.0.0.1:" + strconv.Itoa(port),
		"--session", "session-123",
		"--dir", "/tmp/worktree",
	}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("command args = %v, want %v", gotArgs, wantArgs)
	}
}

func TestResumeOpenCodeSessionWithExecutor_BuildsResumeCommand(t *testing.T) {
	orig := runOpenCodeCommand
	t.Cleanup(func() { runOpenCodeCommand = orig })

	var gotArgs []string
	var gotDir string
	runOpenCodeCommand = func(args []string, dir string, streams attachStreams) error {
		gotArgs = append([]string(nil), args...)
		gotDir = dir
		return nil
	}

	code, err := resumeOpenCodeSessionWithExecutor(&orchapi.AttachInfo{
		OpenCodeSessionID: "session-xyz",
		WorktreePath:      "/tmp/run-worktree",
	}, attachStreams{stderr: &bytes.Buffer{}})

	if err != nil {
		t.Fatalf("resumeOpenCodeSessionWithExecutor() error = %v, want nil", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	wantArgs := []string{"--session", "session-xyz", "/tmp/run-worktree"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("command args = %v, want %v", gotArgs, wantArgs)
	}
	if gotDir != "/tmp/run-worktree" {
		t.Fatalf("command dir = %q, want %q", gotDir, "/tmp/run-worktree")
	}
}

func TestRunAttachWithDeps_RemoteTargetUsesSSHAttach(t *testing.T) {
	api := &mockAttachAPI{
		info: &orchapi.AttachInfo{
			IssueID:       "orch-remote",
			RunID:         "20260101-090909",
			Agent:         "claude",
			SessionName:   "run-orch-remote-20260101-090909",
			SessionExists: true,
			TargetHost:    "user@mac",
			Multiplexer:   orchapi.MultiplexerTmux,
		},
	}
	deps, _, exitCodes := newAttachDepsForTest(api)

	calledRemote := false
	deps.attachRemote = func(info *orchapi.AttachInfo, streams attachStreams) (int, error) {
		calledRemote = true
		if info.TargetHost != "user@mac" {
			t.Fatalf("TargetHost = %q, want user@mac", info.TargetHost)
		}
		return 0, nil
	}

	muxUsed := false
	deps.getMuxWithFallback = func(t multiplexer.Type) (attachSessionMux, string, error) {
		muxUsed = true
		return &mockAttachMux{}, "", nil
	}

	err := runAttachWithDeps("orch-remote#20260101-090909", &attachOptions{}, deps)
	if err != nil {
		t.Fatalf("runAttachWithDeps() error = %v, want nil", err)
	}
	if !calledRemote {
		t.Fatalf("attachRemote was not called")
	}
	if muxUsed {
		t.Fatalf("multiplexer path should not be used for remote attach")
	}
	if len(*exitCodes) != 0 {
		t.Fatalf("exit codes = %v, want none", *exitCodes)
	}
}

func TestAttachRemoteFromInfoWithExecutor_TmuxCommand(t *testing.T) {
	orig := runSSHCommand
	t.Cleanup(func() { runSSHCommand = orig })

	var gotArgs []string
	runSSHCommand = func(args []string, streams attachStreams) error {
		gotArgs = append([]string(nil), args...)
		return nil
	}

	code, err := attachRemoteFromInfoWithExecutor(&orchapi.AttachInfo{
		IssueID:     "orch-r1",
		RunID:       "20260101-010101",
		SessionName: "run-session",
		TargetHost:  "user@mac",
		Multiplexer: orchapi.MultiplexerTmux,
	}, attachStreams{stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("attachRemoteFromInfoWithExecutor() error = %v, want nil", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	want := []string{"-t", "user@mac", "tmux", "attach-session", "-t", "run-session"}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("ssh args = %v, want %v", gotArgs, want)
	}
}

func TestAttachRemoteFromInfoWithExecutor_ZellijCommand(t *testing.T) {
	orig := runSSHCommand
	t.Cleanup(func() { runSSHCommand = orig })

	var gotArgs []string
	runSSHCommand = func(args []string, streams attachStreams) error {
		gotArgs = append([]string(nil), args...)
		return nil
	}

	code, err := attachRemoteFromInfoWithExecutor(&orchapi.AttachInfo{
		IssueID:     "orch-r2",
		RunID:       "20260101-020202",
		TargetHost:  "dev@linux",
		Multiplexer: orchapi.MultiplexerZellij,
	}, attachStreams{stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("attachRemoteFromInfoWithExecutor() error = %v, want nil", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	wantSession := model.GenerateSessionName("orch-r2", "20260101-020202")
	want := []string{"-t", "dev@linux", "zellij", "attach", wantSession}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("ssh args = %v, want %v", gotArgs, want)
	}
}

func TestAttachRemoteFromInfoWithExecutor_OpenCodeCommand(t *testing.T) {
	orig := runSSHCommand
	t.Cleanup(func() { runSSHCommand = orig })

	var gotArgs []string
	runSSHCommand = func(args []string, streams attachStreams) error {
		gotArgs = append([]string(nil), args...)
		return nil
	}

	code, err := attachRemoteFromInfoWithExecutor(&orchapi.AttachInfo{
		IssueID:           "orch-r3",
		RunID:             "20260101-030303",
		TargetHost:        "mac-dev",
		Agent:             "opencode",
		ServerPort:        4099,
		OpenCodeSessionID: "ses-123",
		WorktreePath:      "/tmp/remote-worktree",
	}, attachStreams{stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("attachRemoteFromInfoWithExecutor() error = %v, want nil", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	wantScript := "exec opencode attach " + shellQuote("http://127.0.0.1:4099") + " --session " + shellQuote("ses-123") + " --dir " + shellQuote("/tmp/remote-worktree")
	want := sshScriptArgs("mac-dev", true, wantScript)
	if !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("ssh args = %v, want %v", gotArgs, want)
	}
}
