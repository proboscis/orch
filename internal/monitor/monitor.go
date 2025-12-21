package monitor

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/model"
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

// Options configures the monitor behavior.
type Options struct {
	Session     string
	Issue       string
	Statuses    []model.Status
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
	store        store.Store
	orchPath     string
	globalFlags  []string
	attach       bool
	forceNew     bool
	runs         []*RunWindow
	dashboard    *Dashboard
	activeRun    string
	activeTitle  string
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
		session = defaultSessionName
	}
	orchPath := opts.OrchPath
	if orchPath == "" {
		orchPath = os.Args[0]
	}
	return &Monitor{
		session:      session,
		issueFilter:  opts.Issue,
		statusFilter: opts.Statuses,
		store:        st,
		orchPath:     orchPath,
		globalFlags:  opts.GlobalFlags,
		attach:       opts.Attach,
		forceNew:     opts.ForceNew,
	}
}

// Start creates or attaches to the monitor tmux session.
func (m *Monitor) Start() error {
	if !tmux.IsTmuxAvailable() {
		return fmt.Errorf("tmux is not available")
	}

	if m.forceNew && tmux.HasSession(m.session) {
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
	if err := m.CloseRunPane(); err != nil {
		return err
	}
	return m.selectPaneByTitle(runsPaneTitle)
}

// SwitchIssues switches to the issues dashboard window.
func (m *Monitor) SwitchIssues() error {
	if err := m.CloseRunPane(); err != nil {
		return err
	}
	return m.selectPaneByTitle(issuesPaneTitle)
}

// SwitchChat switches to the agent chat window.
func (m *Monitor) SwitchChat() error {
	if err := m.CloseRunPane(); err != nil {
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
	if err := m.CloseRunPane(); err != nil {
		return err
	}

	chatPane, err := m.findChatPane()
	if err != nil {
		return err
	}
	runPane, err := m.findPaneByTitle(sessionName, "")
	if err != nil {
		return err
	}
	if err := tmux.SwapPane(runPane, chatPane); err != nil {
		return err
	}

	title := runWindowTitle(run)
	_ = tmux.SetPaneTitle(runPane, title)
	_ = tmux.SetPaneTitle(chatPane, chatPaneTitle)
	m.activeRun = sessionName
	m.activeTitle = title
	return tmux.SelectPane(runPane)
}

// CloseRunPane restores the chat pane if a run is open.
func (m *Monitor) CloseRunPane() error {
	if m.activeRun == "" {
		return nil
	}
	runTitle := m.activeTitle
	runPane, err := m.findPaneByTitle(m.session, runTitle)
	if err != nil {
		m.activeRun = ""
		m.activeTitle = ""
		return nil
	}
	chatPane, err := m.findPaneByTitle(m.activeRun, chatPaneTitle)
	if err != nil {
		m.activeRun = ""
		m.activeTitle = ""
		return nil
	}
	if err := tmux.SwapPane(chatPane, runPane); err != nil {
		return err
	}
	_ = tmux.SetPaneTitle(chatPane, chatPaneTitle)
	_ = tmux.SetPaneTitle(runPane, runTitle)
	m.activeRun = ""
	m.activeTitle = ""
	return nil
}

// Quit terminates the monitor tmux session.
func (m *Monitor) Quit() error {
	return tmux.KillSession(m.session)
}

// AnswerQuestion appends an answer event for a run.
func (m *Monitor) AnswerQuestion(run *model.Run, questionID, text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("answer text is required")
	}
	return m.store.AppendEvent(run.Ref(), model.NewAnswerEvent(questionID, text, "user"))
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
func (m *Monitor) StartRun(issueID string) (string, error) {
	args := append([]string{}, m.globalFlags...)
	args = append(args, "run", issueID)

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
func (m *Monitor) SetIssueStatus(issueID, status string) error {
	return m.store.SetIssueStatus(issueID, status)
}

// ListIssues fetches issues from the store.
func (m *Monitor) ListIssues() ([]*model.Issue, error) {
	return m.store.ListIssues()
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
			_ = tmux.SendKeys(chatPane, m.agentChatCommand())
		} else {
			return err
		}
		if issuesPane, err := tmux.SplitWindow(base.ID, false, 0); err == nil {
			_ = tmux.SetPaneTitle(issuesPane, issuesPaneTitle)
			_ = tmux.SendKeys(issuesPane, m.issuesDashboardCommand())
		} else {
			return err
		}
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
	issueSummaries := make(map[string]string)
	for _, w := range windows {
		if _, ok := issueSummaries[w.Run.IssueID]; ok {
			continue
		}
		issue, err := m.store.ResolveIssue(w.Run.IssueID)
		if err != nil {
			continue
		}
		summary := issue.Summary
		if summary == "" {
			summary = issue.Title
		}
		issueSummaries[w.Run.IssueID] = summary
	}

	rows := make([]RunRow, 0, len(windows))
	for _, w := range windows {
		summary := issueSummaries[w.Run.IssueID]
		if summary == "" {
			summary = "-"
		}
		rows = append(rows, RunRow{
			Index:   w.Index,
			ShortID: w.Run.ShortID(),
			IssueID: w.Run.IssueID,
			Status:  w.Run.Status,
			Summary: summary,
			Updated: w.Run.UpdatedAt,
			Run:     w.Run,
		})
	}

	return rows, nil
}

func (m *Monitor) buildIssueRows(issues []*model.Issue, runs []*model.Run) []IssueRow {
	sort.Slice(issues, func(i, j int) bool {
		return issues[i].ID < issues[j].ID
	})

	runsByIssue := make(map[string][]*model.Run)
	for _, run := range runs {
		runsByIssue[run.IssueID] = append(runsByIssue[run.IssueID], run)
	}

	rows := make([]IssueRow, 0, len(issues))
	for i, issue := range issues {
		status := "-"
		if issue.Frontmatter != nil && issue.Frontmatter["status"] != "" {
			status = issue.Frontmatter["status"]
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
	return shellJoin(args)
}

func (m *Monitor) issuesDashboardCommand() string {
	args := append([]string{m.orchPath}, m.globalFlags...)
	args = append(args, "monitor", "--issues-dashboard")
	return shellJoin(args)
}

func (m *Monitor) agentChatCommand() string {
	prompt := buildAgentChatPrompt(m.store.VaultPath())

	cfg, err := config.Load()
	if err != nil {
		return fallbackChatCommand("failed to load config")
	}

	agentName := cfg.Agent
	if agentName == "" {
		agentName = "claude"
	}
	aType, err := agent.ParseAgentType(agentName)
	if err != nil {
		return fallbackChatCommand(err.Error())
	}
	adapter, err := agent.GetAdapter(aType)
	if err != nil {
		return fallbackChatCommand(err.Error())
	}
	if !adapter.IsAvailable() {
		return fallbackChatCommand(fmt.Sprintf("%s CLI not available", agentName))
	}

	cmd, err := adapter.LaunchCommand(&agent.LaunchConfig{
		Type:      aType,
		VaultPath: m.store.VaultPath(),
		Prompt:    prompt,
	})
	if err != nil {
		return fallbackChatCommand(err.Error())
	}
	return cmd
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
	case model.StatusDone, model.StatusFailed, model.StatusCanceled, model.StatusResolved:
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

func runWindowTitle(run *model.Run) string {
	if run == nil {
		return "run"
	}
	return fmt.Sprintf("%s#%s", run.IssueID, run.RunID)
}

func (m *Monitor) selectPaneByTitle(title string) error {
	pane, err := m.findPaneByTitle(m.session, title)
	if err != nil {
		return err
	}
	return tmux.SelectPane(pane)
}

func (m *Monitor) findChatPane() (string, error) {
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
