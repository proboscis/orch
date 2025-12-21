package monitor

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/s22625/orch/internal/model"
)

type issueDashboardMode int

const (
	modeIssues issueDashboardMode = iota
	modeCreateIssue
)

type createIssueState struct {
	step    int
	issueID string
	title   string
	input   string
}

// IssueDashboard is the bubbletea model for the issues UI.
type IssueDashboard struct {
	monitor *Monitor

	issues []IssueRow
	cursor int
	offset int
	width  int
	height int

	mode    issueDashboardMode
	message string
	create  createIssueState

	keymap IssueKeyMap
	styles Styles

	lastRefresh     time.Time
	refreshing      bool
	refreshInterval time.Duration
}

type issuesRefreshMsg struct {
	rows []IssueRow
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
	case "r":
		d.refreshing = true
		return d, d.refreshCmd()
	case "o":
		if row := d.currentIssue(); row != nil {
			return d, d.openIssueCmd(row.ID)
		}
		return d, nil
	case d.keymap.Resolve:
		if row := d.currentIssue(); row != nil {
			return d, d.resolveIssueCmd(row.ID)
		}
		return d, nil
	case "n":
		d.mode = modeCreateIssue
		d.create = createIssueState{}
		return d, nil
	case "enter":
		if row := d.currentIssue(); row != nil {
			return d, d.startRunCmd(row.ID)
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

func (d *IssueDashboard) startRunCmd(issueID string) tea.Cmd {
	return func() tea.Msg {
		output, err := d.monitor.StartRun(issueID)
		if err != nil {
			return errMsg{err: fmt.Errorf("%s", output)}
		}
		return infoMsg{text: output}
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
	idW = 12
	statusW = 10
	latestW = 10
	activeW = 6

	contentWidth := d.safeWidth()
	fixed := idxW + idW + statusW + latestW + activeW + 10
	summaryW = contentWidth - fixed
	if summaryW < 20 {
		summaryW = 20
	}
	return
}

func (d *IssueDashboard) safeWidth() int {
	if d.width > 2 {
		return d.width - 2
	}
	return 80
}

func (d *IssueDashboard) safeHeight() int {
	if d.height > 2 {
		return d.height - 2
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
