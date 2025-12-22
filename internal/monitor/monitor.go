package monitor

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/pr"
	"github.com/s22625/orch/internal/store"
	"github.com/s22625/orch/internal/tmux"
)

const (
	defaultSessionName  = "orch-monitor"
	dashboardWindowName = "dashboard"
	dashboardWindowIdx  = 0
)

const (
	runsPaneTitle   = "runs"
	issuesPaneTitle = "issues"
	chatPaneTitle   = "chat"
)

const (
	runsPaneOption   = "@orch_runs_pane"
	issuesPaneOption = "@orch_issues_pane"
	chatPaneOption   = "@orch_chat_pane"
)

// Options configures the monitor behavior.
type Options struct {
	Session     string
	Issue       string
	Statuses    []model.Status
	RunSort     SortKey
	IssueSort   SortKey
	Agent       string
	Attach      bool
	ForceNew    bool
	OrchPath    string
	GlobalFlags []string
}

// Monitor manages tmux windows and dashboard state.
type Monitor struct {
	session      string
	issueFilter  string
	statusFilter []model.Status
	runSort      SortKey
	issueSort    SortKey
	store        store.Store
	repoName     string
	orchPath     string
	globalFlags  []string
	agent        string
	attach       bool
	forceNew     bool
	runs         []*RunWindow
	dashboard    *Dashboard
}

// RunWindow links a run to a dashboard index.
type RunWindow struct {
	Index        int
	Run          *model.Run
	AgentSession string
}

// New creates a monitor with the provided options.
func New(st store.Store, opts Options) *Monitor {
	session := opts.Session
	if session == "" {
		session = sessionNameForVault(st.VaultPath())
	}
	orchPath := resolveOrchPath(opts.OrchPath)
	runSort := opts.RunSort
	if !IsValidSortKey(runSort) {
		runSort = SortByUpdated
	}
	issueSort := opts.IssueSort
	if !IsValidSortKey(issueSort) {
		issueSort = SortByName
	}
	repoName := resolveRepoName(st)
	return &Monitor{
		session:      session,
		issueFilter:  opts.Issue,
		statusFilter: opts.Statuses,
		runSort:      runSort,
		issueSort:    issueSort,
		store:        st,
		repoName:     repoName,
		orchPath:     orchPath,
		globalFlags:  opts.GlobalFlags,
		agent:        opts.Agent,
		attach:       opts.Attach,
		forceNew:     opts.ForceNew,
	}
}

// RunSort returns the current run sort key.
func (m *Monitor) RunSort() SortKey {
	return m.runSort
}

// IssueSort returns the current issue sort key.
func (m *Monitor) IssueSort() SortKey {
	return m.issueSort
}

// CycleRunSort advances to the next run sort key.
func (m *Monitor) CycleRunSort() SortKey {
	m.runSort = NextSortKey(m.runSort)
	return m.runSort
}

// CycleIssueSort advances to the next issue sort key.
func (m *Monitor) CycleIssueSort() SortKey {
	m.issueSort = NextSortKey(m.issueSort)
	return m.issueSort
}

// sessionNameForVault generates a unique monitor session name based on the vault path.
// This ensures each project has its own monitor session.
func sessionNameForVault(vaultPath string) string {
	if vaultPath == "" {
		return defaultSessionName
	}

	// Normalize the path to handle symlinks and relative paths
	absPath, err := filepath.Abs(vaultPath)
	if err != nil {
		absPath = vaultPath
	}
	// Try to resolve symlinks for consistent naming
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = resolved
	}

	// Generate a short hash of the path for uniqueness
	hash := sha256.Sum256([]byte(absPath))
	shortHash := hex.EncodeToString(hash[:])[:6]

	// Use the last directory component for readability
	baseName := filepath.Base(absPath)
	// Clean up the base name for tmux session naming
	baseName = strings.ReplaceAll(baseName, ".", "-")
	baseName = strings.ReplaceAll(baseName, " ", "-")

	return fmt.Sprintf("orch-%s-%s", baseName, shortHash)
}

func resolveRepoName(st store.Store) string {
	if st == nil {
		return ""
	}

	vaultPath := strings.TrimSpace(st.VaultPath())
	if vaultPath == "" {
		return ""
	}

	repoRoot, err := git.FindMainRepoRoot(vaultPath)
	if err != nil {
		return filepath.Base(vaultPath)
	}

	return filepath.Base(repoRoot)
}

func (m *Monitor) titleWithRepo(base string) string {
	if m == nil || m.repoName == "" {
		return base
	}
	return fmt.Sprintf("%s (%s)", base, m.repoName)
}

// Start creates or attaches to the monitor tmux session.
func (m *Monitor) Start() error {
	if !tmux.IsTmuxAvailable() {
		return fmt.Errorf("tmux is not available")
	}

	if m.forceNew && tmux.HasSession(m.session) {
		if tmux.IsInsideTmux() {
			if current, err := tmux.CurrentSession(); err == nil && current == m.session {
				return fmt.Errorf("cannot use --new from inside %s; detach and rerun", m.session)
			}
		}
		if err := tmux.KillSession(m.session); err != nil {
			return fmt.Errorf("failed to kill existing monitor session: %w", err)
		}
	}

	sessionExists := tmux.HasSession(m.session)
	if sessionExists && m.attach {
		return m.attachSession()
	}

	if !sessionExists {
		if err := m.createSession(); err != nil {
			return err
		}
	}

	if err := m.ensurePaneLayout(); err != nil {
		return err
	}
	if err := m.repairSwappedMonitorChat(); err != nil {
		return err
	}
	m.refreshChatPaneTitle()

	runs, err := m.loadRuns()
	if err != nil {
		return err
	}
	m.runs = runs

	return m.attachSession()
}

// RunDashboard launches the bubbletea dashboard.
func (m *Monitor) RunDashboard() error {
	d := NewDashboard(m)
	m.dashboard = d
	return d.Run()
}

// RunIssuesDashboard launches the issues dashboard.
func (m *Monitor) RunIssuesDashboard() error {
	d := NewIssueDashboard(m)
	return d.Run()
}

// Refresh reloads runs and syncs tmux windows.
func (m *Monitor) Refresh() ([]RunRow, error) {
	runs, err := m.loadRuns()
	if err != nil {
		return nil, err
	}
	m.runs = runs
	return m.buildRunRows(runs)
}

// RefreshIssues reloads issue data for the issues dashboard.
func (m *Monitor) RefreshIssues() ([]IssueRow, error) {
	issues, err := m.store.ListIssues()
	if err != nil {
		return nil, err
	}
	runs, err := m.store.ListRuns(nil)
	if err != nil {
		return nil, err
	}
	return m.buildIssueRows(issues, runs), nil
}

// SwitchWindow selects a tmux window by index.
func (m *Monitor) SwitchWindow(index int) error {
	return tmux.SelectWindow(m.session, index)
}

// SwitchRuns switches to the runs dashboard window.
func (m *Monitor) SwitchRuns() error {
	if err := tmux.SelectWindow(m.session, dashboardWindowIdx); err != nil {
		return err
	}
	return m.selectPaneByOption(runsPaneOption, runsPaneTitle)
}

// SwitchIssues switches to the issues dashboard window.
func (m *Monitor) SwitchIssues() error {
	if err := tmux.SelectWindow(m.session, dashboardWindowIdx); err != nil {
		return err
	}
	return m.selectPaneByOption(issuesPaneOption, issuesPaneTitle)
}

// SwitchChat switches to the agent chat window.
func (m *Monitor) SwitchChat() error {
	if err := tmux.SelectWindow(m.session, dashboardWindowIdx); err != nil {
		return err
	}
	pane, err := m.findChatPane()
	if err != nil {
		return err
	}
	return tmux.SelectPane(pane)
}

// OpenRun links a run session into the monitor and switches to it.
func (m *Monitor) OpenRun(run *model.Run) error {
	if run == nil {
		return fmt.Errorf("run not found")
	}

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

// CloseRun returns to the dashboard window.
func (m *Monitor) CloseRun() error {
	return tmux.SelectWindow(m.session, dashboardWindowIdx)
}

// Quit terminates the monitor tmux session.
func (m *Monitor) Quit() error {
	return tmux.KillSession(m.session)
}

// StopRun kills the run tmux session and marks the run canceled.
func (m *Monitor) StopRun(run *model.Run) error {
	if isTerminalStatus(run.Status) {
		return nil
	}

	sessionName := run.TmuxSession
	if sessionName == "" {
		sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
	}

	if tmux.HasSession(sessionName) {
		_ = tmux.KillSession(sessionName)
	}

	return m.store.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusCanceled))
}

// StartRun launches a new run by invoking the orch binary.
func (m *Monitor) StartRun(issueID string, agentType string) (string, error) {
	args := append([]string{}, m.globalFlags...)
	args = append(args, "run", issueID)
	if agentType != "" {
		args = append(args, "--agent", agentType)
	}

	cmd := exec.Command(m.orchPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	output := strings.TrimSpace(strings.TrimSpace(stdout.String()) + "\n" + strings.TrimSpace(stderr.String()))
	if err != nil {
		if strings.TrimSpace(output) == "" {
			output = err.Error()
		}
		return output, err
	}
	if strings.TrimSpace(output) == "" {
		output = "run started"
	}
	return output, nil
}

// GetAvailableAgents returns a list of available agent types.
func (m *Monitor) GetAvailableAgents() []string {
	agents := []string{
		string(agent.AgentClaude),
		string(agent.AgentCodex),
		string(agent.AgentGemini),
		string(agent.AgentCustom),
	}

	// Filter to only include available agents
	available := make([]string, 0, len(agents))
	for _, agentName := range agents {
		aType, err := agent.ParseAgentType(agentName)
		if err != nil {
			continue
		}
		adapter, err := agent.GetAdapter(aType)
		if err != nil {
			continue
		}
		// Custom agent is always technically "available" (has no CLI check)
		// For others, check if the CLI is installed
		if adapter.IsAvailable() {
			available = append(available, agentName)
		}
	}

	return available
}

// OpenIssue opens an issue via orch open.
func (m *Monitor) OpenIssue(issueID string) (string, error) {
	args := append([]string{}, m.globalFlags...)
	args = append(args, "open", issueID)

	cmd := exec.Command(m.orchPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	output := strings.TrimSpace(strings.TrimSpace(stdout.String()) + "\n" + strings.TrimSpace(stderr.String()))
	if err != nil {
		if strings.TrimSpace(output) == "" {
			output = err.Error()
		}
		return output, err
	}
	if strings.TrimSpace(output) == "" {
		output = fmt.Sprintf("opened %s", issueID)
	}
	return output, nil
}

// CreateIssue creates a new issue via orch issue create.
func (m *Monitor) CreateIssue(issueID, title string) (string, error) {
	args := append([]string{}, m.globalFlags...)
	args = append(args, "issue", "create", issueID)
	if strings.TrimSpace(title) != "" {
		args = append(args, "--title", title)
	}

	cmd := exec.Command(m.orchPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	output := strings.TrimSpace(strings.TrimSpace(stdout.String()) + "\n" + strings.TrimSpace(stderr.String()))
	if err != nil {
		if strings.TrimSpace(output) == "" {
			output = err.Error()
		}
		return output, err
	}
	if strings.TrimSpace(output) == "" {
		output = fmt.Sprintf("created issue %s", issueID)
	}
	return output, nil
}

// SetIssueStatus updates the issue status in the store.
func (m *Monitor) SetIssueStatus(issueID string, status model.IssueStatus) error {
	return m.store.SetIssueStatus(issueID, status)
}

// ResolveRun marks the run as done and its corresponding issue as resolved.
func (m *Monitor) ResolveRun(run *model.Run) error {
	if run == nil {
		return fmt.Errorf("run not found")
	}

	// Mark the run as done if not already in a terminal state
	if !isTerminalStatus(run.Status) {
		if err := m.store.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusDone)); err != nil {
			return fmt.Errorf("failed to mark run as done: %w", err)
		}
	}

	// Mark the corresponding issue as resolved
	if err := m.store.SetIssueStatus(run.IssueID, model.IssueStatusResolved); err != nil {
		return fmt.Errorf("failed to resolve issue: %w", err)
	}

	return nil
}

// ListIssues fetches issues from the store.
func (m *Monitor) ListIssues() ([]*model.Issue, error) {
	return m.store.ListIssues()
}

// ListRunsForIssue fetches runs for a specific issue.
func (m *Monitor) ListRunsForIssue(issueID string) ([]*model.Run, error) {
	if strings.TrimSpace(issueID) == "" {
		return nil, fmt.Errorf("issue id is required")
	}
	runs, err := m.store.ListRuns(&store.ListRunsFilter{IssueID: issueID})
	if err != nil {
		return nil, err
	}
	sortRuns(runs, m.runSort)
	return runs, nil
}

// ListBranchesForIssue returns branches that contain the issue ID in their name.
func (m *Monitor) ListBranchesForIssue(issueID string) ([]branchInfo, error) {
	if strings.TrimSpace(issueID) == "" {
		return nil, fmt.Errorf("issue id is required")
	}

	repoRoot, err := m.getRepoRoot()
	if err != nil {
		return nil, err
	}

	branches, err := git.GetBranchCommitTimes(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to list branches: %w", err)
	}

	return filterBranchesForIssue(branches, issueID), nil
}

// ContinueRun launches a continue run by invoking the orch binary.
func (m *Monitor) ContinueRun(issueID, branch, agentType, prompt string) (string, error) {
	args := append([]string{}, m.globalFlags...)
	args = append(args, "continue", "--issue", issueID, "--branch", branch)
	if agentType != "" {
		args = append(args, "--agent", agentType)
	}

	cmd := exec.Command(m.orchPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// If prompt is provided, we need to inject it after the run starts
	// For now, we'll pass it via environment variable or stdin
	// The continue command doesn't have a --prompt flag, so we'll handle this differently
	if prompt != "" {
		// Store the prompt to be injected via tmux after the session starts
		cmd.Env = append(os.Environ(), fmt.Sprintf("ORCH_CONTINUE_PROMPT=%s", prompt))
	}

	err := cmd.Run()

	output := strings.TrimSpace(strings.TrimSpace(stdout.String()) + "\n" + strings.TrimSpace(stderr.String()))
	if err != nil {
		if strings.TrimSpace(output) == "" {
			output = err.Error()
		}
		return output, err
	}
	if strings.TrimSpace(output) == "" {
		output = "run continued"
	}
	return output, nil
}

func (m *Monitor) getRepoRoot() (string, error) {
	// Try to find the repo root from the store vault path
	vaultPath := m.store.VaultPath()
	if vaultPath == "" {
		return "", fmt.Errorf("vault path not set")
	}
	return git.FindMainRepoRoot(vaultPath)
}

func (m *Monitor) createSession() error {
	cmd := m.runsDashboardCommand()
	cfg := &tmux.SessionConfig{
		SessionName: m.session,
		Command:     cmd,
		WindowName:  dashboardWindowName,
	}
	if err := tmux.NewSession(cfg); err != nil {
		return fmt.Errorf("failed to create monitor session: %w", err)
	}
	return nil
}

func (m *Monitor) attachSession() error {
	if os.Getenv("TMUX") != "" {
		return tmux.SwitchClient(m.session)
	}
	return tmux.AttachSession(m.session)
}

func (m *Monitor) ensurePaneLayout() error {
	if !tmux.HasSession(m.session) {
		return nil
	}

	target := fmt.Sprintf("%s:%d", m.session, dashboardWindowIdx)
	panes, err := tmux.ListPanes(target)
	if err != nil {
		return err
	}

	if hasPaneLayout(panes) {
		m.syncPaneOptions(panes)
		return nil
	}

	if len(panes) > 0 {
		base := panes[0]
		for _, p := range panes {
			if p.Index < base.Index {
				base = p
			}
		}
		for _, p := range panes {
			if p.ID != base.ID {
				_ = tmux.KillPane(p.ID)
			}
		}
		_ = tmux.SetPaneTitle(base.ID, runsPaneTitle)
		if chatPane, err := tmux.SplitWindow(base.ID, true, 25); err == nil {
			_ = tmux.SetPaneTitle(chatPane, chatPaneTitle)
			launch := m.agentChatLaunch()
			_ = tmux.SendKeys(chatPane, launch.command)
			m.sendAgentChatPrompt(chatPane, launch)
			_ = tmux.SetOption(m.session, chatPaneOption, chatPane)
		} else {
			return err
		}
		if issuesPane, err := tmux.SplitWindow(base.ID, false, 0); err == nil {
			_ = tmux.SetPaneTitle(issuesPane, issuesPaneTitle)
			_ = tmux.SendKeys(issuesPane, m.issuesDashboardCommand())
			_ = tmux.SetOption(m.session, issuesPaneOption, issuesPane)
		} else {
			return err
		}
		_ = tmux.SetOption(m.session, runsPaneOption, base.ID)
		return nil
	}

	return fmt.Errorf("failed to initialize panes")
}

func (m *Monitor) loadRuns() ([]*RunWindow, error) {
	filter := &store.ListRunsFilter{
		IssueID: m.issueFilter,
		Limit:   100,
	}

	statuses := m.statusFilter
	if len(statuses) == 0 {
		statuses = defaultStatuses()
	}
	filter.Status = statuses

	runs, err := m.store.ListRuns(filter)
	if err != nil {
		return nil, err
	}

	runWindows := make([]*RunWindow, 0, len(runs))
	for i, run := range runs {
		sessionName := run.TmuxSession
		if sessionName == "" {
			sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
		}
		runWindows = append(runWindows, &RunWindow{
			Index:        i + 1,
			Run:          run,
			AgentSession: sessionName,
		})
	}

	return runWindows, nil
}

func (m *Monitor) ensureRunSession(w *RunWindow) error {
	if tmux.HasSession(w.AgentSession) {
		return nil
	}
	if w.Run.WorktreePath == "" {
		return nil
	}
	return tmux.NewSession(&tmux.SessionConfig{
		SessionName: w.AgentSession,
		WorkDir:     w.Run.WorktreePath,
	})
}

func (m *Monitor) buildRunRows(windows []*RunWindow) ([]RunRow, error) {
	type issueDisplay struct {
		status string
		topic  string
	}

	issueInfo := make(map[string]issueDisplay)
	for _, w := range windows {
		if w == nil || w.Run == nil {
			continue
		}
		if _, ok := issueInfo[w.Run.IssueID]; ok {
			continue
		}
		issue, err := m.store.ResolveIssue(w.Run.IssueID)
		if err != nil {
			continue
		}
		status := "-"
		if issue.Frontmatter != nil && issue.Frontmatter["status"] != "" {
			status = issue.Frontmatter["status"]
		}
		topic := formatIssueTopic(issue)
		if topic == "" {
			topic = "-"
		}
		issueInfo[w.Run.IssueID] = issueDisplay{
			status: status,
			topic:  topic,
		}
	}

	runList := make([]*model.Run, 0, len(windows))
	for _, w := range windows {
		if w != nil && w.Run != nil {
			runList = append(runList, w.Run)
		}
	}
	baseBranch := ""
	if cfg, err := config.Load(); err == nil && cfg.BaseBranch != "" {
		baseBranch = cfg.BaseBranch
	}
	gitStates := gitStatesForRuns(runList, baseBranch)

	// Populate PR info
	prInfoMap := pr.PopulateRunInfo(runList)

	rows := make([]RunRow, 0, len(windows))
	for _, w := range windows {
		if w == nil || w.Run == nil {
			continue
		}
		info := issueInfo[w.Run.IssueID]
		issueStatus := info.status
		if issueStatus == "" {
			issueStatus = "-"
		}
		topic := info.topic
		if topic == "" {
			topic = "-"
		}
		agent := w.Run.Agent
		if agent == "" {
			agent = "-"
		}

		// Build PR display string and state
		prDisplay := "-"
		prState := ""
		if prInfo := prInfoMap[w.Run.Branch]; prInfo != nil {
			prDisplay = fmt.Sprintf("#%d", prInfo.Number)
			prState = strings.ToLower(prInfo.State)
		} else if w.Run.PRUrl != "" || w.Run.Status == model.StatusPROpen {
			prDisplay = "yes"
		}

		merged := "-"
		if state, ok := gitStates[w.Run.RunID]; ok {
			merged = state
		}
		shortID := w.Run.ShortID()
		if w.Run.WorktreePath != "" {
			if _, err := os.Stat(w.Run.WorktreePath); os.IsNotExist(err) {
				shortID += "*"
			}
		}
		rows = append(rows, RunRow{
			Index:       w.Index,
			ShortID:     shortID,
			IssueID:     w.Run.IssueID,
			IssueStatus: issueStatus,
			Agent:       agent,
			Status:      w.Run.Status,
			PR:          prDisplay,
			PRState:     prState,
			Merged:      merged,
			Updated:     w.Run.UpdatedAt,
			Topic:       topic,
			Run:         w.Run,
		})
	}

	sortRunRows(rows, m.runSort)
	return rows, nil
}

func (m *Monitor) buildIssueRows(issues []*model.Issue, runs []*model.Run) []IssueRow {
	runsByIssue := make(map[string][]*model.Run)
	for _, run := range runs {
		runsByIssue[run.IssueID] = append(runsByIssue[run.IssueID], run)
	}

	rows := make([]IssueRow, 0, len(issues))
	for i, issue := range issues {
		status := string(issue.Status)
		if status == "" {
			status = string(model.IssueStatusOpen)
		}

		summary := issue.Summary
		if strings.TrimSpace(summary) == "" {
			summary = issue.Title
		}
		if strings.TrimSpace(summary) == "" {
			summary = "-"
		}

		var latest *model.Run
		activeCount := 0
		for _, run := range runsByIssue[issue.ID] {
			if latest == nil || run.UpdatedAt.After(latest.UpdatedAt) {
				latest = run
			}
			if isActiveStatus(run.Status) {
				activeCount++
			}
		}

		row := IssueRow{
			Index:      i + 1,
			ID:         issue.ID,
			Status:     status,
			Summary:    summary,
			ActiveRuns: activeCount,
			Issue:      issue,
		}
		if latest != nil {
			row.LatestRunID = latest.RunID
			row.LatestStatus = latest.Status
			row.LatestUpdated = latest.UpdatedAt
		}
		rows = append(rows, row)
	}

	sortIssueRows(rows, m.issueSort)
	return rows
}

func (m *Monitor) runsDashboardCommand() string {
	args := append([]string{m.orchPath}, m.globalFlags...)
	args = append(args, "monitor", "--dashboard")
	if m.issueFilter != "" {
		args = append(args, "--issue", m.issueFilter)
	}
	for _, status := range m.statusFilter {
		args = append(args, "--status", string(status))
	}
	if m.runSort != "" {
		args = append(args, "--sort-runs", string(m.runSort))
	}
	if m.issueSort != "" {
		args = append(args, "--sort-issues", string(m.issueSort))
	}
	return shellJoin(args)
}

func (m *Monitor) issuesDashboardCommand() string {
	args := append([]string{m.orchPath}, m.globalFlags...)
	args = append(args, "monitor", "--issues-dashboard")
	if m.runSort != "" {
		args = append(args, "--sort-runs", string(m.runSort))
	}
	if m.issueSort != "" {
		args = append(args, "--sort-issues", string(m.issueSort))
	}
	return shellJoin(args)
}

type agentChatLaunch struct {
	command      string
	prompt       string
	injection    agent.InjectionMethod
	readyPattern string
}

func (m *Monitor) agentChatLaunch() agentChatLaunch {
	// Write the control prompt file with dynamic repo context
	_, err := writeControlPromptFile(m.store)
	if err != nil {
		return agentChatLaunch{command: fallbackChatCommand(fmt.Sprintf("failed to write prompt file: %v", err))}
	}

	// Use the instruction to read the prompt file
	prompt := GetControlPromptInstruction()

	agentName := strings.TrimSpace(m.agent)
	if agentName == "" {
		cfg, err := config.Load()
		if err == nil {
			agentName = cfg.Agent
		}
	}
	if agentName == "" {
		agentName = "claude"
	}
	aType, err := agent.ParseAgentType(agentName)
	if err != nil {
		return agentChatLaunch{command: fallbackChatCommand(err.Error())}
	}
	adapter, err := agent.GetAdapter(aType)
	if err != nil {
		return agentChatLaunch{command: fallbackChatCommand(err.Error())}
	}
	if !adapter.IsAvailable() {
		return agentChatLaunch{command: fallbackChatCommand(fmt.Sprintf("%s CLI not available", agentName))}
	}

	cmd, err := adapter.LaunchCommand(&agent.LaunchConfig{
		Type:      aType,
		VaultPath: m.store.VaultPath(),
		Prompt:    prompt,
	})
	if err != nil {
		return agentChatLaunch{command: fallbackChatCommand(err.Error())}
	}

	return agentChatLaunch{
		command:      cmd,
		prompt:       prompt,
		injection:    adapter.PromptInjection(),
		readyPattern: adapter.ReadyPattern(),
	}
}

func (m *Monitor) sendAgentChatPrompt(pane string, launch agentChatLaunch) {
	if launch.injection != agent.InjectionTmux || launch.prompt == "" {
		return
	}
	paneID := pane
	prompt := launch.prompt
	pattern := launch.readyPattern
	go func() {
		if pattern != "" {
			_ = tmux.WaitForReady(paneID, pattern, 30*time.Second)
		}
		_ = tmux.SendKeys(paneID, prompt)
	}()
}

func defaultStatuses() []model.Status {
	return []model.Status{
		model.StatusRunning,
		model.StatusBlocked,
		model.StatusBlockedAPI,
		model.StatusBooting,
		model.StatusQueued,
		model.StatusPROpen,
	}
}

func isTerminalStatus(status model.Status) bool {
	switch status {
	case model.StatusDone, model.StatusFailed, model.StatusCanceled:
		return true
	default:
		return false
	}
}

func isActiveStatus(status model.Status) bool {
	switch status {
	case model.StatusRunning, model.StatusBlocked, model.StatusBlockedAPI, model.StatusBooting, model.StatusQueued, model.StatusPROpen:
		return true
	default:
		return false
	}
}

func shellJoin(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"\\$&;|<>*?[]{}()!") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func resolveOrchPath(path string) string {
	if path != "" && filepath.IsAbs(path) {
		return path
	}
	execPath, err := os.Executable()
	if err == nil && execPath != "" {
		if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
			execPath = resolved
		}
		return execPath
	}
	if path != "" {
		if abs, err := filepath.Abs(path); err == nil {
			return abs
		}
	}
	if path != "" {
		return path
	}
	return os.Args[0]
}

func (m *Monitor) selectPaneByTitle(title string) error {
	pane, err := m.findPaneByTitle(m.session, title)
	if err != nil {
		return err
	}
	return tmux.SelectPane(pane)
}

func (m *Monitor) selectPaneByOption(option, fallbackTitle string) error {
	pane, err := m.findPaneByOption(option)
	if err == nil {
		return tmux.SelectPane(pane)
	}
	return m.selectPaneByTitle(fallbackTitle)
}

func (m *Monitor) findChatPane() (string, error) {
	if pane, err := m.findPaneByOption(chatPaneOption); err == nil {
		return pane, nil
	}
	target := fmt.Sprintf("%s:%d", m.session, dashboardWindowIdx)
	panes, err := tmux.ListPanes(target)
	if err != nil {
		return "", err
	}
	if len(panes) == 0 {
		return "", fmt.Errorf("no panes found in %s", target)
	}
	for _, pane := range panes {
		if pane.Title == chatPaneTitle {
			return pane.ID, nil
		}
	}
	for _, pane := range panes {
		if pane.Title != runsPaneTitle && pane.Title != issuesPaneTitle {
			return pane.ID, nil
		}
	}
	return "", fmt.Errorf("pane not found: %s", chatPaneTitle)
}

func (m *Monitor) findPaneByOption(option string) (string, error) {
	value, err := tmux.GetOption(m.session, option)
	if err == nil && value != "" {
		if m.paneExists(value) {
			return value, nil
		}
	}
	return "", fmt.Errorf("pane not found for option: %s", option)
}

func (m *Monitor) findPaneByTitle(session, title string) (string, error) {
	window := dashboardWindowIdx
	if session != m.session {
		window = 0
	}
	target := fmt.Sprintf("%s:%d", session, window)
	panes, err := tmux.ListPanes(target)
	if err != nil {
		return "", err
	}
	if title == "" {
		if len(panes) == 0 {
			return "", fmt.Errorf("no panes found in %s", target)
		}
		return panes[0].ID, nil
	}
	for _, pane := range panes {
		if pane.Title == title {
			return pane.ID, nil
		}
	}
	return "", fmt.Errorf("pane not found: %s", title)
}

func hasPaneLayout(panes []tmux.Pane) bool {
	if len(panes) != 3 {
		return false
	}
	foundRuns := false
	foundIssues := false
	for _, pane := range panes {
		if pane.Title == runsPaneTitle {
			foundRuns = true
		}
		if pane.Title == issuesPaneTitle {
			foundIssues = true
		}
	}
	return foundRuns && foundIssues
}

func (m *Monitor) paneExists(id string) bool {
	target := fmt.Sprintf("%s:%d", m.session, dashboardWindowIdx)
	panes, err := tmux.ListPanes(target)
	if err != nil {
		return false
	}
	for _, pane := range panes {
		if pane.ID == id {
			return true
		}
	}
	return false
}

func (m *Monitor) syncPaneOptions(panes []tmux.Pane) {
	var runsID, issuesID, chatID string
	for _, pane := range panes {
		switch pane.Title {
		case runsPaneTitle:
			runsID = pane.ID
		case issuesPaneTitle:
			issuesID = pane.ID
		}
	}
	for _, pane := range panes {
		if pane.ID != runsID && pane.ID != issuesID {
			chatID = pane.ID
			break
		}
	}
	if runsID != "" {
		_ = tmux.SetOption(m.session, runsPaneOption, runsID)
	}
	if issuesID != "" {
		_ = tmux.SetOption(m.session, issuesPaneOption, issuesID)
	}
	if chatID != "" {
		_ = tmux.SetOption(m.session, chatPaneOption, chatID)
	}
}

func (m *Monitor) ensureChatPaneTitle(panes []tmux.Pane) {
	var runsID, issuesID string
	for _, pane := range panes {
		switch pane.Title {
		case runsPaneTitle:
			runsID = pane.ID
		case issuesPaneTitle:
			issuesID = pane.ID
		}
	}
	for _, pane := range panes {
		if pane.ID == runsID || pane.ID == issuesID {
			continue
		}
		if pane.Title != chatPaneTitle {
			_ = tmux.SetPaneTitle(pane.ID, chatPaneTitle)
		}
		return
	}
}

func (m *Monitor) refreshChatPaneTitle() {
	target := fmt.Sprintf("%s:%d", m.session, dashboardWindowIdx)
	panes, err := tmux.ListPanes(target)
	if err != nil {
		return
	}
	m.ensureChatPaneTitle(panes)
}

func (m *Monitor) repairSwappedMonitorChat() error {
	target := fmt.Sprintf("%s:%d", m.session, dashboardWindowIdx)
	panes, err := tmux.ListPanes(target)
	if err != nil {
		return err
	}
	if len(panes) == 0 {
		return nil
	}

	var runsID, issuesID string
	var chatPane tmux.Pane
	for _, pane := range panes {
		switch pane.Title {
		case runsPaneTitle:
			runsID = pane.ID
		case issuesPaneTitle:
			issuesID = pane.ID
		}
	}
	for _, pane := range panes {
		if pane.ID != runsID && pane.ID != issuesID {
			chatPane = pane
			break
		}
	}
	if chatPane.ID == "" || chatPane.Title == chatPaneTitle {
		return nil
	}

	ref, err := model.ParseRunRef(chatPane.Title)
	if err != nil || ref.RunID == "" {
		_ = tmux.SetPaneTitle(chatPane.ID, chatPaneTitle)
		return nil
	}
	run, err := m.store.GetRun(ref)
	if err != nil {
		_ = tmux.SetPaneTitle(chatPane.ID, chatPaneTitle)
		return nil
	}
	sessionName := run.TmuxSession
	if sessionName == "" {
		sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
	}
	if !tmux.HasSession(sessionName) {
		_ = tmux.SetPaneTitle(chatPane.ID, chatPaneTitle)
		return nil
	}
	if err := m.repairSwappedRunSession(run, sessionName); err != nil {
		return err
	}
	return nil
}

func (m *Monitor) repairSwappedRunSession(run *model.Run, sessionName string) error {
	if run == nil {
		return nil
	}
	target := fmt.Sprintf("%s:%d", sessionName, 0)
	panes, err := tmux.ListPanes(target)
	if err != nil {
		return err
	}
	if len(panes) == 0 {
		return nil
	}
	runPane := panes[0]
	if runPane.Title != chatPaneTitle {
		return nil
	}

	chatPaneID, err := m.findChatPane()
	if err != nil {
		return nil
	}
	monitorTarget := fmt.Sprintf("%s:%d", m.session, dashboardWindowIdx)
	monitorPanes, err := tmux.ListPanes(monitorTarget)
	if err != nil {
		return err
	}
	var monitorChatPane tmux.Pane
	for _, pane := range monitorPanes {
		if pane.ID == chatPaneID {
			monitorChatPane = pane
			break
		}
	}
	if monitorChatPane.ID == "" || monitorChatPane.Title == chatPaneTitle {
		return nil
	}
	if err := tmux.SwapPane(runPane.ID, monitorChatPane.ID); err != nil {
		return err
	}
	_ = tmux.SetPaneTitle(monitorChatPane.ID, chatPaneTitle)
	_ = tmux.SetPaneTitle(runPane.ID, run.Ref().String())
	return nil
}

func (m *Monitor) resolveRunWindowID(run *model.Run, sessionName string) (string, error) {
	windows, err := tmux.ListWindows(sessionName)
	if err != nil {
		return "", err
	}
	if run != nil && run.TmuxWindowID != "" {
		if _, ok := windowIndexByID(windows, run.TmuxWindowID); ok {
			return run.TmuxWindowID, nil
		}
	}
	for _, window := range windows {
		if window.Index == 0 {
			return window.ID, nil
		}
	}
	if len(windows) > 0 {
		return windows[0].ID, nil
	}
	return "", nil
}

func windowIndexByID(windows []tmux.Window, id string) (int, bool) {
	for _, window := range windows {
		if window.ID == id {
			return window.Index, true
		}
	}
	return 0, false
}

func nextAvailableWindowIndex(windows []tmux.Window, start int) int {
	used := make(map[int]bool, len(windows))
	for _, window := range windows {
		used[window.Index] = true
	}
	for idx := start; ; idx++ {
		if !used[idx] {
			return idx
		}
	}
}
