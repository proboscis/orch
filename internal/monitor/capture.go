package monitor

import (
	"fmt"
	"strings"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/tmux"
)

// CaptureRun returns the latest tmux pane output for a run.
func (m *Monitor) CaptureRun(run *model.Run, lines int) (string, error) {
	if run == nil {
		return "", fmt.Errorf("run not found")
	}
	if lines <= 0 {
		lines = defaultCaptureLines
	}
	sessionName := run.TmuxSession
	if sessionName == "" {
		sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
	}
	if !tmux.HasSession(sessionName) {
		return "", fmt.Errorf("tmux session %q not found", sessionName)
	}
	content, err := tmux.CapturePane(sessionName, lines)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(content, "\n"), nil
}
