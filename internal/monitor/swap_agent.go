package monitor

import (
	"fmt"
	"strings"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/tmux"
)

const swapPromptInstruction = "Please read 'ORCH_PROMPT.md' in the current directory and follow the instructions found there."

// SwapAgent replaces the active agent for a run with a new agent type.
func (m *Monitor) SwapAgent(run *model.Run, agentName string) (string, error) {
	if run == nil {
		return "", fmt.Errorf("run not found")
	}
	if isTerminalStatus(run.Status) {
		return "", fmt.Errorf("run %s#%s is %s", run.IssueID, run.RunID, run.Status)
	}

	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return "", fmt.Errorf("agent type is required")
	}
	if run.WorktreePath == "" {
		return "", fmt.Errorf("run has no worktree path")
	}
	if !tmux.IsTmuxAvailable() {
		return "", fmt.Errorf("tmux is not available")
	}

	aType, err := agent.ParseAgentType(agentName)
	if err != nil {
		return "", err
	}
	if aType == agent.AgentCustom {
		return "", fmt.Errorf("custom agent requires --agent-cmd")
	}
	adapter, err := agent.GetAdapter(aType)
	if err != nil {
		return "", err
	}
	if !adapter.IsAvailable() {
		return "", fmt.Errorf("agent %s is not available", agentName)
	}

	issue, err := m.store.ResolveIssue(run.IssueID)
	if err != nil {
		return "", err
	}
	prompt := buildSwapPrompt(issue, run)

	sessionName := run.TmuxSession
	if sessionName == "" {
		sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
	}

	launchCfg := &agent.LaunchConfig{
		Type:      aType,
		WorkDir:   run.WorktreePath,
		IssueID:   run.IssueID,
		RunID:     run.RunID,
		RunPath:   run.Path,
		VaultPath: m.store.VaultPath(),
		Branch:    run.Branch,
		Prompt:    prompt,
	}
	agentCmd, err := adapter.LaunchCommand(launchCfg)
	if err != nil {
		return "", err
	}

	if tmux.HasSession(sessionName) {
		if err := tmux.KillSession(sessionName); err != nil {
			return "", err
		}
	}

	if err := tmux.NewSession(&tmux.SessionConfig{
		SessionName: sessionName,
		WorkDir:     run.WorktreePath,
		Command:     agentCmd,
		Env:         launchCfg.Env(),
	}); err != nil {
		return "", err
	}

	if adapter.PromptInjection() == agent.InjectionTmux && launchCfg.Prompt != "" {
		if pattern := adapter.ReadyPattern(); pattern != "" {
			if err := tmux.WaitForReady(sessionName, pattern, 30*time.Second); err != nil {
				return "", err
			}
		}
		if err := tmux.SendKeys(sessionName, launchCfg.Prompt); err != nil {
			return "", err
		}
	}

	if err := m.store.AppendEvent(run.Ref(), model.NewArtifactEvent("agent", map[string]string{"name": agentName})); err != nil {
		return "", err
	}
	if run.TmuxSession == "" {
		if err := m.store.AppendEvent(run.Ref(), model.NewArtifactEvent("session", map[string]string{"name": sessionName})); err != nil {
			return "", err
		}
	}
	if windows, err := tmux.ListWindows(sessionName); err == nil {
		windowID := ""
		for _, window := range windows {
			if window.Index == 0 {
				windowID = window.ID
				break
			}
		}
		if windowID != "" {
			if err := m.store.AppendEvent(run.Ref(), model.NewArtifactEvent("window", map[string]string{"id": windowID})); err != nil {
				return "", err
			}
		}
	}
	if err := m.store.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusRunning)); err != nil {
		return "", err
	}

	return fmt.Sprintf("swapped agent to %s for %s#%s", agentName, run.IssueID, run.RunID), nil
}

func buildSwapPrompt(issue *model.Issue, run *model.Run) string {
	issueID := "-"
	title := ""
	if issue != nil {
		if issue.ID != "" {
			issueID = issue.ID
		}
		title = issue.Title
	} else if run != nil && run.IssueID != "" {
		issueID = run.IssueID
	}

	prompt := fmt.Sprintf("Swapping agent for issue: %s\n\n", issueID)
	if title != "" {
		prompt += fmt.Sprintf("Title: %s\n\n", title)
	}
	prompt += swapPromptInstruction + "\n\n"

	if run != nil {
		if run.RunID != "" {
			prompt += fmt.Sprintf("Run ID: %s\n", run.RunID)
		}
		if run.Agent != "" {
			prompt += fmt.Sprintf("Previous agent: %s\n", run.Agent)
		}
		if run.Status != "" {
			prompt += fmt.Sprintf("Previous status: %s\n", run.Status)
		}
	}
	prompt += "Please continue from where the previous agent left off.\n"
	return prompt
}
