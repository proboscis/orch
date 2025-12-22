package monitor

import (
	"fmt"
	"sort"
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
	modeFilter
	modeContinueTarget
	modeContinueAgent
	modeContinuePrompt
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

// filterState holds the current filter settings for the issue list
type filterState struct {
	showResolved bool // Show resolved issues (default: true)
	showClosed   bool // Show closed issues (default: true)
	cursor       int  // Currently selected option in filter dialog
}

// defaultFilterState returns the default filter state
func defaultFilterState() filterState {
	return filterState{
		showResolved: true,
		showClosed:   true,
		cursor:       0,
	}
}

// branchInfo holds info about a branch for the continue dialogue
type branchInfo struct {
	name       string
	commitTime time.Time
}

type continueTargetKind int

const (
	continueTargetRun continueTargetKind = iota
	continueTargetBranch
)

type continueTarget struct {
	kind   continueTargetKind
	run    *model.Run
	branch branchInfo
}

// continueState holds the state for the continue run dialogue
type continueState struct {
	issueID     string
	targets     []continueTarget
	cursor      int
	offset      int
	loading     bool
	agentCursor int
	agent       string
	prompt      string
}

// IssueDashboard is the bubbletea model for the issues UI.
type IssueDashboard struct {
	monitor *Monitor

	issues         []IssueRow
	filteredIssues []IssueRow // Issues after applying filter
	cursor         int
	offset         int
	width          int
	height         int

	mode        issueDashboardMode
	message     string
	create      createIssueState
	selectRun   selectRunState
	selectAgent selectAgentState
	filter      filterState
	continue_   continueState

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

type issueContinueTargetsMsg struct {
	issueID   string
	runs      []*model.Run
	branches  []branchInfo
	branchErr error
}

type issueTickMsg time.Time

// NewIssueDashboard creates an issue dashboard model.
func NewIssueDashboard(m *Monitor) *IssueDashboard {
	return &IssueDashboard{
		monitor:         m,
		keymap:          DefaultIssueKeyMap(),
		styles:          DefaultStyles(),
		mode:            modeIssues,
		filter:          defaultFilterState(),
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
		d.applyFilter()
		d.refreshing = false
		d.lastRefresh = time.Now()
		if d.cursor >= len(d.filteredIssues) {
			d.cursor = len(d.filteredIssues) - 1
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
	case issueContinueTargetsMsg:
		if msg.issueID != d.continue_.issueID {
			return d, nil
		}
		d.continue_.loading = false
		if msg.branchErr != nil {
			d.message = fmt.Sprintf("failed to load branches for %s: %v", msg.issueID, msg.branchErr)
		}
		d.continue_.targets = buildContinueTargets(msg.runs, msg.branches)
		if len(d.continue_.targets) == 0 {
			d.mode = modeIssues
			d.message = fmt.Sprintf("no runs or branches found for %s", msg.issueID)
			return d, nil
		}
		d.continue_.cursor = 0
		d.continue_.offset = 0
		d.ensureContinueTargetVisible()
		d.mode = modeContinueTarget
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
	case modeFilter:
		return d.styles.Box.Render(d.viewFilter())
	case modeContinueTarget:
		return d.styles.Box.Render(d.viewContinueTarget())
	case modeContinueAgent:
		return d.styles.Box.Render(d.viewContinueAgent())
	case modeContinuePrompt:
		return d.styles.Box.Render(d.viewContinuePrompt())
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
	case modeFilter:
		return d.handleFilterKey(msg)
	case modeContinueTarget:
		return d.handleContinueTargetKey(msg)
	case modeContinueAgent:
		return d.handleContinueAgentKey(msg)
	case modeContinuePrompt:
		return d.handleContinuePromptKey(msg)
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
	case d.keymap.Filter:
		d.mode = modeFilter
		return d, nil
	case d.keymap.Sort:
		sortKey := d.monitor.CycleIssueSort()
		d.message = fmt.Sprintf("sort: %s", sortKey)
		d.refreshing = true
		return d, d.refreshCmd()
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
	case d.keymap.ContinueRun:
		if row := d.currentIssue(); row != nil {
			d.continue_ = continueState{issueID: row.ID, loading: true}
			return d, d.loadContinueTargetsCmd(row.ID)
		}
		return d, nil
	case "up", "k":
		if d.cursor > 0 {
			d.cursor--
		}
		d.ensureCursorVisible()
		return d, nil
	case "down", "j":
		if d.cursor < len(d.filteredIssues)-1 {
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

func (d *IssueDashboard) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "f":
		d.mode = modeIssues
		return d, nil
	case "q":
		return d.quit()
	case "up", "k":
		if d.filter.cursor > 0 {
			d.filter.cursor--
		}
		return d, nil
	case "down", "j":
		if d.filter.cursor < 1 { // 2 filter options (0 and 1)
			d.filter.cursor++
		}
		return d, nil
	case "enter", " ":
		// Toggle the selected filter option
		switch d.filter.cursor {
		case 0:
			d.filter.showResolved = !d.filter.showResolved
		case 1:
			d.filter.showClosed = !d.filter.showClosed
		}
		d.applyFilter()
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

func (d *IssueDashboard) viewFilter() string {
	header := d.styles.Title.Render("FILTER ISSUES")
	lines := []string{header, ""}

	lines = append(lines, "Toggle visibility of issues by status:", "")

	// Filter options
	options := []struct {
		label   string
		enabled bool
	}{
		{"Show resolved issues", d.filter.showResolved},
		{"Show closed issues", d.filter.showClosed},
	}

	for i, opt := range options {
		checkbox := "[ ]"
		if opt.enabled {
			checkbox = "[x]"
		}
		label := fmt.Sprintf("  %s %s", checkbox, opt.label)
		if i == d.filter.cursor {
			label = d.styles.Selected.Render(label)
		}
		lines = append(lines, label)
	}

	lines = append(lines, "", "[Enter/Space] toggle  [Esc/f] close")
	return strings.Join(lines, "\n")
}

// applyFilter filters the issues list based on current filter settings
func (d *IssueDashboard) applyFilter() {
	if d.filter.showResolved && d.filter.showClosed {
		// No filtering needed, show all issues
		d.filteredIssues = d.issues
		return
	}

	filtered := make([]IssueRow, 0, len(d.issues))
	for _, issue := range d.issues {
		status := model.ParseIssueStatus(issue.Status)
		if status == model.IssueStatusResolved && !d.filter.showResolved {
			continue
		}
		if status == model.IssueStatusClosed && !d.filter.showClosed {
			continue
		}
		filtered = append(filtered, issue)
	}
	d.filteredIssues = filtered

	// Reset cursor if it's out of bounds
	if d.cursor >= len(d.filteredIssues) {
		d.cursor = len(d.filteredIssues) - 1
		if d.cursor < 0 {
			d.cursor = 0
		}
	}
	d.ensureCursorVisible()
}

// hasActiveFilters returns true if any filters are hiding issues
func (d *IssueDashboard) hasActiveFilters() bool {
	return !d.filter.showResolved || !d.filter.showClosed
}

func (d *IssueDashboard) renderMeta() string {
	sync := d.renderSyncStatus()
	total := fmt.Sprintf("issues: %d", len(d.filteredIssues))
	if d.hasActiveFilters() {
		total = fmt.Sprintf("issues: %d/%d (filtered)", len(d.filteredIssues), len(d.issues))
	}
	sortLabel := fmt.Sprintf("sort: %s", d.monitor.IssueSort())
	nav := d.renderNav()
	rows := d.renderIssueRange()
	return strings.Join([]string{total, sortLabel, sync, nav, rows}, "  ")
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
	if len(d.filteredIssues) == 0 {
		return "rows: 0/0"
	}
	visibleRows := d.issueVisibleRows(d.tableMaxRows())
	if visibleRows == 0 {
		return fmt.Sprintf("rows: 0/%d", len(d.filteredIssues))
	}
	start := d.offset + 1
	if start < 1 {
		start = 1
	}
	end := d.offset + visibleRows
	if end > len(d.filteredIssues) {
		end = len(d.filteredIssues)
	}
	return fmt.Sprintf("rows: %d-%d/%d", start, end, len(d.filteredIssues))
}

func (d *IssueDashboard) renderTable(maxRows int) string {
	if len(d.filteredIssues) == 0 {
		if d.hasActiveFilters() {
			return "No issues found (filters active - press 'f' to adjust)."
		}
		return "No issues found."
	}

	idxW, idW, statusW, latestW, activeW, summaryW := d.tableWidths()

	header := d.renderRow(idxW, idW, statusW, latestW, activeW, summaryW,
		"#", "ID", "STATUS", "LATEST", "ACTIVE", "SUMMARY", true, nil)

	var rows []string
	visibleRows := d.issueVisibleRows(maxRows)
	start := d.offset
	end := len(d.filteredIssues)
	if visibleRows > 0 {
		end = start + visibleRows
		if end > len(d.filteredIssues) {
			end = len(d.filteredIssues)
		}
	} else {
		end = start
	}
	for i, row := range d.filteredIssues[start:end] {
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

	contentWidth := d.safeWidth()
	lines := []string{
		d.styles.Header.Render("DETAILS"),
	}
	lines = append(lines, wrapLabelValue("ID: ", issue.ID, contentWidth)...)
	lines = append(lines, wrapLabelValue("Title: ", title, contentWidth)...)
	lines = append(lines, wrapLabelValue("Status: ", issue.Status, contentWidth)...)
	lines = append(lines, wrapLabelValue("Active runs: ", fmt.Sprintf("%d", issue.ActiveRuns), contentWidth)...)

	latest := "-"
	if issue.LatestRunID != "" {
		latest = fmt.Sprintf("%s (%s)", issue.LatestRunID, issue.LatestStatus)
	}
	if issue.LatestRunID != "" {
		updated := formatRelativeTime(issue.LatestUpdated, time.Now())
		lines = append(lines, wrapLabelValue("Latest run: ", fmt.Sprintf("%s, %s", latest, updated), contentWidth)...)
	} else {
		lines = append(lines, wrapLabelValue("Latest run: ", latest, contentWidth)...)
	}

	summary := issue.Summary
	if summary == "-" {
		summary = ""
	}
	if strings.TrimSpace(summary) != "" {
		lines = append(lines, "Summary:")
		lines = append(lines, wrapText(summary, contentWidth)...)
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
	if d.cursor < 0 || d.cursor >= len(d.filteredIssues) {
		return nil
	}
	return &d.filteredIssues[d.cursor]
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
	if len(d.filteredIssues) < maxRows {
		return len(d.filteredIssues)
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
	if len(d.filteredIssues) == 0 {
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
	maxOffset := len(d.filteredIssues) - visibleRows
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

// Continue dialogue handlers

func (d *IssueDashboard) handleContinueTargetKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		d.mode = modeIssues
		return d, nil
	case "q":
		return d.quit()
	case "enter":
		if target := d.currentContinueTarget(); target != nil {
			// Move to agent selection
			d.continue_.agent = ""
			d.continue_.agentCursor = d.defaultContinueAgentCursor(target)
			d.mode = modeContinueAgent
		}
		return d, nil
	case "up", "k":
		if d.continue_.cursor > 0 {
			d.continue_.cursor--
		}
		d.ensureContinueTargetVisible()
		return d, nil
	case "down", "j":
		if d.continue_.cursor < len(d.continue_.targets)-1 {
			d.continue_.cursor++
		}
		d.ensureContinueTargetVisible()
		return d, nil
	}
	return d, nil
}

func (d *IssueDashboard) handleContinueAgentKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	agents := d.monitor.GetAvailableAgents()
	switch msg.String() {
	case "esc":
		d.mode = modeContinueTarget
		return d, nil
	case "q":
		return d.quit()
	case "enter":
		if len(agents) > 0 {
			idx := d.continue_.agentCursor
			if idx < 0 {
				idx = 0
			}
			if idx >= len(agents) {
				idx = len(agents) - 1
			}
			d.continue_.agent = agents[idx]
			// Move to prompt input
			d.continue_.prompt = ""
			d.mode = modeContinuePrompt
		}
		return d, nil
	case "up", "k":
		if d.continue_.agentCursor > 0 {
			d.continue_.agentCursor--
		}
		return d, nil
	case "down", "j":
		if d.continue_.agentCursor < len(agents)-1 {
			d.continue_.agentCursor++
		}
		return d, nil
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(msg.String()[0] - '1')
		if idx >= 0 && idx < len(agents) {
			d.continue_.agent = agents[idx]
			d.continue_.agentCursor = idx
			d.continue_.prompt = ""
			d.mode = modeContinuePrompt
		}
		return d, nil
	}
	return d, nil
}

func (d *IssueDashboard) handleContinuePromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Reset cursor for agent selection
		d.continue_.agentCursor = 0
		for i, agent := range d.monitor.GetAvailableAgents() {
			if agent == d.continue_.agent {
				d.continue_.agentCursor = i
				break
			}
		}
		d.mode = modeContinueAgent
		return d, nil
	case "enter":
		// Execute continue command
		target := d.currentContinueTarget()
		if target == nil {
			d.mode = modeIssues
			d.message = "no continue target selected"
			return d, nil
		}
		d.mode = modeIssues
		return d, d.continueRunCmd(d.continue_.issueID, *target, d.continue_.agent, d.continue_.prompt)
	}

	switch msg.Type {
	case tea.KeyBackspace, tea.KeyDelete:
		if len(d.continue_.prompt) > 0 {
			runes := []rune(d.continue_.prompt)
			d.continue_.prompt = string(runes[:len(runes)-1])
		}
		return d, nil
	case tea.KeyRunes:
		d.continue_.prompt += string(msg.Runes)
		return d, nil
	default:
		return d, nil
	}
}

func (d *IssueDashboard) viewContinueTarget() string {
	header := d.styles.Title.Render("CONTINUE RUN - SELECT RUN/BRANCH")
	lines := []string{header, ""}

	issueID := d.continue_.issueID
	if strings.TrimSpace(issueID) == "" {
		issueID = "-"
	}
	lines = append(lines, fmt.Sprintf("Issue: %s", issueID), "")

	if d.continue_.loading {
		lines = append(lines, "Loading runs and branches...")
		return strings.Join(lines, "\n")
	}

	if len(d.continue_.targets) == 0 {
		lines = append(lines, "No runs or branches found for this issue.")
		lines = append(lines, "", "[Esc] back")
		return strings.Join(lines, "\n")
	}

	lines = append(lines, "Select a run or branch to continue from:", "")
	maxRows := d.continueTargetMaxRows()
	visibleRows := d.continueTargetVisibleRows(maxRows)
	start := d.continue_.offset
	end := len(d.continue_.targets)
	if visibleRows > 0 {
		end = start + visibleRows
		if end > len(d.continue_.targets) {
			end = len(d.continue_.targets)
		}
	} else {
		end = start
	}
	for i, target := range d.continue_.targets[start:end] {
		label := d.formatContinueTargetLabel(target)
		label = truncate(label, d.safeWidth()-2)
		if i+start == d.continue_.cursor {
			label = d.styles.Selected.Render(label)
		}
		lines = append(lines, label)
	}

	lines = append(lines, "", "[Enter] select  [Esc] cancel")
	return strings.Join(lines, "\n")
}

func (d *IssueDashboard) viewContinueAgent() string {
	header := d.styles.Title.Render("CONTINUE RUN - SELECT AGENT")
	lines := []string{header, ""}

	issueID := d.continue_.issueID
	if strings.TrimSpace(issueID) == "" {
		issueID = "-"
	}
	runRef, branch := d.currentContinueDetails()
	if branch == "" {
		branch = "-"
	}
	lines = append(lines, fmt.Sprintf("Issue: %s", issueID))
	if runRef != "" {
		lines = append(lines, fmt.Sprintf("Run: %s", runRef))
	}
	lines = append(lines, fmt.Sprintf("Branch: %s", branch), "")

	agents := d.monitor.GetAvailableAgents()
	if len(agents) == 0 {
		lines = append(lines, "No agents available.")
		lines = append(lines, "", "[Esc] back")
		return strings.Join(lines, "\n")
	}

	lines = append(lines, "Select an agent:", "")
	for i, agent := range agents {
		label := fmt.Sprintf("  [%d] %s", i+1, agent)
		if i == d.continue_.agentCursor {
			label = d.styles.Selected.Render(label)
		}
		lines = append(lines, label)
	}

	lines = append(lines, "", "[Enter/1-9] select agent  [Esc] back")
	return strings.Join(lines, "\n")
}

func (d *IssueDashboard) viewContinuePrompt() string {
	header := d.styles.Title.Render("CONTINUE RUN - FOLLOW-UP PROMPT")
	lines := []string{header, ""}

	issueID := d.continue_.issueID
	if strings.TrimSpace(issueID) == "" {
		issueID = "-"
	}
	runRef, branch := d.currentContinueDetails()
	if branch == "" {
		branch = "-"
	}
	lines = append(lines, fmt.Sprintf("Issue: %s", issueID))
	if runRef != "" {
		lines = append(lines, fmt.Sprintf("Run: %s", runRef))
	}
	lines = append(lines, fmt.Sprintf("Branch: %s", branch))
	lines = append(lines, fmt.Sprintf("Agent: %s", d.continue_.agent), "")

	lines = append(lines, "Enter an optional follow-up prompt (leave empty to skip):")
	lines = append(lines, fmt.Sprintf("> %s", d.continue_.prompt))

	lines = append(lines, "", "[Enter] start run  [Esc] back")
	return strings.Join(lines, "\n")
}

func (d *IssueDashboard) loadContinueTargetsCmd(issueID string) tea.Cmd {
	return func() tea.Msg {
		runs, err := d.monitor.ListRunsForIssue(issueID)
		if err != nil {
			return errMsg{err: err}
		}
		branches, branchErr := d.monitor.ListBranchesForIssue(issueID)
		if branchErr != nil {
			branches = nil
		}
		return issueContinueTargetsMsg{
			issueID:   issueID,
			runs:      runs,
			branches:  branches,
			branchErr: branchErr,
		}
	}
}

func (d *IssueDashboard) continueRunCmd(issueID string, target continueTarget, agentType, prompt string) tea.Cmd {
	return func() tea.Msg {
		var req ContinueRunRequest
		switch target.kind {
		case continueTargetRun:
			if target.run == nil {
				return errMsg{err: fmt.Errorf("continue target run not found")}
			}
			req.RunRef = target.run.Ref().String()
		case continueTargetBranch:
			req.IssueID = issueID
			req.Branch = target.branch.name
		default:
			return errMsg{err: fmt.Errorf("unknown continue target")}
		}

		output, err := d.monitor.ContinueRun(req, agentType, prompt)
		if err != nil {
			return errMsg{err: fmt.Errorf("%s", output)}
		}
		return infoMsg{text: output}
	}
}

func (d *IssueDashboard) currentContinueTarget() *continueTarget {
	if d.continue_.cursor < 0 || d.continue_.cursor >= len(d.continue_.targets) {
		return nil
	}
	return &d.continue_.targets[d.continue_.cursor]
}

func (d *IssueDashboard) currentContinueDetails() (string, string) {
	target := d.currentContinueTarget()
	if target == nil {
		return "", ""
	}
	if target.kind == continueTargetRun && target.run != nil {
		return target.run.Ref().String(), target.run.Branch
	}
	return "", target.branch.name
}

func (d *IssueDashboard) defaultContinueAgentCursor(target *continueTarget) int {
	agents := d.monitor.GetAvailableAgents()
	if target == nil || len(agents) == 0 {
		return 0
	}
	if target.kind == continueTargetRun && target.run != nil && target.run.Agent != "" {
		for i, agent := range agents {
			if agent == target.run.Agent {
				return i
			}
		}
	}
	return 0
}

func (d *IssueDashboard) formatContinueTargetLabel(target continueTarget) string {
	switch target.kind {
	case continueTargetRun:
		if target.run == nil {
			return "  run -"
		}
		branch := target.run.Branch
		if branch == "" {
			branch = "-"
		}
		agent := target.run.Agent
		if agent == "" {
			agent = "-"
		}
		updated := formatRelativeTime(target.run.UpdatedAt, time.Now())
		return fmt.Sprintf("  run %s#%s  %s  %s  %s  %s", target.run.IssueID, target.run.RunID, target.run.Status, agent, branch, updated)
	case continueTargetBranch:
		timeStr := formatRelativeTime(target.branch.commitTime, time.Now())
		return fmt.Sprintf("  branch %s  (%s)", target.branch.name, timeStr)
	default:
		return "  -"
	}
}

func (d *IssueDashboard) continueTargetMaxRows() int {
	base := 8
	available := d.safeHeight() - base
	if available < 1 {
		return 1
	}
	return available
}

func (d *IssueDashboard) continueTargetVisibleRows(maxRows int) int {
	if maxRows <= 0 {
		return 0
	}
	if len(d.continue_.targets) < maxRows {
		return len(d.continue_.targets)
	}
	return maxRows
}

func (d *IssueDashboard) ensureContinueTargetVisible() {
	if len(d.continue_.targets) == 0 {
		d.continue_.offset = 0
		return
	}
	visibleRows := d.continueTargetVisibleRows(d.continueTargetMaxRows())
	if visibleRows <= 0 {
		d.continue_.offset = 0
		return
	}
	if d.continue_.cursor < d.continue_.offset {
		d.continue_.offset = d.continue_.cursor
	}
	if d.continue_.cursor >= d.continue_.offset+visibleRows {
		d.continue_.offset = d.continue_.cursor - visibleRows + 1
	}
	maxOffset := len(d.continue_.targets) - visibleRows
	if maxOffset < 0 {
		maxOffset = 0
	}
	if d.continue_.offset > maxOffset {
		d.continue_.offset = maxOffset
	}
	if d.continue_.offset < 0 {
		d.continue_.offset = 0
	}
}

func buildContinueTargets(runs []*model.Run, branches []branchInfo) []continueTarget {
	targets := make([]continueTarget, 0, len(runs)+len(branches))
	for _, run := range runs {
		if run == nil {
			continue
		}
		targets = append(targets, continueTarget{
			kind: continueTargetRun,
			run:  run,
		})
	}

	for _, branch := range branches {
		targets = append(targets, continueTarget{
			kind:   continueTargetBranch,
			branch: branch,
		})
	}

	return targets
}

// filterBranchesForIssue filters branches that contain the issue ID in their name
func filterBranchesForIssue(branches map[string]time.Time, issueID string) []branchInfo {
	var result []branchInfo
	issueIDLower := strings.ToLower(issueID)
	for name, commitTime := range branches {
		nameLower := strings.ToLower(name)
		if strings.Contains(nameLower, issueIDLower) {
			result = append(result, branchInfo{
				name:       name,
				commitTime: commitTime,
			})
		}
	}
	// Sort by commit time descending (most recent first)
	sort.Slice(result, func(i, j int) bool {
		return result[i].commitTime.After(result[j].commitTime)
	})
	return result
}
