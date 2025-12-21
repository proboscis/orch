package monitor

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/s22625/orch/internal/model"
)

// Mode represents the current UI mode
type Mode int

const (
	ModeNormal Mode = iota
	ModeAnswer
	ModeStop
	ModeNewRun
	ModeHelp
)

// RunRow represents a single run in the dashboard table
type RunRow struct {
	Index    int          // 1-9 for quick switching
	ShortID  string       // 6-char hex ID
	IssueID  string       // Issue identifier
	Status   model.Status // Current status
	Ago      string       // Time since last update
	Summary  string       // Brief description
	Run      *model.Run   // Full run data
	Questions int         // Number of pending questions
}

// Dashboard is the bubbletea model for the monitor dashboard
type Dashboard struct {
	runs      []RunRow
	cursor    int
	width     int
	height    int
	mode      Mode
	styles    Styles
	err       error
	refreshAt time.Time

	// For answer/stop modes
	selectedRun int
	inputBuffer string

	// Callbacks
	onSwitchToRun func(index int)
	onAnswer      func(runIndex int, answer string)
	onStop        func(runIndex int)
	onNewRun      func()
	onRefresh     func()
	onQuit        func()
}

// NewDashboard creates a new dashboard model
func NewDashboard() *Dashboard {
	return &Dashboard{
		runs:      []RunRow{},
		cursor:    0,
		mode:      ModeNormal,
		styles:    DefaultStyles(),
		refreshAt: time.Now(),
	}
}

// SetRuns updates the run list
func (d *Dashboard) SetRuns(runs []RunRow) {
	d.runs = runs
	d.refreshAt = time.Now()
}

// SetCallbacks sets the action callbacks
func (d *Dashboard) SetCallbacks(
	onSwitchToRun func(index int),
	onAnswer func(runIndex int, answer string),
	onStop func(runIndex int),
	onNewRun func(),
	onRefresh func(),
	onQuit func(),
) {
	d.onSwitchToRun = onSwitchToRun
	d.onAnswer = onAnswer
	d.onStop = onStop
	d.onNewRun = onNewRun
	d.onRefresh = onRefresh
	d.onQuit = onQuit
}

// TickMsg is sent periodically to refresh the display
type TickMsg time.Time

// RefreshMsg triggers a data refresh
type RefreshMsg struct{}

// Init initializes the dashboard
func (d *Dashboard) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		tickCmd(),
	)
}

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// Update handles messages
func (d *Dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return d.handleKey(msg)

	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
		return d, nil

	case TickMsg:
		if d.onRefresh != nil {
			d.onRefresh()
		}
		return d, tickCmd()

	case RefreshMsg:
		if d.onRefresh != nil {
			d.onRefresh()
		}
		return d, nil
	}

	return d, nil
}

func (d *Dashboard) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch d.mode {
	case ModeHelp:
		// Any key exits help
		d.mode = ModeNormal
		return d, nil

	case ModeAnswer:
		return d.handleAnswerKey(msg)

	case ModeStop:
		return d.handleStopKey(msg)

	case ModeNewRun:
		return d.handleNewRunKey(msg)

	default:
		return d.handleNormalKey(msg)
	}
}

func (d *Dashboard) handleNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		if d.onQuit != nil {
			d.onQuit()
		}
		return d, tea.Quit

	case "?":
		d.mode = ModeHelp
		return d, nil

	case "r":
		if d.onRefresh != nil {
			d.onRefresh()
		}
		return d, nil

	case "a":
		// Enter answer mode if there are blocked runs
		for i, r := range d.runs {
			if r.Status == model.StatusBlocked && r.Questions > 0 {
				d.mode = ModeAnswer
				d.selectedRun = i
				d.inputBuffer = ""
				return d, nil
			}
		}
		return d, nil

	case "s":
		// Enter stop mode if there are stoppable runs
		for i, r := range d.runs {
			if r.Status == model.StatusRunning || r.Status == model.StatusBlocked || r.Status == model.StatusBooting {
				d.mode = ModeStop
				d.selectedRun = i
				return d, nil
			}
		}
		return d, nil

	case "n":
		if d.onNewRun != nil {
			d.onNewRun()
		}
		return d, nil

	case "up", "k":
		if d.cursor > 0 {
			d.cursor--
		}
		return d, nil

	case "down", "j":
		if d.cursor < len(d.runs)-1 {
			d.cursor++
		}
		return d, nil

	case "enter":
		if len(d.runs) > 0 && d.onSwitchToRun != nil {
			d.onSwitchToRun(d.runs[d.cursor].Index)
		}
		return d, nil

	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		num := int(msg.String()[0] - '0')
		for _, r := range d.runs {
			if r.Index == num && d.onSwitchToRun != nil {
				d.onSwitchToRun(num)
				return d, nil
			}
		}
		return d, nil
	}

	return d, nil
}

func (d *Dashboard) handleAnswerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		d.mode = ModeNormal
		d.inputBuffer = ""
		return d, nil

	case "enter":
		if d.inputBuffer != "" && d.onAnswer != nil {
			d.onAnswer(d.runs[d.selectedRun].Index, d.inputBuffer)
			d.mode = ModeNormal
			d.inputBuffer = ""
		}
		return d, nil

	case "backspace":
		if len(d.inputBuffer) > 0 {
			d.inputBuffer = d.inputBuffer[:len(d.inputBuffer)-1]
		}
		return d, nil

	case "up":
		// Navigate to previous blocked run
		for i := d.selectedRun - 1; i >= 0; i-- {
			if d.runs[i].Status == model.StatusBlocked && d.runs[i].Questions > 0 {
				d.selectedRun = i
				return d, nil
			}
		}
		return d, nil

	case "down":
		// Navigate to next blocked run
		for i := d.selectedRun + 1; i < len(d.runs); i++ {
			if d.runs[i].Status == model.StatusBlocked && d.runs[i].Questions > 0 {
				d.selectedRun = i
				return d, nil
			}
		}
		return d, nil

	default:
		// Add character to input buffer
		if len(msg.String()) == 1 {
			d.inputBuffer += msg.String()
		}
		return d, nil
	}
}

func (d *Dashboard) handleStopKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		d.mode = ModeNormal
		return d, nil

	case "y", "Y":
		if d.onStop != nil {
			d.onStop(d.runs[d.selectedRun].Index)
		}
		d.mode = ModeNormal
		return d, nil

	case "n", "N":
		d.mode = ModeNormal
		return d, nil

	case "up":
		// Navigate to previous stoppable run
		for i := d.selectedRun - 1; i >= 0; i-- {
			s := d.runs[i].Status
			if s == model.StatusRunning || s == model.StatusBlocked || s == model.StatusBooting {
				d.selectedRun = i
				return d, nil
			}
		}
		return d, nil

	case "down":
		// Navigate to next stoppable run
		for i := d.selectedRun + 1; i < len(d.runs); i++ {
			s := d.runs[i].Status
			if s == model.StatusRunning || s == model.StatusBlocked || s == model.StatusBooting {
				d.selectedRun = i
				return d, nil
			}
		}
		return d, nil
	}

	return d, nil
}

func (d *Dashboard) handleNewRunKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		d.mode = ModeNormal
		return d, nil
	}
	return d, nil
}

// View renders the dashboard
func (d *Dashboard) View() string {
	if d.width == 0 {
		return "Loading..."
	}

	switch d.mode {
	case ModeHelp:
		return d.renderHelp()
	case ModeAnswer:
		return d.renderAnswerMode()
	case ModeStop:
		return d.renderStopMode()
	default:
		return d.renderNormal()
	}
}

func (d *Dashboard) renderNormal() string {
	var b strings.Builder

	// Calculate available width for content (accounting for borders)
	contentWidth := d.width - 4
	if contentWidth < 60 {
		contentWidth = 60
	}

	// Title
	title := d.styles.Title.Render(" ORCH MONITOR ")
	titleLine := lipgloss.NewStyle().
		Width(contentWidth).
		Align(lipgloss.Center).
		Render(title)

	b.WriteString(titleLine)
	b.WriteString("\n\n")

	// Table header
	header := fmt.Sprintf("  %-3s %-8s %-12s %-10s %-6s %s",
		"#", "ID", "ISSUE", "STATUS", "AGO", "SUMMARY")
	b.WriteString(d.styles.Header.Render(header))
	b.WriteString("\n")

	// Table rows
	if len(d.runs) == 0 {
		b.WriteString(d.styles.Muted.Render("  No active runs\n"))
	} else {
		for i, r := range d.runs {
			row := d.renderRow(r, i == d.cursor)
			b.WriteString(row)
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")

	// Status summary
	running, blocked, done, failed := d.countStatuses()
	summary := d.styles.StatusSummary(running, blocked, done, failed)
	b.WriteString("  " + summary)
	b.WriteString("\n\n")

	// Help bar
	helpLine := d.styles.Muted.Render("  [1-9] attach   [a] answer   [s] stop   [n] new   [r] refresh   [?] help   [q] quit")
	b.WriteString(helpLine)

	// Wrap in box
	content := b.String()
	box := d.styles.FullBox.
		Width(contentWidth + 2).
		Render(content)

	return box
}

func (d *Dashboard) renderRow(r RunRow, selected bool) string {
	// Truncate summary if too long
	summary := r.Summary
	maxSummary := d.width - 50
	if maxSummary < 10 {
		maxSummary = 10
	}
	if len(summary) > maxSummary {
		summary = summary[:maxSummary-3] + "..."
	}

	// Truncate issue if too long
	issue := r.IssueID
	if len(issue) > 12 {
		issue = issue[:12]
	}

	row := fmt.Sprintf("  %-3d %-8s %-12s %-10s %-6s %s",
		r.Index,
		r.ShortID,
		issue,
		string(r.Status),
		r.Ago,
		summary,
	)

	// Style the status part
	styledRow := fmt.Sprintf("  %-3d %-8s %-12s %s %-6s %s",
		r.Index,
		r.ShortID,
		issue,
		d.styles.StyleStatus(string(r.Status))+strings.Repeat(" ", 10-len(string(r.Status))),
		r.Ago,
		summary,
	)

	if selected {
		return d.styles.Selected.Render(row)
	}
	return styledRow
}

func (d *Dashboard) renderAnswerMode() string {
	var b strings.Builder

	contentWidth := d.width - 4
	if contentWidth < 60 {
		contentWidth = 60
	}

	title := d.styles.Title.Render(" ANSWER QUESTION ")
	titleLine := lipgloss.NewStyle().
		Width(contentWidth).
		Align(lipgloss.Center).
		Render(title)

	b.WriteString(titleLine)
	b.WriteString("\n\n")

	b.WriteString(d.styles.Normal.Render("  Blocked runs with pending questions:\n\n"))

	for i, r := range d.runs {
		if r.Status != model.StatusBlocked || r.Questions == 0 {
			continue
		}

		selected := i == d.selectedRun
		prefix := "  "
		if selected {
			prefix = "> "
		}

		line := fmt.Sprintf("%s[%d] %s - %d question(s)", prefix, r.Index, r.IssueID, r.Questions)
		if selected {
			b.WriteString(d.styles.Selected.Render(line))
		} else {
			b.WriteString(d.styles.Normal.Render(line))
		}
		b.WriteString("\n")

		// Show question text for selected run
		if selected && r.Run != nil {
			questions := r.Run.UnansweredQuestions()
			for j, q := range questions {
				text := q.Attrs["text"]
				if len(text) > 60 {
					text = text[:57] + "..."
				}
				qLine := fmt.Sprintf("      Q%d: %s", j+1, text)
				b.WriteString(d.styles.Muted.Render(qLine))
				b.WriteString("\n")
			}
		}
	}

	b.WriteString("\n")
	b.WriteString(d.styles.Normal.Render("  Your answer: "))
	b.WriteString(d.inputBuffer)
	b.WriteString("_")
	b.WriteString("\n\n")

	helpLine := d.styles.Muted.Render("  [Enter] submit   [↑↓] select run   [Esc] cancel")
	b.WriteString(helpLine)

	content := b.String()
	box := d.styles.FullBox.
		Width(contentWidth + 2).
		Render(content)

	return box
}

func (d *Dashboard) renderStopMode() string {
	var b strings.Builder

	contentWidth := d.width - 4
	if contentWidth < 60 {
		contentWidth = 60
	}

	title := d.styles.Title.Render(" STOP RUN ")
	titleLine := lipgloss.NewStyle().
		Width(contentWidth).
		Align(lipgloss.Center).
		Render(title)

	b.WriteString(titleLine)
	b.WriteString("\n\n")

	if d.selectedRun < len(d.runs) {
		r := d.runs[d.selectedRun]
		b.WriteString(d.styles.Normal.Render(fmt.Sprintf("  Stop run %s (%s)?\n\n", r.IssueID, r.ShortID)))
		b.WriteString(d.styles.Muted.Render("  This will cancel the agent session.\n\n"))
	}

	helpLine := d.styles.Muted.Render("  [y] confirm   [n] cancel   [↑↓] select run   [Esc] cancel")
	b.WriteString(helpLine)

	content := b.String()
	box := d.styles.FullBox.
		Width(contentWidth + 2).
		Render(content)

	return box
}

func (d *Dashboard) renderHelp() string {
	var b strings.Builder

	contentWidth := d.width - 4
	if contentWidth < 60 {
		contentWidth = 60
	}

	title := d.styles.Title.Render(" HELP ")
	titleLine := lipgloss.NewStyle().
		Width(contentWidth).
		Align(lipgloss.Center).
		Render(title)

	b.WriteString(titleLine)
	b.WriteString("\n\n")

	help := []struct{ key, desc string }{
		{"1-9", "Attach to run by index"},
		{"Enter", "Attach to selected run"},
		{"↑/k, ↓/j", "Navigate run list"},
		{"a", "Answer mode - respond to blocked questions"},
		{"s", "Stop mode - cancel a run"},
		{"n", "New run - start a new issue"},
		{"r", "Refresh run list"},
		{"?", "Show this help"},
		{"q", "Quit monitor"},
	}

	for _, h := range help {
		line := fmt.Sprintf("  %-12s %s\n", h.key, h.desc)
		b.WriteString(d.styles.Normal.Render(line))
	}

	b.WriteString("\n")
	b.WriteString(d.styles.Header.Render("  tmux shortcuts:\n"))
	b.WriteString(d.styles.Normal.Render("  Ctrl-b 0     Return to dashboard\n"))
	b.WriteString(d.styles.Normal.Render("  Ctrl-b n/p   Next/previous window\n"))
	b.WriteString(d.styles.Normal.Render("  Ctrl-b w     Window picker\n"))

	b.WriteString("\n")
	helpLine := d.styles.Muted.Render("  Press any key to return")
	b.WriteString(helpLine)

	content := b.String()
	box := d.styles.FullBox.
		Width(contentWidth + 2).
		Render(content)

	return box
}

func (d *Dashboard) countStatuses() (running, blocked, done, failed int) {
	for _, r := range d.runs {
		switch r.Status {
		case model.StatusRunning, model.StatusBooting:
			running++
		case model.StatusBlocked:
			blocked++
		case model.StatusDone, model.StatusPROpen:
			done++
		case model.StatusFailed:
			failed++
		}
	}
	return
}

// FormatAgo formats a duration as a human-readable "ago" string
func FormatAgo(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "<1m"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
