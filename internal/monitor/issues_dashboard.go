package monitor

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/s22625/orch/internal/model"
)

type issueDashboardMode int

const (
	modeIssues issueDashboardMode = iota
	modeCreateIssue
	modeSelectRun
	modeSelectAgent
)

type createIssueState struct {
	step    int
	issueID string
	title   string
	input   string
}

type selectRunState struct {
	issueID string
	runs    []*model.Run
	cursor  int
	offset  int
	loading bool
}

type selectAgentState struct {
	issueID string
	agents  []string
	cursor  int
}

// IssueDashboard is the bubbletea model for the issues UI.
type IssueDashboard struct {
	monitor *Monitor

	issues []IssueRow
	cursor int
	offset int
	width  int
	height int

	mode        issueDashboardMode
	message     string
	create      createIssueState
	selectRun   selectRunState
	selectAgent selectAgentState

	keymap IssueKeyMap
	styles Styles

	lastRefresh     time.Time
	refreshing      bool
	refreshInterval time.Duration
}

type issuesRefreshMsg struct {
	rows []IssueRow
}

type issueRunsMsg struct {
	issueID string
	runs    []*model.Run
}

type issueTickMsg time.Time

// NewIssueDashboard creates an issue dashboard model.
func NewIssueDashboard(m *Monitor) *IssueDashboard {
	return &IssueDashboard{
		monitor:         m,
		keymap:          DefaultIssueKeyMap(),
		styles:          DefaultStyles(),
		mode:            modeIssues,
		refreshInterval: defaultRefreshInterval,
	}
}

// Run starts the bubbletea program.
func (d *IssueDashboard) Run() error {
	program := tea.NewProgram(d, tea.WithAltScreen())
	_, err := program.Run()
	return err
}

// Init implements tea.Model.
func (d *IssueDashboard) Init() tea.Cmd {
	d.refreshing = true
	return tea.Batch(d.refreshCmd(), d.tickCmd())
}

// Update implements tea.Model.
func (d *IssueDashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
		return d, nil
	case issuesRefreshMsg:
		d.issues = msg.rows
		d.refreshing = false
		d.lastRefresh = time.Now()
		if d.cursor >= len(d.issues) {
			d.cursor = len(d.issues) - 1
			if d.cursor < 0 {
				d.cursor = 0
			}
		}
		d.ensureCursorVisible()
		return d, nil
	case issueRunsMsg:
		if msg.issueID != d.selectRun.issueID {
			return d, nil
		}
		d.selectRun.runs = msg.runs
		d.selectRun.loading = false
		if len(d.selectRun.runs) == 0 {
			d.mode = modeIssues
			d.message = fmt.Sprintf("no runs found for %s", msg.issueID)
			return d, nil
		}
		if len(d.selectRun.runs) == 1 {
			d.mode = modeIssues
			return d, d.openRunCmd(d.selectRun.runs[0])
		}
		d.selectRun.cursor = 0
		d.selectRun.offset = 0
		d.ensureSelectRunVisible()
		d.mode = modeSelectRun
		return d, nil
	case infoMsg:
		d.message = msg.text
		d.refreshing = true
		return d, d.refreshCmd()
	case errMsg:
		d.message = msg.err.Error()
		d.refreshing = false
		return d, nil
	case issueTickMsg:
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
func (d *IssueDashboard) View() string {
	switch d.mode {
	case modeCreateIssue:
		return d.styles.Box.Render(d.viewCreateIssue())
	case modeSelectRun:
		return d.styles.Box.Render(d.viewSelectRun())
	case modeSelectAgent:
		return d.styles.Box.Render(d.viewSelectAgent())
	default:
		return d.styles.Box.Render(d.viewIssues())
	}
}

func (d *IssueDashboard) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return d.quit()
	}

	switch d.mode {
	case modeCreateIssue:
		return d.handleCreateIssueKey(msg)
	case modeSelectRun:
		return d.handleSelectRunKey(msg)
	case modeSelectAgent:
		return d.handleSelectAgentKey(msg)
	default:
		return d.handleIssuesKey(msg)
	}
}

func (d *IssueDashboard) handleIssuesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return d.quit()
	case d.keymap.Runs:
		if err := d.monitor.SwitchRuns(); err != nil {
			d.message = err.Error()
		}
		return d, nil
	case d.keymap.Issues:
		return d, nil
	case d.keymap.Chat:
		if err := d.monitor.SwitchChat(); err != nil {
			d.message = err.Error()
		}
		return d, nil
	case d.keymap.Open:
		if row := d.currentIssue(); row != nil {
			return d, d.openIssueCmd(row.ID)
		}
		return d, nil
	case d.keymap.Resolve:
		if row := d.currentIssue(); row != nil {
			return d, d.resolveIssueCmd(row.ID)
		}
		return d, nil
	case d.keymap.OpenRun:
		if row := d.currentIssue(); row != nil {
			d.mode = modeSelectRun
			d.selectRun = selectRunState{issueID: row.ID, loading: true}
			return d, d.loadIssueRunsCmd(row.ID)
		}
		return d, nil
	case d.keymap.StartRun:
		if row := d.currentIssue(); row != nil {
			d.selectAgent = selectAgentState{
				issueID: row.ID,
				agents:  d.monitor.GetAvailableAgents(),
				cursor:  0,
			}
			d.mode = modeSelectAgent
		}
		return d, nil
	case "up", "k":
		if d.cursor > 0 {
			d.cursor--
		}
		d.ensureCursorVisible()
		return d, nil
	case "down", "j":
		if d.cursor < len(d.issues)-1 {
			d.cursor++
		}
		d.ensureCursorVisible()
		return d, nil
	case "?":
		d.message = d.keymap.HelpLine()
		return d, nil
	}
	return d, nil
}

func (d *IssueDashboard) handleSelectRunKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		d.mode = modeIssues
		return d, nil
	case "q":
		return d.quit()
	case d.keymap.StartRun:
		if strings.TrimSpace(d.selectRun.issueID) != "" {
			d.selectAgent = selectAgentState{
				issueID: d.selectRun.issueID,
				agents:  d.monitor.GetAvailableAgents(),
				cursor:  0,
			}
			d.mode = modeSelectAgent
		}
		return d, nil
	case "enter":
		if run := d.currentSelectRun(); run != nil {
			d.mode = modeIssues
			return d, d.openRunCmd(run)
		}
		return d, nil
	case "up", "k":
		if d.selectRun.cursor > 0 {
			d.selectRun.cursor--
		}
		d.ensureSelectRunVisible()
		return d, nil
	case "down", "j":
		if d.selectRun.cursor < len(d.selectRun.runs)-1 {
			d.selectRun.cursor++
		}
		d.ensureSelectRunVisible()
		return d, nil
	case "?":
		d.message = d.keymap.HelpLine()
		return d, nil
	}
	return d, nil
}

func (d *IssueDashboard) handleCreateIssueKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		d.mode = modeIssues
		return d, nil
	case "enter":
		switch d.create.step {
		case 0:
			issueID := strings.TrimSpace(d.create.input)
			if issueID == "" {
				d.message = "issue id is required"
				return d, nil
			}
			d.create.issueID = issueID
			d.create.input = ""
			d.create.step = 1
			return d, nil
		case 1:
			d.create.title = strings.TrimSpace(d.create.input)
			issueID := d.create.issueID
			title := d.create.title
			d.mode = modeIssues
			d.create = createIssueState{}
			return d, d.createIssueCmd(issueID, title)
		}
	}

	switch msg.Type {
	case tea.KeyBackspace, tea.KeyDelete:
		if len(d.create.input) > 0 {
			runes := []rune(d.create.input)
			d.create.input = string(runes[:len(runes)-1])
		}
		return d, nil
	case tea.KeyRunes:
		d.create.input += string(msg.Runes)
		return d, nil
	default:
		return d, nil
	}
}

func (d *IssueDashboard) handleSelectAgentKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		d.mode = modeIssues
		return d, nil
	case "q":
		return d.quit()
	case "enter":
		if d.selectAgent.cursor >= 0 && d.selectAgent.cursor < len(d.selectAgent.agents) {
			agentType := d.selectAgent.agents[d.selectAgent.cursor]
			issueID := d.selectAgent.issueID
			d.mode = modeIssues
			return d, d.startRunCmd(issueID, agentType)
		}
		return d, nil
	case "up", "k":
		if d.selectAgent.cursor > 0 {
			d.selectAgent.cursor--
		}
		return d, nil
	case "down", "j":
		if d.selectAgent.cursor < len(d.selectAgent.agents)-1 {
			d.selectAgent.cursor++
		}
		return d, nil
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(msg.String()[0] - '1')
		if idx >= 0 && idx < len(d.selectAgent.agents) {
			agentType := d.selectAgent.agents[idx]
			issueID := d.selectAgent.issueID
			d.mode = modeIssues
			return d, d.startRunCmd(issueID, agentType)
		}
		return d, nil
	}
	return d, nil
}

func (d *IssueDashboard) quit() (tea.Model, tea.Cmd) {
	_ = d.monitor.Quit()
	return d, tea.Quit
}

func (d *IssueDashboard) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		rows, err := d.monitor.RefreshIssues()
		if err != nil {
			return errMsg{err: err}
		}
		return issuesRefreshMsg{rows: rows}
	}
}

func (d *IssueDashboard) tickCmd() tea.Cmd {
	return tea.Tick(d.refreshInterval, func(t time.Time) tea.Msg {
		return issueTickMsg(t)
	})
}

func (d *IssueDashboard) loadIssueRunsCmd(issueID string) tea.Cmd {
	return func() tea.Msg {
		runs, err := d.monitor.ListRunsForIssue(issueID)
		if err != nil {
			return errMsg{err: err}
		}
		return issueRunsMsg{issueID: issueID, runs: runs}
	}
}

func (d *IssueDashboard) startRunCmd(issueID, agentType string) tea.Cmd {
	return func() tea.Msg {
		output, err := d.monitor.StartRun(issueID, agentType)
		if err != nil {
			return errMsg{err: fmt.Errorf("%s", output)}
		}
		return infoMsg{text: output}
	}
}

func (d *IssueDashboard) openRunCmd(run *model.Run) tea.Cmd {
	return func() tea.Msg {
		if run == nil {
			return errMsg{err: fmt.Errorf("run not found")}
		}
		if err := d.monitor.OpenRun(run); err != nil {
			return errMsg{err: err}
		}
		return infoMsg{text: fmt.Sprintf("opened %s#%s", run.IssueID, run.RunID)}
	}
}

func (d *IssueDashboard) openIssueCmd(issueID string) tea.Cmd {
	return func() tea.Msg {
		output, err := d.monitor.OpenIssue(issueID)
		if err != nil {
			return errMsg{err: fmt.Errorf("%s", output)}
		}
		return infoMsg{text: output}
	}
}

func (d *IssueDashboard) resolveIssueCmd(issueID string) tea.Cmd {
	return func() tea.Msg {
		if err := d.monitor.SetIssueStatus(issueID, model.IssueStatusResolved); err != nil {
			return errMsg{err: err}
		}
		return infoMsg{text: fmt.Sprintf("resolved issue %s", issueID)}
	}
}

func (d *IssueDashboard) createIssueCmd(issueID, title string) tea.Cmd {
	return func() tea.Msg {
		output, err := d.monitor.CreateIssue(issueID, title)
		if err != nil {
			return errMsg{err: fmt.Errorf("%s", output)}
		}
		return infoMsg{text: output}
	}
}

func (d *IssueDashboard) viewIssues() string {
	title := d.styles.Title.Render("ORCH ISSUES")
	meta := d.renderMeta()
	listRows, detailRows := d.layoutHeights()
	table := d.renderTable(listRows)
	details := d.renderDetails(detailRows)
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
		details,
	}
	if message != "" {
		lines = append(lines, "", message)
	}
	lines = append(lines, "", footer)
	return strings.Join(lines, "\n")
}

func (d *IssueDashboard) viewCreateIssue() string {
	header := d.styles.Title.Render("NEW ISSUE")
	lines := []string{header, ""}
	switch d.create.step {
	case 0:
		lines = append(lines, "Issue ID:", fmt.Sprintf("> %s", d.create.input))
		lines = append(lines, "", "[Enter] next  [Esc] cancel")
	case 1:
		lines = append(lines, "Title (optional):", fmt.Sprintf("> %s", d.create.input))
		lines = append(lines, "", "[Enter] create  [Esc] cancel")
	}
	return strings.Join(lines, "\n")
}

func (d *IssueDashboard) viewSelectRun() string {
	header := d.styles.Title.Render("OPEN RUN")
	lines := []string{header, ""}

	issueID := d.selectRun.issueID
	if strings.TrimSpace(issueID) == "" {
		issueID = "-"
	}
	lines = append(lines, fmt.Sprintf("Issue: %s", issueID), "")

	if d.selectRun.loading {
		lines = append(lines, "Loading runs...")
		return strings.Join(lines, "\n")
	}

	if len(d.selectRun.runs) == 0 {
		lines = append(lines, "No runs found.")
		lines = append(lines, "", "[Esc] back  [r] start run")
		return strings.Join(lines, "\n")
	}

	lines = append(lines, "Select a run:", "")
	maxRows := d.selectRunMaxRows()
	visibleRows := d.selectRunVisibleRows(maxRows)
	start := d.selectRun.offset
	end := len(d.selectRun.runs)
	if visibleRows > 0 {
		end = start + visibleRows
		if end > len(d.selectRun.runs) {
			end = len(d.selectRun.runs)
		}
	} else {
		end = start
	}
	for i, run := range d.selectRun.runs[start:end] {
		label := fmt.Sprintf("  %s#%s  %s  %s", run.IssueID, run.RunID, run.Status, formatRelativeTime(run.UpdatedAt, time.Now()))
		label = truncate(label, d.safeWidth()-2)
		if i+start == d.selectRun.cursor {
			label = d.styles.Selected.Render(label)
		}
		lines = append(lines, label)
	}

	lines = append(lines, "", "[Enter] open run  [Esc] back  [r] start run")
	return strings.Join(lines, "\n")
}

func (d *IssueDashboard) viewSelectAgent() string {
	header := d.styles.Title.Render("SELECT AGENT")
	lines := []string{header, ""}

	issueID := d.selectAgent.issueID
	if strings.TrimSpace(issueID) == "" {
		issueID = "-"
	}
	lines = append(lines, fmt.Sprintf("Issue: %s", issueID), "")

	if len(d.selectAgent.agents) == 0 {
		lines = append(lines, "No agents available.")
		lines = append(lines, "", "[Esc] back")
		return strings.Join(lines, "\n")
	}

	lines = append(lines, "Select an agent for the run:", "")
	for i, agent := range d.selectAgent.agents {
		label := fmt.Sprintf("  [%d] %s", i+1, agent)
		if i == d.selectAgent.cursor {
			label = d.styles.Selected.Render(label)
		}
		lines = append(lines, label)
	}

	lines = append(lines, "", "[Enter/1-9] select agent  [Esc] back")
	return strings.Join(lines, "\n")
}

func (d *IssueDashboard) renderMeta() string {
	sync := d.renderSyncStatus()
	total := fmt.Sprintf("issues: %d", len(d.issues))
	nav := d.renderNav()
	rows := d.renderIssueRange()
	return strings.Join([]string{total, sync, nav, rows}, "  ")
}

func (d *IssueDashboard) renderSyncStatus() string {
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

func (d *IssueDashboard) renderNav() string {
	return fmt.Sprintf("nav: [%s] runs  [%s] issues  [%s] chat", d.keymap.Runs, d.keymap.Issues, d.keymap.Chat)
}

func (d *IssueDashboard) renderIssueRange() string {
	if len(d.issues) == 0 {
		return "rows: 0/0"
	}
	visibleRows := d.issueVisibleRows(d.tableMaxRows())
	if visibleRows == 0 {
		return fmt.Sprintf("rows: 0/%d", len(d.issues))
	}
	start := d.offset + 1
	if start < 1 {
		start = 1
	}
	end := d.offset + visibleRows
	if end > len(d.issues) {
		end = len(d.issues)
	}
	return fmt.Sprintf("rows: %d-%d/%d", start, end, len(d.issues))
}

func (d *IssueDashboard) renderTable(maxRows int) string {
	if len(d.issues) == 0 {
		return "No issues found."
	}

	idxW, idW, statusW, latestW, activeW, summaryW := d.tableWidths()

	header := d.renderRow(idxW, idW, statusW, latestW, activeW, summaryW,
		"#", "ID", "STATUS", "LATEST", "ACTIVE", "SUMMARY", true, nil)

	var rows []string
	visibleRows := d.issueVisibleRows(maxRows)
	start := d.offset
	end := len(d.issues)
	if visibleRows > 0 {
		end = start + visibleRows
		if end > len(d.issues) {
			end = len(d.issues)
		}
	} else {
		end = start
	}
	for i, row := range d.issues[start:end] {
		latest := "-"
		if row.LatestRunID != "" {
			latest = string(row.LatestStatus)
		}
		r := d.renderRow(idxW, idW, statusW, latestW, activeW, summaryW,
			fmt.Sprintf("%d", row.Index),
			row.ID,
			row.Status,
			latest,
			fmt.Sprintf("%d", row.ActiveRuns),
			row.Summary,
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

func (d *IssueDashboard) renderRow(idxW, idW, statusW, latestW, activeW, summaryW int, idx, id, status, latest, active, summary string, header bool, row *IssueRow) string {
	baseStyle := d.styles.Text
	headerStyle := d.styles.Header

	idxCol := d.pad(idx, idxW, baseStyle)
	idCol := d.pad(id, idW, baseStyle)
	statusCol := d.pad(status, statusW, baseStyle)
	latestCol := d.pad(latest, latestW, baseStyle)
	activeCol := d.pad(active, activeW, baseStyle)
	summaryCol := d.pad(summary, summaryW, baseStyle)

	if header {
		idxCol = d.pad(idx, idxW, headerStyle)
		idCol = d.pad(id, idW, headerStyle)
		statusCol = d.pad(status, statusW, headerStyle)
		latestCol = d.pad(latest, latestW, headerStyle)
		activeCol = d.pad(active, activeW, headerStyle)
		summaryCol = d.pad(summary, summaryW, headerStyle)
	}

	if row != nil && row.LatestStatus != "" {
		if style, ok := d.styles.Status[row.LatestStatus]; ok {
			latestCol = d.pad(latest, latestW, style)
		}
	}

	return strings.Join([]string{idxCol, idCol, statusCol, latestCol, activeCol, summaryCol}, "  ")
}

func (d *IssueDashboard) renderDetails(maxLines int) string {
	issue := d.currentIssue()
	if issue == nil || issue.Issue == nil {
		return "No issue selected."
	}

	title := issue.Issue.Title
	if strings.TrimSpace(title) == "" {
		title = issue.Issue.ID
	}

	lines := []string{
		d.styles.Header.Render("DETAILS"),
		fmt.Sprintf("ID: %s", issue.ID),
		fmt.Sprintf("Title: %s", title),
		fmt.Sprintf("Status: %s", issue.Status),
		fmt.Sprintf("Active runs: %d", issue.ActiveRuns),
	}

	latest := "-"
	if issue.LatestRunID != "" {
		latest = fmt.Sprintf("%s (%s)", issue.LatestRunID, issue.LatestStatus)
	}
	if issue.LatestRunID != "" {
		updated := formatRelativeTime(issue.LatestUpdated, time.Now())
		lines = append(lines, fmt.Sprintf("Latest run: %s, %s", latest, updated))
	} else {
		lines = append(lines, fmt.Sprintf("Latest run: %s", latest))
	}

	summary := issue.Summary
	if summary == "-" {
		summary = ""
	}
	if strings.TrimSpace(summary) != "" {
		lines = append(lines, "Summary:")
		lines = append(lines, wrapText(summary, d.safeWidth()-2)...)
	}

	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
		if maxLines > 1 {
			lines[maxLines-1] = "..."
		}
	}

	return strings.Join(lines, "\n")
}

func (d *IssueDashboard) renderFooter() string {
	return d.keymap.HelpLine()
}

func (d *IssueDashboard) tableWidths() (idxW, idW, statusW, latestW, activeW, summaryW int) {
	idxW = 2
	idW = 14
	statusW = 10
	latestW = 10
	activeW = 6

	contentWidth := d.safeWidth()
	maxID := idW
	for _, row := range d.issues {
		if w := runewidth.StringWidth(row.ID); w > maxID {
			maxID = w
		}
	}
	summaryMin := 20
	maxIDForSummary := contentWidth - (idxW + statusW + latestW + activeW + 10 + summaryMin)
	if maxIDForSummary < idW {
		maxIDForSummary = idW
	}
	if maxID > maxIDForSummary {
		idW = maxIDForSummary
	} else {
		idW = maxID
	}

	fixed := idxW + idW + statusW + latestW + activeW + 10
	summaryW = contentWidth - fixed
	if summaryW < summaryMin {
		summaryW = summaryMin
	}
	return
}

func (d *IssueDashboard) safeWidth() int {
	frame := d.styles.Box.GetHorizontalFrameSize()
	if d.width > frame {
		return d.width - frame
	}
	return 80
}

func (d *IssueDashboard) safeHeight() int {
	frame := d.styles.Box.GetVerticalFrameSize()
	if d.height > frame {
		return d.height - frame
	}
	return 24
}

func (d *IssueDashboard) pad(s string, width int, style lipgloss.Style) string {
	return style.Width(width).Render(truncate(s, width))
}

func (d *IssueDashboard) currentIssue() *IssueRow {
	if d.cursor < 0 || d.cursor >= len(d.issues) {
		return nil
	}
	return &d.issues[d.cursor]
}

func (d *IssueDashboard) currentSelectRun() *model.Run {
	if d.selectRun.cursor < 0 || d.selectRun.cursor >= len(d.selectRun.runs) {
		return nil
	}
	return d.selectRun.runs[d.selectRun.cursor]
}

func (d *IssueDashboard) tableMaxRows() int {
	listRows, _ := d.layoutHeights()
	return listRows
}

func (d *IssueDashboard) issueVisibleRows(maxRows int) int {
	if maxRows <= 0 {
		return 0
	}
	if len(d.issues) < maxRows {
		return len(d.issues)
	}
	return maxRows
}

func (d *IssueDashboard) pageSize() int {
	rows := d.issueVisibleRows(d.tableMaxRows())
	if rows < 1 {
		return 1
	}
	return rows
}

func (d *IssueDashboard) selectRunMaxRows() int {
	base := 8
	available := d.safeHeight() - base
	if available < 1 {
		return 1
	}
	return available
}

func (d *IssueDashboard) selectRunVisibleRows(maxRows int) int {
	if maxRows <= 0 {
		return 0
	}
	if len(d.selectRun.runs) < maxRows {
		return len(d.selectRun.runs)
	}
	return maxRows
}

func (d *IssueDashboard) ensureCursorVisible() {
	if len(d.issues) == 0 {
		d.offset = 0
		return
	}
	visibleRows := d.issueVisibleRows(d.tableMaxRows())
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
	maxOffset := len(d.issues) - visibleRows
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

func (d *IssueDashboard) ensureSelectRunVisible() {
	if len(d.selectRun.runs) == 0 {
		d.selectRun.offset = 0
		return
	}
	visibleRows := d.selectRunVisibleRows(d.selectRunMaxRows())
	if visibleRows <= 0 {
		d.selectRun.offset = 0
		return
	}
	if d.selectRun.cursor < d.selectRun.offset {
		d.selectRun.offset = d.selectRun.cursor
	}
	if d.selectRun.cursor >= d.selectRun.offset+visibleRows {
		d.selectRun.offset = d.selectRun.cursor - visibleRows + 1
	}
	maxOffset := len(d.selectRun.runs) - visibleRows
	if maxOffset < 0 {
		maxOffset = 0
	}
	if d.selectRun.offset > maxOffset {
		d.selectRun.offset = maxOffset
	}
	if d.selectRun.offset < 0 {
		d.selectRun.offset = 0
	}
}

func (d *IssueDashboard) layoutHeights() (listRows, detailRows int) {
	base := 7
	if d.message != "" {
		base += 2
	}
	available := d.safeHeight() - base
	if available <= 1 {
		return 0, 0
	}

	detailRows = 8
	if detailRows > available-2 {
		detailRows = available / 3
	}
	if detailRows < 3 {
		detailRows = 3
	}
	listRows = available - detailRows - 1
	if listRows < 1 {
		listRows = 1
		detailRows = available - 1
		if detailRows < 0 {
			detailRows = 0
		}
	}
	return listRows, detailRows
}
