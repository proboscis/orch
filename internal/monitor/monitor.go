package monitor

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
	"github.com/s22625/orch/internal/tmux"
)

const defaultSessionName = "orch-monitor"

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

// SwitchWindow selects a tmux window by index.
func (m *Monitor) SwitchWindow(index int) error {
	return tmux.SelectWindow(m.session, index)
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

// ListIssues fetches issues from the store.
func (m *Monitor) ListIssues() ([]*model.Issue, error) {
	return m.store.ListIssues()
}

func (m *Monitor) createSession() error {
	cmd := m.dashboardCommand()
	cfg := &tmux.SessionConfig{
		SessionName: m.session,
		Command:     cmd,
		WindowName:  "dashboard",
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
		if w.Index == 0 {
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

func (m *Monitor) dashboardCommand() string {
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
