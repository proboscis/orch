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
	defaultSessionName = "orch-monitor"
	runsWindowIndex    = 0
	issuesWindowIndex  = 1
	agentWindowIndex   = 2
	runWindowOffset    = 3
)

const (
	runsWindowName   = "runs"
	issuesWindowName = "issues"
	agentWindowName  = "agent"
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
}

// RunWindow links a run to a tmux window index.
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

	if err := m.ensureAuxWindows(); err != nil {
		return err
	}

	runs, err := m.loadRuns()
	if err != nil {
		return err
	}
	m.runs = runs
	if err := m.syncWindows(runs); err != nil {
		return err
	}

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
	if tmux.HasSession(m.session) {
		if err := m.syncWindows(runs); err != nil {
			return nil, err
		}
	}
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
	return m.SwitchWindow(runsWindowIndex)
}

// SwitchIssues switches to the issues dashboard window.
func (m *Monitor) SwitchIssues() error {
	return m.SwitchWindow(issuesWindowIndex)
}

// SwitchChat switches to the agent chat window.
func (m *Monitor) SwitchChat() error {
	return m.SwitchWindow(agentWindowIndex)
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

// ListIssues fetches issues from the store.
func (m *Monitor) ListIssues() ([]*model.Issue, error) {
	return m.store.ListIssues()
}

func (m *Monitor) createSession() error {
	cmd := m.runsDashboardCommand()
	cfg := &tmux.SessionConfig{
		SessionName: m.session,
		Command:     cmd,
		WindowName:  runsWindowName,
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

func (m *Monitor) ensureAuxWindows() error {
	if !tmux.HasSession(m.session) {
		return nil
	}

	windows, err := tmux.ListWindows(m.session)
	if err != nil {
		return err
	}

	indexToName := make(map[int]string, len(windows))
	nameToIndex := make(map[string]int, len(windows))
	for _, w := range windows {
		indexToName[w.Index] = w.Name
		nameToIndex[w.Name] = w.Index
	}

	if name, ok := indexToName[issuesWindowIndex]; ok && name != issuesWindowName {
		_ = tmux.UnlinkWindow(m.session, issuesWindowIndex)
	}
	if _, ok := nameToIndex[issuesWindowName]; !ok {
		if err := tmux.NewWindow(m.session, issuesWindowName, "", m.issuesDashboardCommand()); err != nil {
			return err
		}
	}

	if name, ok := indexToName[agentWindowIndex]; ok && name != agentWindowName {
		_ = tmux.UnlinkWindow(m.session, agentWindowIndex)
	}
	if _, ok := nameToIndex[agentWindowName]; !ok {
		workDir, _ := os.Getwd()
		if err := tmux.NewWindow(m.session, agentWindowName, workDir, m.agentChatCommand()); err != nil {
			return err
		}
	}

	windows, err = tmux.ListWindows(m.session)
	if err != nil {
		return err
	}
	nameToIndex = make(map[string]int, len(windows))
	for _, w := range windows {
		nameToIndex[w.Name] = w.Index
	}

	if index, ok := nameToIndex[issuesWindowName]; ok && index != issuesWindowIndex {
		if err := tmux.MoveWindow(m.session, issuesWindowName, issuesWindowIndex); err != nil {
			return err
		}
	}
	if index, ok := nameToIndex[agentWindowName]; ok && index != agentWindowIndex {
		if err := tmux.MoveWindow(m.session, agentWindowName, agentWindowIndex); err != nil {
			return err
		}
	}

	return nil
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
			Index:        runWindowOffset + i,
			Run:          run,
			AgentSession: sessionName,
		})
	}

	return runWindows, nil
}

func (m *Monitor) syncWindows(windows []*RunWindow) error {
	existing, err := tmux.ListWindows(m.session)
	if err != nil {
		return fmt.Errorf("failed to list windows: %w", err)
	}

	desired := make(map[int]*RunWindow)
	for _, w := range windows {
		desired[w.Index] = w
	}

	for _, w := range existing {
		if isReservedWindowIndex(w.Index) {
			continue
		}
		if _, ok := desired[w.Index]; !ok {
			_ = tmux.UnlinkWindow(m.session, w.Index)
		}
	}

	for _, w := range windows {
		if err := m.ensureRunSession(w); err != nil {
			return err
		}
		if !tmux.HasSession(w.AgentSession) {
			continue
		}
		_ = tmux.UnlinkWindow(m.session, w.Index)
		if err := tmux.LinkWindow(w.AgentSession, 0, m.session, w.Index); err != nil {
			return fmt.Errorf("failed to link window %d: %w", w.Index, err)
		}
	}

	return nil
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
	// Write the control prompt file with dynamic repo context
	_, err := writeControlPromptFile(m.store)
	if err != nil {
		return fallbackChatCommand(fmt.Sprintf("failed to write prompt file: %v", err))
	}

	// Use the instruction to read the prompt file
	prompt := GetControlPromptInstruction()

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

func isReservedWindowIndex(index int) bool {
	switch index {
	case runsWindowIndex, issuesWindowIndex, agentWindowIndex:
		return true
	default:
		return false
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
