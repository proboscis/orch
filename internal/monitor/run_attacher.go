package monitor

import (
	"fmt"
	"os"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/tmux"
)

type RunAttacher interface {
	Attach(m *Monitor, run *model.Run) error
}

func GetRunAttacher(agentType string) RunAttacher {
	if agentType == string(agent.AgentOpenCode) {
		return &OpenCodeRunAttacher{}
	}
	return &TmuxRunAttacher{}
}

type TmuxRunAttacher struct{}

func (a *TmuxRunAttacher) Attach(m *Monitor, run *model.Run) error {
	sessionName := run.TmuxSession
	if sessionName == "" {
		sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
	}
	w := &RunWindow{
		Run:          run,
		AgentSession: sessionName,
	}
	if err := m.ensureRunSession(w); err != nil {
		return err
	}
	if !tmux.HasSession(sessionName) {
		return fmt.Errorf("run session not found: %s", sessionName)
	}

	if err := m.ensurePaneLayout(); err != nil {
		return err
	}
	if err := m.repairSwappedRunSession(run, sessionName); err != nil {
		return err
	}
	m.refreshChatPaneTitle()

	windowID, err := m.resolveRunWindowID(run, sessionName)
	if err != nil {
		return err
	}

	monitorWindows, err := tmux.ListWindows(m.session)
	if err != nil {
		return err
	}
	if windowID != "" {
		if _, ok := windowIndexByID(monitorWindows, windowID); ok {
			return tmux.SelectWindowByID(windowID)
		}
	}

	targetIndex := nextAvailableWindowIndex(monitorWindows, dashboardWindowIdx+1)
	if windowID != "" {
		if err := tmux.LinkWindowByID(windowID, m.session, targetIndex); err != nil {
			return err
		}
		return tmux.SelectWindowByID(windowID)
	}
	if err := tmux.LinkWindow(sessionName, 0, m.session, targetIndex); err != nil {
		return err
	}
	return tmux.SelectWindow(m.session, targetIndex)
}

type OpenCodeRunAttacher struct{}

func (a *OpenCodeRunAttacher) Attach(m *Monitor, run *model.Run) error {
	if run.ServerPort == 0 {
		return fmt.Errorf("no server port found for opencode run: %s", run.Ref().String())
	}

	serverURL := fmt.Sprintf("http://127.0.0.1:%d", run.ServerPort)
	attachCmd := fmt.Sprintf("opencode attach %s", serverURL)
	if run.OpenCodeSessionID != "" {
		attachCmd = fmt.Sprintf("%s --session %s", attachCmd, run.OpenCodeSessionID)
	}

	monitorWindows, err := tmux.ListWindows(m.session)
	if err != nil {
		return err
	}

	windowName := fmt.Sprintf("opencode-%s", run.ShortID())
	for _, w := range monitorWindows {
		if w.Name == windowName {
			return tmux.SelectWindow(m.session, w.Index)
		}
	}

	targetIndex := nextAvailableWindowIndex(monitorWindows, dashboardWindowIdx+1)
	workDir := run.WorktreePath
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	if err := tmux.NewWindow(m.session, windowName, workDir, attachCmd); err != nil {
		return fmt.Errorf("failed to create opencode window: %w", err)
	}
	return tmux.SelectWindow(m.session, targetIndex)
}
