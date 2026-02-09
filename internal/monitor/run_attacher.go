package monitor

import (
	"fmt"
	"os"
	"strings"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/model"
)

type RunAttacher interface {
	Attach(m *Monitor, run *model.Run) error
}

func GetRunAttacher(agentType string) RunAttacher {
	baseAgent := extractAgentName(agentType)
	if baseAgent == string(agent.AgentOpenCode) {
		return &OpenCodeRunAttacher{}
	}
	return &MuxRunAttacher{}
}

func extractAgentName(agentType string) string {
	if idx := strings.Index(agentType, ":"); idx != -1 {
		return agentType[:idx]
	}
	return agentType
}

type MuxRunAttacher struct{}

func (a *MuxRunAttacher) Attach(m *Monitor, run *model.Run) error {
	sessionName := run.SessionName
	if sessionName == "" {
		sessionName = model.GenerateSessionName(run.IssueID, run.RunID)
	}
	w := &RunWindow{
		Run:          run,
		AgentSession: sessionName,
	}
	if err := m.ensureRunSession(w); err != nil {
		return err
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

	monitorWindows, err := m.mux.ListWindows(m.session)
	if err != nil {
		return err
	}
	if windowID != "" {
		if _, ok := windowIndexByID(monitorWindows, windowID); ok {
			return m.mux.SelectWindowByID(windowID)
		}
	}

	targetIndex := nextAvailableWindowIndex(monitorWindows, dashboardWindowIdx+1)
	if windowID != "" {
		if err := m.mux.LinkWindowByID(windowID, m.session, targetIndex); err != nil {
			return err
		}
		return m.mux.SelectWindowByID(windowID)
	}
	if err := m.mux.LinkWindow(sessionName, 0, m.session, targetIndex); err != nil {
		return err
	}
	return m.mux.SelectWindow(m.session, targetIndex)
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
	if run.WorktreePath != "" {
		attachCmd = fmt.Sprintf("%s --dir %s", attachCmd, run.WorktreePath)
	}

	monitorWindows, err := m.mux.ListWindows(m.session)
	if err != nil {
		return err
	}

	windowName := fmt.Sprintf("%s[%s]", run.IssueID, run.ShortID())
	for _, w := range monitorWindows {
		if w.Name == windowName {
			return m.mux.SelectWindow(m.session, w.Index)
		}
	}

	workDir := run.WorktreePath
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	if err := m.mux.NewWindow(m.session, windowName, workDir, attachCmd); err != nil {
		return fmt.Errorf("failed to create opencode window for %s: %w", run.Ref().String(), err)
	}

	updatedWindows, err := m.mux.ListWindows(m.session)
	if err != nil {
		return err
	}
	for _, w := range updatedWindows {
		if w.Name == windowName {
			return m.mux.SelectWindow(m.session, w.Index)
		}
	}
	return fmt.Errorf("created window %s not found", windowName)
}
