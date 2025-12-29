package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/s22625/orch/internal/model"
)

func TestNewCaptureAllCmd(t *testing.T) {
	cmd := newCaptureAllCmd()

	if cmd.Use != "capture-all" {
		t.Errorf("unexpected use: %s", cmd.Use)
	}

	if cmd.Short != "Capture output from all running agents" {
		t.Errorf("unexpected short: %s", cmd.Short)
	}

	linesFlag := cmd.Flags().Lookup("lines")
	if linesFlag == nil {
		t.Error("missing --lines flag")
	}

	if linesFlag.DefValue != "100" {
		t.Errorf("unexpected default for --lines: %s", linesFlag.DefValue)
	}

	if err := cmd.Args(cmd, []string{}); err != nil {
		t.Errorf("unexpected error with no args: %v", err)
	}

	if err := cmd.Args(cmd, []string{"extra"}); err == nil {
		t.Error("expected error with args")
	}
}

func TestRunCaptureAllJSON(t *testing.T) {
	resetGlobalOpts(t)

	vault := t.TempDir()
	globalOpts.VaultPath = vault
	globalOpts.Backend = "file"
	globalOpts.JSON = true
	globalOpts.Quiet = true

	writeIssue(t, vault, "issue-1")
	writeIssue(t, vault, "issue-2")
	writeIssue(t, vault, "issue-3")

	st, err := getStore()
	if err != nil {
		t.Fatalf("getStore: %v", err)
	}

	run1, err := st.CreateRun("issue-1", "run-1", nil)
	if err != nil {
		t.Fatalf("create run1: %v", err)
	}
	run2, err := st.CreateRun("issue-2", "run-2", nil)
	if err != nil {
		t.Fatalf("create run2: %v", err)
	}
	run3, err := st.CreateRun("issue-3", "run-3", nil)
	if err != nil {
		t.Fatalf("create run3: %v", err)
	}

	if err := st.AppendEvent(run1.Ref(), model.NewStatusEvent(model.StatusRunning)); err != nil {
		t.Fatalf("status run1: %v", err)
	}
	if err := st.AppendEvent(run2.Ref(), model.NewStatusEvent(model.StatusBlocked)); err != nil {
		t.Fatalf("status run2: %v", err)
	}
	if err := st.AppendEvent(run3.Ref(), model.NewStatusEvent(model.StatusDone)); err != nil {
		t.Fatalf("status run3: %v", err)
	}

	outputs := map[string]string{
		model.GenerateTmuxSession("issue-1", "run-1"): "run-1 output\n",
	}

	origHasSession := captureAllHasSession
	origCapturePane := captureAllCapturePane
	t.Cleanup(func() {
		captureAllHasSession = origHasSession
		captureAllCapturePane = origCapturePane
	})

	var capturedLines []int
	captureAllHasSession = func(session string) bool {
		_, ok := outputs[session]
		return ok
	}
	captureAllCapturePane = func(session string, lines int) (string, error) {
		capturedLines = append(capturedLines, lines)
		return outputs[session], nil
	}

	out := captureStdout(t, func() {
		if err := runCaptureAll(&captureAllOptions{Lines: 5}); err != nil {
			t.Fatalf("runCaptureAll: %v", err)
		}
	})

	var got struct {
		OK    bool             `json:"ok"`
		Items []captureAllItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	if got.OK {
		t.Fatalf("expected ok=false with missing session")
	}
	if len(got.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(got.Items))
	}
	if len(capturedLines) != 1 || capturedLines[0] != 5 {
		t.Fatalf("unexpected lines captured: %v", capturedLines)
	}

	items := make(map[string]captureAllItem, len(got.Items))
	for _, item := range got.Items {
		items[item.IssueID] = item
	}

	item1, ok := items["issue-1"]
	if !ok {
		t.Fatalf("missing issue-1 item")
	}
	if !item1.OK {
		t.Fatalf("issue-1 should be ok")
	}
	if item1.RunID != "run-1" {
		t.Fatalf("issue-1 run_id = %q, want run-1", item1.RunID)
	}
	if item1.Status != string(model.StatusRunning) {
		t.Fatalf("issue-1 status = %q, want %q", item1.Status, model.StatusRunning)
	}
	if item1.Content != "run-1 output\n" {
		t.Fatalf("issue-1 content = %q", item1.Content)
	}
	if item1.Lines != 5 {
		t.Fatalf("issue-1 lines = %d, want 5", item1.Lines)
	}
	if item1.TmuxSession != model.GenerateTmuxSession("issue-1", "run-1") {
		t.Fatalf("issue-1 tmux_session = %q", item1.TmuxSession)
	}

	item2, ok := items["issue-2"]
	if !ok {
		t.Fatalf("missing issue-2 item")
	}
	if item2.OK {
		t.Fatalf("issue-2 should not be ok")
	}
	if item2.RunID != "run-2" {
		t.Fatalf("issue-2 run_id = %q, want run-2", item2.RunID)
	}
	if item2.Status != string(model.StatusBlocked) {
		t.Fatalf("issue-2 status = %q, want %q", item2.Status, model.StatusBlocked)
	}
	if !strings.Contains(item2.Error, "tmux session") {
		t.Fatalf("issue-2 error = %q", item2.Error)
	}
}

func TestRunCaptureAllPlain(t *testing.T) {
	resetGlobalOpts(t)

	vault := t.TempDir()
	globalOpts.VaultPath = vault
	globalOpts.Backend = "file"

	writeIssue(t, vault, "issue-1")
	writeIssue(t, vault, "issue-2")

	st, err := getStore()
	if err != nil {
		t.Fatalf("getStore: %v", err)
	}

	run1, err := st.CreateRun("issue-1", "run-1", nil)
	if err != nil {
		t.Fatalf("create run1: %v", err)
	}
	run2, err := st.CreateRun("issue-2", "run-2", nil)
	if err != nil {
		t.Fatalf("create run2: %v", err)
	}

	if err := st.AppendEvent(run1.Ref(), model.NewStatusEvent(model.StatusRunning)); err != nil {
		t.Fatalf("status run1: %v", err)
	}
	if err := st.AppendEvent(run2.Ref(), model.NewStatusEvent(model.StatusBlocked)); err != nil {
		t.Fatalf("status run2: %v", err)
	}

	outputs := map[string]string{
		model.GenerateTmuxSession("issue-1", "run-1"): "run-1 output\n",
	}

	origHasSession := captureAllHasSession
	origCapturePane := captureAllCapturePane
	t.Cleanup(func() {
		captureAllHasSession = origHasSession
		captureAllCapturePane = origCapturePane
	})

	captureAllHasSession = func(session string) bool {
		_, ok := outputs[session]
		return ok
	}
	captureAllCapturePane = func(session string, lines int) (string, error) {
		return outputs[session], nil
	}

	out := captureStdout(t, func() {
		if err := runCaptureAll(&captureAllOptions{Lines: 5}); err != nil {
			t.Fatalf("runCaptureAll: %v", err)
		}
	})

	if !strings.Contains(out, "=== issue-1#run-1 [running] ===") {
		t.Fatalf("missing issue-1 header: %q", out)
	}
	if !strings.Contains(out, "run-1 output") {
		t.Fatalf("missing issue-1 output: %q", out)
	}
	if !strings.Contains(out, "=== issue-2#run-2 [blocked] ===") {
		t.Fatalf("missing issue-2 header: %q", out)
	}
	if !strings.Contains(out, "error: tmux session") {
		t.Fatalf("missing issue-2 error: %q", out)
	}
}
