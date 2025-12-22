package monitor

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/s22625/orch/internal/model"
)

type dashboardMode int

const (
	modeDashboard dashboardMode = iota
	modeStopSelectRun
	modeNewSelectIssue
)

type stopState struct {
	runs []RunRow
}

type newRunState struct {
	issues  []*model.Issue
	cursor  int
	loading bool
}

// Dashboard is the bubbletea model for the monitor UI.
type Dashboard struct {
	monitor *Monitor

	runs   []RunRow
	cursor int
	offset int
	width  int
	height int

	mode    dashboardMode
	message string

	stop   stopState
	newRun newRunState

	keymap KeyMap
	styles Styles

	lastRefresh     time.Time
	refreshing      bool
	refreshInterval time.Duration
}

type refreshMsg struct {
	rows []RunRow
}

type tickMsg time.Time

type issuesMsg struct {
	issues []*model.Issue
}

type infoMsg struct {
	text string
}

type errMsg struct {
	err error
}

// NewDashboard creates a dashboard model.
func NewDashboard(m *Monitor) *Dashboard {
	return &Dashboard{
		monitor:         m,
		keymap:          DefaultKeyMap(),
		styles:          DefaultStyles(),
		mode:            modeDashboard,
		refreshInterval: defaultRefreshInterval,
	}
}

// Run starts the bubbletea program.
func (d *Dashboard) Run() error {
	program := tea.NewProgram(d, tea.WithAltScreen())
	_, err := program.Run()
	return err
}

// Init implements tea.Model.
func (d *Dashboard) Init() tea.Cmd {
	d.refreshing = true
	return tea.Batch(d.refreshCmd(), d.tickCmd())
}

// Update implements tea.Model.
func (d *Dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
		return d, nil
	case refreshMsg:
		d.runs = msg.rows
		d.refreshing = false
		d.lastRefresh = time.Now()
		if d.cursor >= len(d.runs) {
			d.cursor = len(d.runs) - 1
			if d.cursor < 0 {
				d.cursor = 0
			}
		}
		d.ensureCursorVisible()
		return d, nil
	case issuesMsg:
		d.newRun.issues = msg.issues
		d.newRun.cursor = 0
		d.newRun.loading = false
		return d, nil
	case infoMsg:
		d.message = msg.text
		d.refreshing = true
		return d, d.refreshCmd()
	case errMsg:
		d.message = msg.err.Error()
		d.refreshing = false
		return d, nil
	case tickMsg:
		if d.refreshing {
			return d, d.tickCmd()
		}
		d.refreshing = true
		return d, tea.Batch(d.refreshCmd(), d.tickCmd())
	case tea.KeyMsg:
		return d.handleKey(msg)
	default:
		return d, nil
	}
}

// View implements tea.Model.
func (d *Dashboard) View() string {
	switch d.mode {
	case modeStopSelectRun:
		return d.styles.Box.Render(d.viewStopRuns())
	case modeNewSelectIssue:
		return d.styles.Box.Render(d.viewNewRun())
	default:
		return d.styles.Box.Render(d.viewDashboard())
	}
}

func (d *Dashboard) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return d.quit()
	}

	switch d.mode {
	case modeDashboard:
		return d.handleDashboardKey(msg)
	case modeStopSelectRun:
		return d.handleStopKey(msg)
	case modeNewSelectIssue:
		return d.handleNewRunKey(msg)
	default:
		return d, nil
	}
}

func (d *Dashboard) handleDashboardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return d.quit()
	case d.keymap.Runs:
		if err := d.monitor.SwitchRuns(); err != nil {
			d.message = err.Error()
		}
		return d, nil
	case d.keymap.Issues:
		if err := d.monitor.SwitchIssues(); err != nil {
			d.message = err.Error()
		}
		return d, nil
	case d.keymap.Chat:
		if err := d.monitor.SwitchChat(); err != nil {
			d.message = err.Error()
		}
		return d, nil
	case "r":
		d.refreshing = true
		return d, d.refreshCmd()
	case "s":
		return d.enterStopMode()
	case "n":
		return d.enterNewRunMode()
	case d.keymap.Resolve:
		if d.cursor >= 0 && d.cursor < len(d.runs) {
			run := d.runs[d.cursor].Run
			return d, d.resolveRunCmd(run)
		}
		return d, nil
	case "up", "k":
		if d.cursor > 0 {
			d.cursor--
		}
		d.ensureCursorVisible()
		return d, nil
	case "down", "j":
		if d.cursor < len(d.runs)-1 {
			d.cursor++
		}
		d.ensureCursorVisible()
		return d, nil
	case "enter":
		if d.cursor >= 0 && d.cursor < len(d.runs) {
			run := d.runs[d.cursor].Run
			if err := d.monitor.OpenRun(run); err != nil {
				d.message = err.Error()
			}
		}
		return d, nil
	case "?":
		d.message = d.keymap.HelpLine()
		return d, nil
	}

	return d, nil
}

func (d *Dashboard) handleStopKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		d.mode = modeDashboard
		return d, nil
	case "q":
		return d.quit()
	}

	if index, ok := parseNumberKey(msg); ok {
		for _, row := range d.stop.runs {
			if row.Index == index {
				cmd := d.stopCmd(row.Run)
				d.mode = modeDashboard
				return d, cmd
			}
		}
		d.message = "run not in stop list"
	}
	return d, nil
}

func (d *Dashboard) handleNewRunKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		d.mode = modeDashboard
		return d, nil
	case "q":
		return d.quit()
	case "up", "k":
		if d.newRun.cursor > 0 {
			d.newRun.cursor--
		}
		return d, nil
	case "down", "j":
		if d.newRun.cursor < len(d.newRun.issues)-1 {
			d.newRun.cursor++
		}
		return d, nil
	case "enter":
		if d.newRun.cursor >= 0 && d.newRun.cursor < len(d.newRun.issues) {
			issueID := d.newRun.issues[d.newRun.cursor].ID
			d.mode = modeDashboard
			return d, d.startRunCmd(issueID)
		}
		return d, nil
	}

	if index, ok := parseNumberKey(msg); ok {
		if index >= 1 && index <= len(d.newRun.issues) {
			issueID := d.newRun.issues[index-1].ID
			d.mode = modeDashboard
			return d, d.startRunCmd(issueID)
		}
	}
	return d, nil
}

func (d *Dashboard) quit() (tea.Model, tea.Cmd) {
	_ = d.monitor.Quit()
	return d, tea.Quit
}

func (d *Dashboard) enterStopMode() (tea.Model, tea.Cmd) {
	var runs []RunRow
	for _, row := range d.runs {
		if row.Run == nil {
			continue
		}
		if isTerminalStatus(row.Status) {
			continue
		}
		runs = append(runs, row)
	}
	if len(runs) == 0 {
		d.message = "no active runs to stop"
		return d, nil
	}
	d.stop = stopState{runs: runs}
	d.mode = modeStopSelectRun
	return d, nil
}

func (d *Dashboard) enterNewRunMode() (tea.Model, tea.Cmd) {
	d.newRun = newRunState{loading: true}
	d.mode = modeNewSelectIssue
	return d, d.loadIssuesCmd()
}

func (d *Dashboard) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		rows, err := d.monitor.Refresh()
		if err != nil {
			return errMsg{err: err}
		}
		return refreshMsg{rows: rows}
	}
}

func (d *Dashboard) tickCmd() tea.Cmd {
	return tea.Tick(d.refreshInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (d *Dashboard) loadIssuesCmd() tea.Cmd {
	return func() tea.Msg {
		issues, err := d.monitor.ListIssues()
		if err != nil {
			return errMsg{err: err}
		}
		sort.Slice(issues, func(i, j int) bool {
			return issues[i].ID < issues[j].ID
		})
		return issuesMsg{issues: issues}
	}
}

func (d *Dashboard) startRunCmd(issueID string) tea.Cmd {
	return func() tea.Msg {
		output, err := d.monitor.StartRun(issueID)
		if err != nil {
			return errMsg{err: fmt.Errorf("%s", output)}
		}
		return infoMsg{text: output}
	}
}

func (d *Dashboard) stopCmd(run *model.Run) tea.Cmd {
	return func() tea.Msg {
		if run == nil {
			return errMsg{err: fmt.Errorf("run not found")}
		}
		if err := d.monitor.StopRun(run); err != nil {
			return errMsg{err: err}
		}
		return infoMsg{text: fmt.Sprintf("stopped %s#%s", run.IssueID, run.RunID)}
	}
}

func (d *Dashboard) resolveRunCmd(run *model.Run) tea.Cmd {
	return func() tea.Msg {
		if run == nil {
			return errMsg{err: fmt.Errorf("run not found")}
		}
		if err := d.monitor.ResolveRun(run); err != nil {
			return errMsg{err: err}
		}
		return infoMsg{text: fmt.Sprintf("resolved %s#%s and issue %s", run.IssueID, run.RunID, run.IssueID)}
	}
}

func (d *Dashboard) viewDashboard() string {
	title := d.styles.Title.Render("ORCH MONITOR")
	meta := d.renderMeta()
	table := d.renderTable(d.tableMaxRows())
	stats := d.renderStats()
	footer := d.renderFooter()
	message := ""
	if d.message != "" {
		message = d.styles.Faint.Render(d.message)
	}

	lines := []string{
		title,
		"",
		meta,
		"",
		table,
		"",
		stats,
	}
	if message != "" {
		lines = append(lines, "", message)
	}
	lines = append(lines, "", footer)
	return strings.Join(lines, "\n")
}

func (d *Dashboard) viewStopRuns() string {
	lines := []string{
		d.styles.Title.Render("STOP RUN"),
		"",
		"Select a run to stop:",
		"",
	}
	for _, row := range d.stop.runs {
		line := fmt.Sprintf("  [%d] %s#%s (%s)", row.Index, row.IssueID, row.Run.RunID, row.Status)
		lines = append(lines, truncate(line, d.safeWidth()-2))
	}
	lines = append(lines, "", "Select run [1-9], or [Esc] to cancel.")
	return strings.Join(lines, "\n")
}

func (d *Dashboard) viewNewRun() string {
	lines := []string{
		d.styles.Title.Render("NEW RUN"),
		"",
	}
	if d.newRun.loading {
		lines = append(lines, "Loading issues...")
		return strings.Join(lines, "\n")
	}
	if len(d.newRun.issues) == 0 {
		lines = append(lines, "No issues found.")
		return strings.Join(lines, "\n")
	}

	lines = append(lines, "Select an issue:", "")
	for i, issue := range d.newRun.issues {
		label := fmt.Sprintf("  [%d] %s - %s", i+1, issue.ID, issue.Summary)
		label = truncate(label, d.safeWidth()-2)
		if i == d.newRun.cursor {
			label = d.styles.Selected.Render(label)
		}
		lines = append(lines, label)
	}
	lines = append(lines, "", "[Enter] start  [Esc] cancel")
	return strings.Join(lines, "\n")
}

func (d *Dashboard) renderTable(maxRows int) string {
	if len(d.runs) == 0 {
		return "No runs found."
	}

	idxW, idW, issueW, issueStatusW, agentW, statusW, prW, mergedW, updatedW, topicW := d.tableWidths()

	header := d.renderRow(idxW, idW, issueW, issueStatusW, agentW, statusW, prW, mergedW, updatedW, topicW,
		"#", "ID", "ISSUE", "ISSUE-ST", "AGENT", "STATUS", "PR", "MERGED", "UPDATED", "TOPIC", true, nil)

	var rows []string
	visibleRows := d.runVisibleRows(maxRows)
	start := d.offset
	end := len(d.runs)
	if visibleRows > 0 {
		end = start + visibleRows
		if end > len(d.runs) {
			end = len(d.runs)
		}
	} else {
		end = start
	}
	for i, row := range d.runs[start:end] {
		r := d.renderRow(idxW, idW, issueW, issueStatusW, agentW, statusW, prW, mergedW, updatedW, topicW,
			fmt.Sprintf("%d", row.Index),
			row.ShortID,
			row.IssueID,
			row.IssueStatus,
			row.Agent,
			string(row.Status),
			row.PR,
			row.Merged,
			formatRelativeTime(row.Updated, time.Now()),
			row.Topic,
			false,
			&row,
		)
		if i+start == d.cursor {
			r = d.styles.Selected.Render(r)
		}
		rows = append(rows, r)
	}

	return strings.Join(append([]string{header}, rows...), "\n")
}

func (d *Dashboard) renderRow(idxW, idW, issueW, issueStatusW, agentW, statusW, prW, mergedW, updatedW, topicW int, idx, id, issue, issueStatus, agent, status, pr, merged, updated, topic string, header bool, row *RunRow) string {
	baseStyle := d.styles.Text
	headerStyle := d.styles.Header

	idxCol := d.pad(idx, idxW, baseStyle)
	idCol := d.pad(id, idW, baseStyle)
	issueCol := d.pad(issue, issueW, baseStyle)
	issueStatusCol := d.pad(issueStatus, issueStatusW, baseStyle)
	agentCol := d.pad(agent, agentW, baseStyle)
	updatedCol := d.pad(updated, updatedW, baseStyle)
	topicCol := d.pad(topic, topicW, baseStyle)
	statusCol := d.pad(status, statusW, baseStyle)
	prCol := d.pad(pr, prW, baseStyle)
	mergedCol := d.pad(merged, mergedW, baseStyle)

	if header {
		idxCol = d.pad(idx, idxW, headerStyle)
		idCol = d.pad(id, idW, headerStyle)
		issueCol = d.pad(issue, issueW, headerStyle)
		issueStatusCol = d.pad(issueStatus, issueStatusW, headerStyle)
		agentCol = d.pad(agent, agentW, headerStyle)
		updatedCol = d.pad(updated, updatedW, headerStyle)
		topicCol = d.pad(topic, topicW, headerStyle)
		statusCol = d.pad(status, statusW, headerStyle)
		prCol = d.pad(pr, prW, headerStyle)
		mergedCol = d.pad(merged, mergedW, headerStyle)
	}

	if row != nil {
		if style, ok := d.styles.Status[row.Status]; ok {
			statusCol = d.pad(status, statusW, style)
		}
		// Apply PR state styling
		if row.PRState != "" {
			if style, ok := d.styles.PRState[row.PRState]; ok {
				prCol = d.pad(pr, prW, style)
			}
		}
	}

	return strings.Join([]string{idxCol, idCol, issueCol, issueStatusCol, agentCol, statusCol, prCol, mergedCol, updatedCol, topicCol}, "  ")
}

func (d *Dashboard) renderStats() string {
	counts := make(map[model.Status]int)
	for _, row := range d.runs {
		counts[row.Status]++
	}

	stats := []string{
		fmt.Sprintf("booting: %d", counts[model.StatusBooting]),
		fmt.Sprintf("queued: %d", counts[model.StatusQueued]),
		fmt.Sprintf("running: %d", counts[model.StatusRunning]),
		fmt.Sprintf("blocked: %d", counts[model.StatusBlocked]),
		fmt.Sprintf("blocked_api: %d", counts[model.StatusBlockedAPI]),
		fmt.Sprintf("pr_open: %d", counts[model.StatusPROpen]),
		fmt.Sprintf("done: %d", counts[model.StatusDone]),
		fmt.Sprintf("failed: %d", counts[model.StatusFailed]),
		fmt.Sprintf("canceled: %d", counts[model.StatusCanceled]),
	}

	return strings.Join(stats, "  ")
}

func (d *Dashboard) renderMeta() string {
	filterParts := []string{}
	if d.monitor.issueFilter != "" {
		filterParts = append(filterParts, fmt.Sprintf("issue=%s", d.monitor.issueFilter))
	}
	if len(d.monitor.statusFilter) > 0 {
		var statuses []string
		for _, s := range d.monitor.statusFilter {
			statuses = append(statuses, string(s))
		}
		filterParts = append(filterParts, fmt.Sprintf("status=%s", strings.Join(statuses, ",")))
	}
	filter := "filter: all"
	if len(filterParts) > 0 {
		filter = "filter: " + strings.Join(filterParts, " ")
	}

	sync := d.renderSyncStatus()
	nav := d.renderNav()
	rows := d.renderRunRange()
	return strings.Join([]string{filter, sync, nav, rows}, "  ")
}

func (d *Dashboard) renderSyncStatus() string {
	if d.refreshing {
		return "sync: syncing..."
	}
	if d.lastRefresh.IsZero() {
		return "sync: pending"
	}
	ago := formatRelativeTime(d.lastRefresh, time.Now())
	label := fmt.Sprintf("sync: %s", ago)
	if time.Since(d.lastRefresh) > d.refreshInterval*3 {
		label += " (stale)"
	}
	return label
}

func (d *Dashboard) renderNav() string {
	return fmt.Sprintf("nav: [%s] runs  [%s] issues  [%s] chat", d.keymap.Runs, d.keymap.Issues, d.keymap.Chat)
}

func (d *Dashboard) renderRunRange() string {
	if len(d.runs) == 0 {
		return "rows: 0/0"
	}
	visibleRows := d.runVisibleRows(d.tableMaxRows())
	if visibleRows == 0 {
		return fmt.Sprintf("rows: 0/%d", len(d.runs))
	}
	start := d.offset + 1
	if start < 1 {
		start = 1
	}
	end := d.offset + visibleRows
	if end > len(d.runs) {
		end = len(d.runs)
	}
	return fmt.Sprintf("rows: %d-%d/%d", start, end, len(d.runs))
}

func (d *Dashboard) renderFooter() string {
	return d.keymap.HelpLine()
}

func (d *Dashboard) tableWidths() (idxW, idW, issueW, issueStatusW, agentW, statusW, prW, mergedW, updatedW, topicW int) {
	idxW = 2
	idW = 6
	issueW = 10
	issueStatusW = 8
	agentW = 6
	statusW = 10
	prW = 6 // Increased to fit PR numbers like "#1234"
	mergedW = 8
	updatedW = 7
	contentWidth := d.safeWidth()
	fixed := idxW + idW + issueW + issueStatusW + agentW + statusW + prW + mergedW + updatedW + 18
	topicW = contentWidth - fixed
	if topicW < 12 {
		topicW = 12
	}
	return
}

func (d *Dashboard) safeWidth() int {
	if d.width > 2 {
		return d.width - 2
	}
	return 80
}

func (d *Dashboard) safeHeight() int {
	if d.height > 2 {
		return d.height - 2
	}
	return 24
}

func (d *Dashboard) tableMaxRows() int {
	base := 8
	if d.message != "" {
		base += 2
	}
	available := d.safeHeight() - base
	if available <= 1 {
		return 0
	}
	return available - 1
}

func (d *Dashboard) runVisibleRows(maxRows int) int {
	if maxRows <= 0 {
		return 0
	}
	if len(d.runs) < maxRows {
		return len(d.runs)
	}
	return maxRows
}

func (d *Dashboard) pageSize() int {
	rows := d.runVisibleRows(d.tableMaxRows())
	if rows < 1 {
		return 1
	}
	return rows
}

func (d *Dashboard) ensureCursorVisible() {
	if len(d.runs) == 0 {
		d.offset = 0
		return
	}
	visibleRows := d.runVisibleRows(d.tableMaxRows())
	if visibleRows <= 0 {
		d.offset = 0
		return
	}
	if d.cursor < d.offset {
		d.offset = d.cursor
	}
	if d.cursor >= d.offset+visibleRows {
		d.offset = d.cursor - visibleRows + 1
	}
	maxOffset := len(d.runs) - visibleRows
	if maxOffset < 0 {
		maxOffset = 0
	}
	if d.offset > maxOffset {
		d.offset = maxOffset
	}
	if d.offset < 0 {
		d.offset = 0
	}
}

func (d *Dashboard) pad(s string, width int, style lipgloss.Style) string {
	return style.Width(width).Render(truncate(s, width))
}

func parseNumberKey(msg tea.KeyMsg) (int, bool) {
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		return 0, false
	}
	r := msg.Runes[0]
	if r < '1' || r > '9' {
		return 0, false
	}
	return int(r - '0'), true
}

func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	if width <= 3 {
		return string(runes[:width])
	}
	return string(runes[:width-3]) + "..."
}

func wrapText(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}

	var lines []string
	var current strings.Builder
	for _, word := range words {
		if current.Len() == 0 {
			current.WriteString(word)
			continue
		}
		if lipgloss.Width(current.String())+1+lipgloss.Width(word) > width {
			lines = append(lines, current.String())
			current.Reset()
			current.WriteString(word)
			continue
		}
		current.WriteString(" ")
		current.WriteString(word)
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	return lines
}

func formatRelativeTime(when time.Time, now time.Time) string {
	if when.After(now) {
		return "just now"
	}

	elapsed := now.Sub(when)
	switch {
	case elapsed < 10*time.Second:
		return "just now"
	case elapsed < time.Minute:
		return fmt.Sprintf("%ds ago", int(elapsed.Seconds()))
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm ago", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(elapsed.Hours()))
	case elapsed < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(elapsed.Hours()/24))
	default:
		return fmt.Sprintf("%dw ago", int(elapsed.Hours()/(24*7)))
	}
}
