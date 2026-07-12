package daemon

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/proboscis/orch/api/orchpb"
	"github.com/proboscis/orch/internal/agent"
	"github.com/proboscis/orch/internal/model"
)

func agentSessionEvents(run *model.Run) []*model.Event {
	var events []*model.Event
	for _, e := range run.Events {
		if e.Type == model.EventTypeArtifact && e.Name == "agent_session" {
			events = append(events, e)
		}
	}
	return events
}

func errorArtifactMessages(run *model.Run) []string {
	var msgs []string
	for _, e := range run.Events {
		if e.Type == model.EventTypeArtifact && e.Name == "error" {
			msgs = append(msgs, e.Attrs["message"])
		}
	}
	return msgs
}

func newAgentSessionTestStore(issueID, runID string) (*mockStore, *model.Run) {
	run := &model.Run{IssueID: model.IssueID(issueID), RunID: model.RunID(runID), Status: model.StatusRunning}
	st := &mockStore{
		runs:   map[string]*model.Run{run.Ref().String(): run},
		issues: map[string]*model.Issue{issueID: {ID: model.IssueID(issueID), Title: "t", Status: model.IssueStatusOpen}},
	}
	return st, run
}

func TestRecordAgentSessionIdentityClaude(t *testing.T) {
	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	st, run := newAgentSessionTestStore("iss-claude", "r1")

	launchCfg := &agent.LaunchConfig{
		Type:           agent.AgentClaude,
		AgentSessionID: "11111111-1111-1111-1111-111111111111",
	}

	result := server.recordAgentSessionIdentity(st, run, launchCfg)
	if result == nil {
		t.Fatal("expected an AgentSessionResult for claude")
	}
	if result.Backend != "claude" || result.ID != "11111111-1111-1111-1111-111111111111" || result.Generation != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}

	events := agentSessionEvents(run)
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 agent_session artifact, got %d", len(events))
	}
	attrs := events[0].Attrs
	if attrs["backend"] != "claude" || attrs["id"] != "11111111-1111-1111-1111-111111111111" || attrs["generation"] != "1" {
		t.Fatalf("unexpected artifact attrs: %v", attrs)
	}
	// The fold must pick the identity up (ADR-0005 R1: folded like opencode_session).
	if run.AgentSessionID != "11111111-1111-1111-1111-111111111111" || run.AgentSessionGeneration != 1 {
		t.Fatalf("fold missed agent_session: id=%q gen=%d", run.AgentSessionID, run.AgentSessionGeneration)
	}
}

func TestRecordAgentSessionIdentityClaudeWithoutMintRecordsNothing(t *testing.T) {
	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	st, run := newAgentSessionTestStore("iss-claude2", "r1")

	result := server.recordAgentSessionIdentity(st, run, &agent.LaunchConfig{Type: agent.AgentClaude})
	if result != nil {
		t.Fatalf("expected nil result without a minted id, got %+v", result)
	}
	if events := agentSessionEvents(run); len(events) != 0 {
		t.Fatalf("expected no agent_session artifacts, got %d", len(events))
	}
}

func TestRecordAgentSessionIdentityCodexResolvesRollout(t *testing.T) {
	restore := codexAgentSessionResolveTimeout
	codexAgentSessionResolveTimeout = 3 * time.Second
	defer func() { codexAgentSessionResolveTimeout = restore }()

	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	st, run := newAgentSessionTestStore("iss-codex", "r1")

	codexHome := t.TempDir()
	worktree := t.TempDir()
	day := time.Now()
	dir := filepath.Join(codexHome, "sessions", day.Format("2006"), day.Format("01"), day.Format("02"))
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	line := `{"timestamp":"t","type":"session_meta","payload":{"id":"codex-rollout-id","cwd":"` + worktree + `"}}`
	if err := os.WriteFile(filepath.Join(dir, "rollout-test.jsonl"), []byte(line+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	launchCfg := &agent.LaunchConfig{
		Type:      agent.AgentCodex,
		WorkDir:   worktree,
		CodexHome: codexHome,
	}

	result := server.recordAgentSessionIdentity(st, run, launchCfg)
	if result == nil || result.Backend != "codex" || result.ID != "codex-rollout-id" || result.Generation != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}

	events := agentSessionEvents(run)
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 agent_session artifact, got %d", len(events))
	}
	if events[0].Attrs["backend"] != "codex" || events[0].Attrs["id"] != "codex-rollout-id" || events[0].Attrs["generation"] != "1" {
		t.Fatalf("unexpected artifact attrs: %v", events[0].Attrs)
	}
}

func TestRecordAgentSessionIdentityCodexUnresolvedIsLoud(t *testing.T) {
	restore := codexAgentSessionResolveTimeout
	codexAgentSessionResolveTimeout = 300 * time.Millisecond
	defer func() { codexAgentSessionResolveTimeout = restore }()

	server := NewSocketServer(nil, log.New(io.Discard, "", 0))
	st, run := newAgentSessionTestStore("iss-codex2", "r1")

	launchCfg := &agent.LaunchConfig{
		Type:      agent.AgentCodex,
		WorkDir:   "/nonexistent/worktree",
		CodexHome: t.TempDir(), // no rollouts will ever appear
	}

	result := server.recordAgentSessionIdentity(st, run, launchCfg)
	if result == nil || result.Backend != "codex" {
		t.Fatalf("unresolved codex identity must still return a result naming the miss, got %+v", result)
	}
	if result.ID != "" {
		t.Fatalf("unresolved codex identity must never guess an id, got %q", result.ID)
	}
	if !strings.Contains(result.Unresolved, "agent_session_unresolved") {
		t.Fatalf("result.Unresolved should name agent_session_unresolved, got %q", result.Unresolved)
	}

	if events := agentSessionEvents(run); len(events) != 0 {
		t.Fatalf("expected no agent_session artifact on failure, got %d", len(events))
	}
	msgs := errorArtifactMessages(run)
	if len(msgs) != 1 || !strings.Contains(msgs[0], "agent_session_unresolved") {
		t.Fatalf("expected one loud error artifact naming agent_session_unresolved, got %v", msgs)
	}
}

func TestRecordAgentSessionIdentityRecordsNothingForOtherBackends(t *testing.T) {
	server := NewSocketServer(nil, log.New(io.Discard, "", 0))

	for _, agentType := range []agent.AgentType{agent.AgentOpenCode, agent.AgentGemini, agent.AgentCustom} {
		st, run := newAgentSessionTestStore("iss-"+string(agentType), "r1")
		result := server.recordAgentSessionIdentity(st, run, &agent.LaunchConfig{Type: agentType, AgentSessionID: "should-be-ignored"})
		if result != nil {
			t.Fatalf("%s: expected nil result, got %+v", agentType, result)
		}
		if events := agentSessionEvents(run); len(events) != 0 {
			t.Fatalf("%s: expected no agent_session artifacts, got %d", agentType, len(events))
		}
	}
}

func TestSyncStartRunResultProjectsAgentSession(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	st, run := newAgentSessionTestStore("iss-sync", "run-sync")
	server := newTestServer(t, st)

	req := &orchpb.StartRunRequest{IssueId: "iss-sync"}
	result := &StartRunResult{
		RunID:  "run-sync",
		Status: "running",
		AgentSession: &AgentSessionResult{
			Backend:    "claude",
			ID:         "22222222-2222-2222-2222-222222222222",
			Generation: 1,
		},
	}

	if err := server.syncStartRunResultToMasterStore(st, req, result, "", ""); err != nil {
		t.Fatalf("syncStartRunResultToMasterStore() error = %v", err)
	}

	events := agentSessionEvents(run)
	if len(events) != 1 {
		t.Fatalf("expected the master projection to append 1 agent_session artifact, got %d", len(events))
	}
	attrs := events[0].Attrs
	if attrs["backend"] != "claude" || attrs["id"] != "22222222-2222-2222-2222-222222222222" || attrs["generation"] != "1" {
		t.Fatalf("unexpected projected attrs: %v", attrs)
	}
}

func TestSyncContinueRunResultProjectsAgentSessionUnresolved(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()

	st, run := newAgentSessionTestStore("iss-sync2", "run-sync2")
	server := newTestServer(t, st)

	req := &orchpb.ContinueRunRequest{IssueId: "iss-sync2"}
	result := &ContinueRunResult{
		IssueID: "iss-sync2",
		RunID:   "run-sync2",
		Status:  "running",
		AgentSession: &AgentSessionResult{
			Backend:    "codex",
			Unresolved: "agent_session_unresolved: no codex rollout with session_meta cwd == /wt",
		},
	}

	if err := server.syncContinueRunResultToMasterStore(st, req, result, "", "", "codex", "", ""); err != nil {
		t.Fatalf("syncContinueRunResultToMasterStore() error = %v", err)
	}

	if events := agentSessionEvents(run); len(events) != 0 {
		t.Fatalf("unresolved identity must not project an agent_session artifact, got %d", len(events))
	}
	msgs := errorArtifactMessages(run)
	if len(msgs) != 1 || !strings.Contains(msgs[0], "agent_session_unresolved") {
		t.Fatalf("expected the projected error artifact naming agent_session_unresolved, got %v", msgs)
	}
}

// --- full launch-ladder integration tests (real tmux on a private server,
// fake agent binaries so no real agent ever launches or burns tokens) ---

// setupPrivateTmux points tmux at a private socket dir so the test never
// touches the user's tmux server, and the private server inherits this test
// process's PATH (where the fake agent binaries live).
func setupPrivateTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	// The socket dir must stay short (unix socket path limit); t.TempDir()
	// under /var/folders is too long on macOS.
	tmuxDir, err := os.MkdirTemp("/tmp", "orch-tmux-")
	if err != nil {
		t.Fatalf("mkdir tmux tmpdir: %v", err)
	}
	t.Setenv("TMUX_TMPDIR", tmuxDir)
	t.Setenv("TMUX", "")
	t.Cleanup(func() {
		cmd := exec.Command("tmux", "kill-server")
		cmd.Env = append(os.Environ(), "TMUX_TMPDIR="+tmuxDir)
		_ = cmd.Run()
		_ = os.RemoveAll(tmuxDir)
	})
}

func writeFakeAgentBin(t *testing.T, name, body string) string {
	t.Helper()
	binDir := t.TempDir()
	script := "#!/bin/sh\n" + body
	if err := os.WriteFile(filepath.Join(binDir, name), []byte(script), 0755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return binDir
}

func waitForFile(t *testing.T, path string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return string(data)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
	return ""
}

func TestStartRunLadderRecordsClaudeAgentSession(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()
	setupPrivateTmux(t)

	argvFile := filepath.Join(t.TempDir(), "claude-argv.txt")
	writeFakeAgentBin(t, "claude", fmt.Sprintf(`if [ "$1" = "--version" ]; then echo "claude test"; exit 0; fi
echo "$@" > %s
sleep 15
`, argvFile))

	repo := initGitRepoWithCommit(t)
	st := &mockStore{
		runs:   make(map[string]*model.Run),
		issues: map[string]*model.Issue{"iss-ladder-claude": {ID: "iss-ladder-claude", Title: "t", Body: "b", Status: model.IssueStatusOpen}},
	}
	server := NewSocketServer(nil, log.New(io.Discard, "", 0))

	opts := &StartRunOptions{
		IssueID:     "iss-ladder-claude",
		Agent:       "claude",
		Multiplexer: "tmux",
		WorktreeDir: t.TempDir(),
	}
	result, err := server.processStartRunCore(st, repo, opts)
	if err != nil {
		t.Fatalf("processStartRunCore() error = %v", err)
	}

	run, ok := st.runs[fmt.Sprintf("iss-ladder-claude#%s", result.RunID)]
	if !ok {
		t.Fatalf("run not found in store, got %v", st.runs)
	}

	events := agentSessionEvents(run)
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 agent_session artifact per launch, got %d", len(events))
	}
	id := events[0].Attrs["id"]
	if id == "" || events[0].Attrs["backend"] != "claude" || events[0].Attrs["generation"] != "1" {
		t.Fatalf("unexpected artifact attrs: %v", events[0].Attrs)
	}
	if result.AgentSession == nil || result.AgentSession.ID != id {
		t.Fatalf("StartRunResult must carry the same identity for master projection, got %+v", result.AgentSession)
	}

	// The claude argv must pin the SAME id via --session-id (acceptance 1).
	argv := waitForFile(t, argvFile, 10*time.Second)
	m := regexp.MustCompile(`--session-id ([0-9a-fA-F-]+)`).FindStringSubmatch(argv)
	if m == nil {
		t.Fatalf("claude argv missing --session-id: %q", argv)
	}
	if m[1] != id {
		t.Fatalf("argv --session-id %q != recorded artifact id %q", m[1], id)
	}
}

func TestStartRunLadderRecordsCodexAgentSession(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()
	setupPrivateTmux(t)

	restore := codexAgentSessionResolveTimeout
	codexAgentSessionResolveTimeout = 20 * time.Second
	defer func() { codexAgentSessionResolveTimeout = restore }()

	codexHome := t.TempDir()
	// AuthPreflight requires auth.json when CODEX_HOME is configured.
	if err := os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	// The fake codex mimics the real boot behavior: it writes a rollout file
	// with session_meta {id, cwd=$PWD} under $CODEX_HOME/sessions/YYYY/MM/DD.
	writeFakeAgentBin(t, "codex", `if [ "$1" = "--version" ]; then echo "codex test"; exit 0; fi
day=$(date +%Y/%m/%d)
mkdir -p "$CODEX_HOME/sessions/$day"
printf '{"timestamp":"t","type":"session_meta","payload":{"id":"e2e-codex-rollout-id","cwd":"%s"}}\n' "$PWD" > "$CODEX_HOME/sessions/$day/rollout-e2e.jsonl"
sleep 15
`)

	repo := initGitRepoWithCommit(t)
	st := &mockStore{
		runs:   make(map[string]*model.Run),
		issues: map[string]*model.Issue{"iss-ladder-codex": {ID: "iss-ladder-codex", Title: "t", Body: "b", Status: model.IssueStatusOpen}},
	}
	server := NewSocketServer(nil, log.New(io.Discard, "", 0))

	opts := &StartRunOptions{
		IssueID:     "iss-ladder-codex",
		Agent:       "codex",
		Multiplexer: "tmux",
		WorktreeDir: t.TempDir(),
		CodexHome:   codexHome,
	}
	result, err := server.processStartRunCore(st, repo, opts)
	if err != nil {
		t.Fatalf("processStartRunCore() error = %v", err)
	}

	run, ok := st.runs[fmt.Sprintf("iss-ladder-codex#%s", result.RunID)]
	if !ok {
		t.Fatalf("run not found in store, got %v", st.runs)
	}

	events := agentSessionEvents(run)
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 agent_session artifact per launch, got %d", len(events))
	}
	if events[0].Attrs["id"] != "e2e-codex-rollout-id" || events[0].Attrs["backend"] != "codex" || events[0].Attrs["generation"] != "1" {
		t.Fatalf("unexpected artifact attrs: %v", events[0].Attrs)
	}
	if result.AgentSession == nil || result.AgentSession.ID != "e2e-codex-rollout-id" {
		t.Fatalf("StartRunResult must carry the resolved codex identity, got %+v", result.AgentSession)
	}
}

func TestContinueRunLadderRecordsClaudeAgentSession(t *testing.T) {
	cleanup := setupXDGTestEnv(t)
	defer cleanup()
	setupPrivateTmux(t)

	argvFile := filepath.Join(t.TempDir(), "claude-argv.txt")
	writeFakeAgentBin(t, "claude", fmt.Sprintf(`if [ "$1" = "--version" ]; then echo "claude test"; exit 0; fi
echo "$@" > %s
sleep 15
`, argvFile))

	repo := initGitRepoWithCommit(t)
	branch := "issue/iss-ladder-cont/run-e2e"
	worktree := filepath.Join(t.TempDir(), "cont-wt")
	cmd := exec.Command("git", "-C", repo, "worktree", "add", "-b", branch, worktree, "main")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add failed: %v (%s)", err, out)
	}

	st := &mockStore{
		runs:   make(map[string]*model.Run),
		issues: map[string]*model.Issue{"iss-ladder-cont": {ID: "iss-ladder-cont", Title: "t", Body: "b", Status: model.IssueStatusOpen}},
	}
	server := NewSocketServer(nil, log.New(io.Discard, "", 0))

	opts := &ContinueRunOptions{
		IssueID:     "iss-ladder-cont",
		Branch:      branch,
		Agent:       "claude",
		Multiplexer: "tmux",
	}
	result, err := server.processContinueRunCore(st, repo, opts)
	if err != nil {
		t.Fatalf("processContinueRunCore() error = %v", err)
	}

	run, ok := st.runs[fmt.Sprintf("iss-ladder-cont#%s", result.RunID)]
	if !ok {
		t.Fatalf("run not found in store, got %v", st.runs)
	}

	events := agentSessionEvents(run)
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 agent_session artifact per launch, got %d", len(events))
	}
	id := events[0].Attrs["id"]
	if id == "" || events[0].Attrs["backend"] != "claude" || events[0].Attrs["generation"] != "1" {
		t.Fatalf("unexpected artifact attrs: %v", events[0].Attrs)
	}
	if result.AgentSession == nil || result.AgentSession.ID != id {
		t.Fatalf("ContinueRunResult must carry the same identity for master projection, got %+v", result.AgentSession)
	}

	argv := waitForFile(t, argvFile, 10*time.Second)
	if !strings.Contains(argv, "--session-id "+id) {
		t.Fatalf("claude argv missing --session-id %s: %q", id, argv)
	}
}
