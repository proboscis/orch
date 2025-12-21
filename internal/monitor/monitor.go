package monitor

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
	"github.com/s22625/orch/internal/tmux"
)

const (
	// MonitorSession is the tmux session name for the monitor
	MonitorSession = "orch-monitor"
)

// RunWindow represents a run linked to a tmux window
type RunWindow struct {
	WindowIndex  int        // tmux window index (1-9)
	Run          *model.Run // Run data
	AgentSession string     // Original agent tmux session name
}

// Monitor manages the monitor tmux session and dashboard
type Monitor struct {
	session   string        // tmux session name
	store     store.Store   // data store
	runs      []*RunWindow  // active run windows
	dashboard *Dashboard    // bubbletea dashboard
	program   *tea.Program  // bubbletea program

	// Filters
	filterIssue  string
	filterStatus []model.Status
}

// Options configures the monitor
type Options struct {
	Issue  string
	Status []model.Status
	Attach bool
	New    bool
}

// New creates a new monitor
func New(st store.Store, opts *Options) *Monitor {
	m := &Monitor{
		session:   MonitorSession,
		store:     st,
		runs:      make([]*RunWindow, 0),
		dashboard: NewDashboard(),
	}

	if opts != nil {
		m.filterIssue = opts.Issue
		m.filterStatus = opts.Status
	}

	return m
}

// Start starts the monitor
func (m *Monitor) Start() error {
	// Check if we're inside tmux
	if !insideTmux() {
		return m.startInNewTmuxSession()
	}

	// We're inside tmux, check if monitor session exists
	if tmux.HasSession(m.session) {
		// Attach to existing monitor
		return m.attachToExisting()
	}

	// Running inside tmux but not in monitor session
	// Start dashboard in current terminal
	return m.runDashboard()
}

// startInNewTmuxSession creates the monitor session and attaches to it
func (m *Monitor) startInNewTmuxSession() error {
	// Create new tmux session with the monitor command
	cwd, _ := os.Getwd()

	// Get the path to the current executable
	executable, err := os.Executable()
	if err != nil {
		executable = "orch"
	}

	// Create session running orch monitor
	err = tmux.NewSession(&tmux.SessionConfig{
		SessionName: m.session,
		WorkDir:     cwd,
		Command:     executable + " monitor --in-tmux",
	})
	if err != nil {
		return fmt.Errorf("failed to create monitor session: %w", err)
	}

	// Attach to the session
	return tmux.AttachSession(m.session)
}

// attachToExisting attaches to an existing monitor session
func (m *Monitor) attachToExisting() error {
	return tmux.AttachSession(m.session)
}

// runDashboard runs the bubbletea dashboard
func (m *Monitor) runDashboard() error {
	// Load initial run data
	if err := m.refreshRuns(); err != nil {
		return fmt.Errorf("failed to load runs: %w", err)
	}

	// Set up callbacks
	m.dashboard.SetCallbacks(
		m.onSwitchToRun,
		m.onAnswer,
		m.onStop,
		m.onNewRun,
		m.onRefresh,
		m.onQuit,
	)

	// Create and run the bubbletea program
	m.program = tea.NewProgram(m.dashboard, tea.WithAltScreen())
	_, err := m.program.Run()
	return err
}

// refreshRuns loads runs from the store and updates the dashboard
func (m *Monitor) refreshRuns() error {
	filter := &store.ListRunsFilter{
		IssueID: m.filterIssue,
		Status:  m.filterStatus,
		Limit:   9, // Max 9 runs for quick switching
	}

	// If no status filter, show active runs
	if len(filter.Status) == 0 {
		filter.Status = []model.Status{
			model.StatusRunning,
			model.StatusBlocked,
			model.StatusBooting,
		}
	}

	runs, err := m.store.ListRuns(filter)
	if err != nil {
		return err
	}

	// Convert to RunRows
	rows := make([]RunRow, len(runs))
	for i, r := range runs {
		questions := r.UnansweredQuestions()
		summary := m.getRunSummary(r)

		rows[i] = RunRow{
			Index:     i + 1,
			ShortID:   r.ShortID(),
			IssueID:   r.IssueID,
			Status:    r.Status,
			Ago:       FormatAgo(r.UpdatedAt),
			Summary:   summary,
			Run:       r,
			Questions: len(questions),
		}
	}

	m.dashboard.SetRuns(rows)

	// Update run windows list
	m.runs = make([]*RunWindow, len(runs))
	for i, r := range runs {
		m.runs[i] = &RunWindow{
			WindowIndex:  i + 1,
			Run:          r,
			AgentSession: r.TmuxSession,
		}
	}

	return nil
}

// getRunSummary extracts a summary from the run's issue
func (m *Monitor) getRunSummary(r *model.Run) string {
	issue, err := m.store.ResolveIssue(r.IssueID)
	if err != nil {
		return ""
	}
	// Return title or first line of description
	if issue.Title != "" {
		return issue.Title
	}
	return ""
}

// onSwitchToRun switches to a run's tmux window
func (m *Monitor) onSwitchToRun(index int) {
	for _, rw := range m.runs {
		if rw.WindowIndex == index {
			m.switchToRun(rw)
			return
		}
	}
}

// switchToRun attaches to a run's agent session
func (m *Monitor) switchToRun(rw *RunWindow) {
	if rw.AgentSession == "" {
		return
	}

	// Check if agent session exists
	if !tmux.HasSession(rw.AgentSession) {
		return
	}

	// If we're in the monitor session, create/switch to a window that attaches to the agent
	if currentSession() == m.session {
		// Create a window in monitor session that attaches to the agent session
		windowName := fmt.Sprintf("%d-%s", rw.WindowIndex, rw.Run.IssueID)

		// Check if window already exists by trying to select it
		cmd := exec.Command("tmux", "select-window", "-t", m.session+":"+windowName)
		if cmd.Run() != nil {
			// Window doesn't exist, create it
			err := tmux.NewWindow(m.session, windowName, "", "tmux attach-session -t "+rw.AgentSession)
			if err != nil {
				return
			}
		}
	} else {
		// Not in monitor session, just attach to agent session
		tmux.AttachSession(rw.AgentSession)
	}
}

// onAnswer answers a question for a run
func (m *Monitor) onAnswer(runIndex int, answer string) {
	for _, rw := range m.runs {
		if rw.WindowIndex == runIndex {
			m.answerRun(rw, answer)
			return
		}
	}
}

// answerRun submits an answer to a run's pending question
func (m *Monitor) answerRun(rw *RunWindow, answer string) {
	questions := rw.Run.UnansweredQuestions()
	if len(questions) == 0 {
		return
	}

	// Answer the first unanswered question
	q := questions[0]

	// Create answer event
	event := model.NewAnswerEvent(q.Name, answer, "user")
	err := m.store.AppendEvent(rw.Run.Ref(), event)
	if err != nil {
		return
	}

	// Refresh to see updated state
	m.refreshRuns()
}

// onStop stops a run
func (m *Monitor) onStop(runIndex int) {
	for _, rw := range m.runs {
		if rw.WindowIndex == runIndex {
			m.stopRun(rw)
			return
		}
	}
}

// stopRun cancels a run
func (m *Monitor) stopRun(rw *RunWindow) {
	// Add canceled status event
	event := model.NewStatusEvent(model.StatusCanceled)
	m.store.AppendEvent(rw.Run.Ref(), event)

	// Kill the tmux session if it exists
	if rw.AgentSession != "" && tmux.HasSession(rw.AgentSession) {
		tmux.KillSession(rw.AgentSession)
	}

	// Refresh
	m.refreshRuns()
}

// onNewRun starts a new run selection (placeholder for now)
func (m *Monitor) onNewRun() {
	// This would typically open an issue selector
	// For now, just print a message
}

// onRefresh triggers a data refresh
func (m *Monitor) onRefresh() {
	m.refreshRuns()
}

// onQuit handles quit
func (m *Monitor) onQuit() {
	// Cleanup if needed
}

// insideTmux returns true if we're running inside tmux
func insideTmux() bool {
	return os.Getenv("TMUX") != ""
}

// currentSession returns the current tmux session name
func currentSession() string {
	cmd := exec.Command("tmux", "display-message", "-p", "#{session_name}")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// HasMonitorSession checks if the monitor session already exists
func HasMonitorSession() bool {
	return tmux.HasSession(MonitorSession)
}

// AttachToMonitor attaches to an existing monitor session
func AttachToMonitor() error {
	return tmux.AttachSession(MonitorSession)
}
