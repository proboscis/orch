package monitor

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/tmux"
)

type dashboardMode int

const (
	modeDashboard dashboardMode = iota
	modeStopSelectRun
	modeNewSelectIssue
	modeDashboardFilter
	modeHelp
)

type stopState struct {
	runs []RunRow
}

type newRunState struct {
	issues  []*model.Issue
	cursor  int
	loading bool
}

type captureState struct {
	runRef  string
	content string
	message string
	loading bool
}

type issueState struct {
	issueID string
	content string
	message string
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
	capture captureState
	issue   issueState

	stop   stopState
	newRun newRunState
	filter runFilterState

	keymap KeyMap
	styles Styles

	lastRefresh     time.Time
	refreshing      bool
	refreshInterval time.Duration
	filterPreset    int
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

type captureMsg struct {
	runRef  string
	content string
	err     error
}

type issueContentMsg struct {
	issueID string
	content string
	err     error
}

type execFinishedMsg struct {
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
		filterPreset:    -1,
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
		return d, d.startRunPanels()
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
	case captureMsg:
		if msg.runRef != d.capture.runRef {
			return d, nil
		}
		d.capture.loading = false
		if msg.err != nil {
			d.capture.content = ""
			d.capture.message = "No capture available."
			return d, nil
		}
		trimmed := strings.TrimRight(msg.content, "\n")
		if strings.TrimSpace(trimmed) == "" {
			d.capture.content = ""
			d.capture.message = "No capture available."
			return d, nil
		}
		d.capture.content = trimmed
		d.capture.message = ""
		return d, nil
	case issueContentMsg:
		if msg.issueID != d.issue.issueID {
			return d, nil
		}
		d.issue.loading = false
		if msg.err != nil {
			d.issue.content = ""
			d.issue.message = "No issue content available."
			return d, nil
		}
		trimmed := strings.TrimRight(msg.content, "\n")
		if strings.TrimSpace(trimmed) == "" {
			d.issue.content = ""
			d.issue.message = "No issue content available."
			return d, nil
		}
		d.issue.content = trimmed
		d.issue.message = ""
		return d, nil
	case execFinishedMsg:
		if msg.err != nil {
			d.message = msg.err.Error()
		}
		d.refreshing = true
		return d, d.refreshCmd()
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
	case modeDashboardFilter:
		return d.styles.Box.Render(d.viewFilter())
	case modeHelp:
		return d.styles.Box.Render(d.viewHelp())
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
	case modeDashboardFilter:
		return d.handleFilterKey(msg)
	case modeHelp:
		return d.handleHelpKey(msg)
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
	case d.keymap.Filter, "/":
		return d.enterFilterMode()
	case d.keymap.QuickFilter:
		return d.applyQuickFilterPreset()
	case d.keymap.Sort:
		sortKey := d.monitor.CycleRunSort()
		d.message = fmt.Sprintf("sort: %s", sortKey)
		d.refreshing = true
		return d, d.refreshCmd()
	case d.keymap.Resolve:
		if d.cursor >= 0 && d.cursor < len(d.runs) {
			run := d.runs[d.cursor].Run
			return d, d.resolveRunCmd(run)
		}
		return d, nil
	case d.keymap.Merge:
		if d.cursor >= 0 && d.cursor < len(d.runs) {
			run := d.runs[d.cursor].Run
			return d, d.requestMergeCmd(run)
		}
		return d, nil
	case "up", "k":
		if d.cursor > 0 {
			d.cursor--
			d.ensureCursorVisible()
			return d, d.startRunPanels()
		}
		return d, nil
	case "down", "j":
		if d.cursor < len(d.runs)-1 {
			d.cursor++
			d.ensureCursorVisible()
			return d, d.startRunPanels()
		}
		return d, nil
	case "enter":
		if d.cursor >= 0 && d.cursor < len(d.runs) {
			run := d.runs[d.cursor].Run
			if err := d.monitor.OpenRun(run); err != nil {
				d.message = err.Error()
			}
		}
		return d, nil
	case d.keymap.Exec:
		if d.cursor >= 0 && d.cursor < len(d.runs) {
			run := d.runs[d.cursor].Run
			if run != nil {
				return d, d.execShellCmd(run)
			}
		}
		d.message = "no run selected"
		return d, nil
	case d.keymap.EditIssue:
		if d.cursor >= 0 && d.cursor < len(d.runs) {
			run := d.runs[d.cursor].Run
			if run != nil {
				return d, d.openIssueInNvimCmd(run.IssueID)
			}
		}
		d.message = "no run selected"
		return d, nil
	case "?":
		d.mode = modeHelp
		return d, nil
	}

	return d, nil
}

func (d *Dashboard) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Any key dismisses the help popup
	d.mode = modeDashboard
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

func (d *Dashboard) startRunPanels() tea.Cmd {
	captureCmd := d.startCapture()
	issueCmd := d.startIssueContent()
	if captureCmd == nil {
		return issueCmd
	}
	if issueCmd == nil {
		return captureCmd
	}
	return tea.Batch(captureCmd, issueCmd)
}

func (d *Dashboard) startCapture() tea.Cmd {
	run := d.selectedRun()
	if run == nil {
		d.capture = captureState{message: "No capture available."}
		return nil
	}
	runRef := run.Ref().String()
	if d.capture.runRef != runRef {
		d.capture.content = ""
	}
	d.capture.runRef = runRef
	d.capture.message = ""
	d.capture.loading = true
	return d.captureCmd(run, runRef)
}

func (d *Dashboard) startIssueContent() tea.Cmd {
	run := d.selectedRun()
	if run == nil || strings.TrimSpace(run.IssueID) == "" {
		d.issue = issueState{message: "No issue content available."}
		return nil
	}
	issueID := run.IssueID
	if d.issue.issueID != issueID {
		d.issue.content = ""
	}
	d.issue.issueID = issueID
	d.issue.message = ""
	d.issue.loading = true
	return d.issueContentCmd(issueID)
}

func (d *Dashboard) captureCmd(run *model.Run, runRef string) tea.Cmd {
	lines := defaultCaptureLines
	return func() tea.Msg {
		content, err := d.monitor.CaptureRun(run, lines)
		return captureMsg{
			runRef:  runRef,
			content: content,
			err:     err,
		}
	}
}

func (d *Dashboard) issueContentCmd(issueID string) tea.Cmd {
	return func() tea.Msg {
		content, err := d.monitor.IssueContent(issueID)
		return issueContentMsg{
			issueID: issueID,
			content: content,
			err:     err,
		}
	}
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
		// Use empty agent type to use default agent
		output, err := d.monitor.StartRun(issueID, "")
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

func (d *Dashboard) execShellCmd(run *model.Run) tea.Cmd {
	if run == nil {
		return func() tea.Msg {
			return errMsg{err: fmt.Errorf("run not found")}
		}
	}

	// Resolve worktree path
	worktreePath := run.WorktreePath
	if worktreePath == "" {
		return func() tea.Msg {
			return errMsg{err: fmt.Errorf("run has no worktree")}
		}
	}
	if !filepath.IsAbs(worktreePath) {
		repoRoot, err := git.FindMainRepoRoot("")
		if err != nil {
			return func() tea.Msg {
				return errMsg{err: fmt.Errorf("could not find git repository: %w", err)}
			}
		}
		worktreePath = filepath.Join(repoRoot, worktreePath)
	}

	windowName := fmt.Sprintf("exec-%s", run.ShortID())
	env := []string{
		fmt.Sprintf("ORCH_ISSUE_ID=%s", shellQuote(run.IssueID)),
		fmt.Sprintf("ORCH_RUN_ID=%s", shellQuote(run.RunID)),
		fmt.Sprintf("ORCH_RUN_PATH=%s", shellQuote(run.Path)),
		fmt.Sprintf("ORCH_WORKTREE_PATH=%s", shellQuote(worktreePath)),
		fmt.Sprintf("ORCH_BRANCH=%s", shellQuote(run.Branch)),
	}
	shellCmd := strings.Join(env, " ") + " exec zsh"

	return func() tea.Msg {
		if err := tmux.NewWindow(d.monitor.session, windowName, worktreePath, shellCmd); err != nil {
			return execFinishedMsg{err: fmt.Errorf("failed to open exec window: %w", err)}
		}
		return execFinishedMsg{err: nil}
	}
}

func (d *Dashboard) requestMergeCmd(run *model.Run) tea.Cmd {
	return func() tea.Msg {
		if run == nil {
			return errMsg{err: fmt.Errorf("run not found")}
		}
		output, err := d.monitor.RequestMerge(run)
		if err != nil {
			if strings.TrimSpace(output) == "" {
				return errMsg{err: err}
			}
			return errMsg{err: fmt.Errorf("%s", output)}
		}
		if strings.TrimSpace(output) == "" {
			output = fmt.Sprintf("merge requested for %s#%s", run.IssueID, run.RunID)
		}
		return infoMsg{text: output}
	}
}

// openIssueInNvimCmd opens the issue file in nvim, suspending the TUI.
// When the editor closes, the TUI resumes and the run list is refreshed.
func (d *Dashboard) openIssueInNvimCmd(issueID string) tea.Cmd {
	issue, err := d.monitor.store.ResolveIssue(issueID)
	if err != nil || issue == nil {
		return func() tea.Msg {
			return errMsg{err: fmt.Errorf("could not resolve issue %s: %v", issueID, err)}
		}
	}
	if strings.TrimSpace(issue.Path) == "" {
		return func() tea.Msg {
			return errMsg{err: fmt.Errorf("issue %s has no file path", issueID)}
		}
	}

	c := exec.Command("nvim", issue.Path)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return errMsg{err: fmt.Errorf("editor error: %v", err)}
		}
		return infoMsg{text: fmt.Sprintf("closed issue %s", issueID)}
	})
}

func (d *Dashboard) viewDashboard() string {
	title := d.styles.Title.Render("ORCH MONITOR")
	meta := d.renderMeta()
	table := d.renderTable(d.tableMaxRows())
	stats := d.renderStats()
	details := d.renderDetails(d.detailsPaneHeight())
	context := d.renderContext(d.capturePaneHeight())
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
	if details != "" {
		lines = append(lines, "", details)
	}
	if context != "" {
		lines = append(lines, "", context)
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

func (d *Dashboard) viewHelp() string {
	lines := []string{
		d.styles.Title.Render("HELP - KEYBOARD SHORTCUTS"),
		"",
		d.styles.Header.Render("Navigation"),
		"  g          Switch to Runs dashboard",
		"  i          Switch to Issues dashboard",
		"  c          Switch to Chat pane",
		"  up / k     Move cursor up",
		"  down / j   Move cursor down",
		"",
		d.styles.Header.Render("Run Actions"),
		"  enter      Open selected run",
		"  I          Open issue file in nvim",
		"  e          Execute shell in run's worktree",
		"  s          Stop run (select from active runs)",
		"  n          New run (select issue to start)",
		"  R          Resolve run and mark issue as resolved",
		"  M          Request merge for run",
		"",
		d.styles.Header.Render("Filtering & Sorting"),
		"  f  or  /   Enter filter mode",
		"  F          Cycle quick filter presets",
		"  S          Cycle sort order",
		"",
		d.styles.Header.Render("Other"),
		"  r          Refresh data",
		"  q          Quit monitor",
		"  ?          Show this help",
		"",
		d.styles.Faint.Render("Press any key to close this help"),
	}
	return strings.Join(lines, "\n")
}

func (d *Dashboard) selectedRun() *model.Run {
	if d.cursor >= 0 && d.cursor < len(d.runs) {
		return d.runs[d.cursor].Run
	}
	return nil
}

func (d *Dashboard) selectedRunRow() *RunRow {
	if d.cursor >= 0 && d.cursor < len(d.runs) {
		return &d.runs[d.cursor]
	}
	return nil
}

func (d *Dashboard) renderTable(maxRows int) string {
	if len(d.runs) == 0 {
		if !d.monitor.RunFilter().IsDefault() {
			return "No runs found (filters active - press 'f' to adjust)."
		}
		return "No runs found."
	}
	idxW, idW, issueW, issueStatusW, agentW, statusW, aliveW, branchW, worktreeW, prW, mergedW, updatedW, topicW := d.tableWidths()

	header := d.renderRow(idxW, idW, issueW, issueStatusW, agentW, statusW, aliveW, branchW, worktreeW, prW, mergedW, updatedW, topicW,
		"#", "ID", "ISSUE", "ISSUE-ST", "AGENT", "STATUS", "ALIVE", "BRANCH", "WORKTREE", "PR", "MERGED", "UPDATED", "TOPIC", true, nil)

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
		r := d.renderRow(idxW, idW, issueW, issueStatusW, agentW, statusW, aliveW, branchW, worktreeW, prW, mergedW, updatedW, topicW,
			fmt.Sprintf("%d", row.Index),
			row.ShortID,
			row.IssueID,
			row.IssueStatus,
			row.Agent,
			string(row.Status),
			row.Alive,
			row.Branch,
			row.Worktree,
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

func (d *Dashboard) renderRow(idxW, idW, issueW, issueStatusW, agentW, statusW, aliveW, branchW, worktreeW, prW, mergedW, updatedW, topicW int, idx, id, issue, issueStatus, agent, status, alive, branch, worktree, pr, merged, updated, topic string, header bool, row *RunRow) string {
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
	aliveCol := d.pad(alive, aliveW, baseStyle)
	branchCol := d.pad(branch, branchW, baseStyle)
	worktreeCol := d.pad(worktree, worktreeW, baseStyle)
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
		aliveCol = d.pad(alive, aliveW, headerStyle)
		branchCol = d.pad(branch, branchW, headerStyle)
		worktreeCol = d.pad(worktree, worktreeW, headerStyle)
		prCol = d.pad(pr, prW, headerStyle)
		mergedCol = d.pad(merged, mergedW, headerStyle)
	}

	if row != nil {
		if style, ok := d.styles.Status[row.Status]; ok {
			statusCol = d.pad(status, statusW, style)
		}
		if style, ok := d.styles.Alive[row.Alive]; ok {
			aliveCol = d.pad(alive, aliveW, style)
		}
		// Apply PR state styling
		if row.PRState != "" {
			if style, ok := d.styles.PRState[row.PRState]; ok {
				prCol = d.pad(pr, prW, style)
			}
		}
	}

	return strings.Join([]string{idxCol, idCol, issueCol, issueStatusCol, agentCol, statusCol, aliveCol, branchCol, worktreeCol, prCol, mergedCol, updatedCol, topicCol}, "  ")
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

func (d *Dashboard) renderDetails(maxLines int) string {
	if maxLines <= 0 {
		return ""
	}
	row := d.selectedRunRow()
	if row == nil || row.Run == nil {
		return "No run selected."
	}
	run := row.Run

	branch := strings.TrimSpace(run.Branch)
	if branch == "" {
		branch = "-"
	}
	worktree := strings.TrimSpace(run.WorktreePath)
	if worktree == "" {
		worktree = "-"
	}
	summary := strings.TrimSpace(row.IssueSummary)
	if summary == "" {
		summary = "-"
	}

	contentWidth := d.safeWidth()
	lines := []string{
		d.styles.Header.Render("DETAILS"),
	}
	lines = append(lines, wrapLabelValue("Run: ", run.Ref().String(), contentWidth)...)
	lines = append(lines, wrapLabelValue("Issue: ", run.IssueID, contentWidth)...)
	lines = append(lines, wrapLabelValue("Summary: ", summary, contentWidth)...)
	lines = append(lines, wrapLabelValue("Branch: ", branch, contentWidth)...)
	lines = append(lines, wrapLabelValue("Worktree: ", worktree, contentWidth)...)

	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
		if maxLines > 1 {
			lines[maxLines-1] = "..."
		}
	}
	return strings.Join(lines, "\n")
}

func (d *Dashboard) renderContext(height int) string {
	if height <= 0 {
		return ""
	}
	width := d.safeWidth()
	issueLabel := "ISSUE"
	captureLabel := "CAPTURE"
	if run := d.selectedRun(); run != nil {
		if strings.TrimSpace(run.IssueID) != "" {
			issueLabel = fmt.Sprintf("ISSUE %s", run.IssueID)
		}
		captureLabel = fmt.Sprintf("CAPTURE %s", run.Ref().String())
	}
	issueHeader := d.styles.Header.Render(truncate(issueLabel, width))
	captureHeader := d.styles.Header.Render(truncate(captureLabel, width))
	if height == 1 {
		return issueHeader
	}
	if height == 2 {
		return strings.Join([]string{issueHeader, captureHeader}, "\n")
	}

	contentHeight := height - 2
	issueLines := d.issueLines(width)
	captureLines := d.captureLines(width)
	issueContentLines := contentHeight / 2
	if issueContentLines < 1 {
		issueContentLines = 1
	}
	captureContentLines := contentHeight - issueContentLines
	if captureContentLines < 1 {
		captureContentLines = 1
		issueContentLines = contentHeight - captureContentLines
	}

	if len(issueLines) > issueContentLines && issueContentLines > 0 {
		issueLines = issueLines[:issueContentLines]
		if issueContentLines > 1 {
			issueLines[issueContentLines-1] = "..."
		}
	}
	if len(captureLines) > captureContentLines && captureContentLines > 0 {
		captureLines = captureLines[len(captureLines)-captureContentLines:]
	}

	lines := []string{issueHeader}
	if issueContentLines > 0 {
		lines = append(lines, issueLines...)
	}
	lines = append(lines, captureHeader)
	if captureContentLines > 0 {
		lines = append(lines, captureLines...)
	}
	return strings.Join(lines, "\n")
}

func (d *Dashboard) captureLines(width int) []string {
	if d.capture.loading && d.capture.content == "" {
		return []string{d.styles.Faint.Render("Loading capture...")}
	}
	if d.capture.message != "" {
		return []string{d.styles.Faint.Render(d.capture.message)}
	}
	content := d.capture.content
	if strings.TrimSpace(content) == "" {
		return []string{d.styles.Faint.Render("No capture available.")}
	}
	return wrapText(content, width)
}

// renderCapture renders the capture pane for testing.
func (d *Dashboard) renderCapture(height int) string {
	if height <= 0 {
		return ""
	}
	width := d.safeWidth()
	captureLabel := "CAPTURE"
	if run := d.selectedRun(); run != nil {
		captureLabel = fmt.Sprintf("CAPTURE %s", run.Ref().String())
	}
	captureHeader := d.styles.Header.Render(truncate(captureLabel, width))
	if height == 1 {
		return captureHeader
	}

	contentHeight := height - 1
	captureContent := d.captureLines(width)
	if len(captureContent) > contentHeight && contentHeight > 0 {
		captureContent = captureContent[len(captureContent)-contentHeight:]
	}

	lines := []string{captureHeader}
	lines = append(lines, captureContent...)
	return strings.Join(lines, "\n")
}

func (d *Dashboard) issueLines(width int) []string {
	if d.issue.loading && d.issue.content == "" {
		return []string{d.styles.Faint.Render("Loading issue...")}
	}
	if d.issue.message != "" {
		return []string{d.styles.Faint.Render(d.issue.message)}
	}
	content := d.issue.content
	if strings.TrimSpace(content) == "" {
		return []string{d.styles.Faint.Render("No issue content available.")}
	}
	return wrapText(content, width)
}

func (d *Dashboard) renderMeta() string {
	filter := d.monitor.RunFilter().Summary()
	sortLabel := fmt.Sprintf("sort: %s", d.monitor.RunSort())
	sync := d.renderSyncStatus()
	nav := d.renderNav()
	rows := d.renderRunRange()
	return strings.Join([]string{filter, sortLabel, sync, nav, rows}, "  ")
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

func (d *Dashboard) tableWidths() (idxW, idW, issueW, issueStatusW, agentW, statusW, aliveW, branchW, worktreeW, prW, mergedW, updatedW, topicW int) {
	idxW = 2
	idW = 6
	issueW = 14
	issueStatusW = 8
	agentW = agent.MaxAgentDisplayWidth
	statusW = 10
	aliveW = 5
	branchW = runTableBranchWidth
	worktreeW = runTableWorktreeWidth
	prW = 6 // Increased to fit PR numbers like "#1234"
	mergedW = 8
	updatedW = 7
	contentWidth := d.safeWidth()
	columnCount := 13
	fixed := idxW + idW + issueW + issueStatusW + agentW + statusW + aliveW + branchW + worktreeW + prW + mergedW + updatedW + (columnCount-1)*2
	topicW = contentWidth - fixed
	if topicW < 6 {
		topicW = 6
	}
	return
}

func (d *Dashboard) safeWidth() int {
	frame := d.styles.Box.GetHorizontalFrameSize()
	if d.width > frame {
		return d.width - frame
	}
	return 80
}

func (d *Dashboard) safeHeight() int {
	frame := d.styles.Box.GetVerticalFrameSize()
	if d.height > frame {
		return d.height - frame
	}
	return 24
}

func (d *Dashboard) baseHeight() int {
	base := 8
	if d.message != "" {
		base += 2
	}
	return base
}

func (d *Dashboard) detailsPaneHeight() int {
	return runDetailsMaxLines
}

func (d *Dashboard) capturePaneHeight() int {
	available := d.safeHeight() - d.baseHeight()
	if details := d.detailsPaneHeight(); details > 0 {
		available -= details + 1
	}
	if available <= 1 {
		return 0
	}
	desired := available / 3
	if desired < 4 {
		desired = 4
	}
	if desired > 10 {
		desired = 10
	}
	maxCapture := available - 1
	if maxCapture < 0 {
		return 0
	}
	if desired > maxCapture {
		desired = maxCapture
	}
	if desired < 0 {
		return 0
	}
	return desired
}

func (d *Dashboard) tableMaxRows() int {
	available := d.safeHeight() - d.baseHeight() - d.capturePaneHeight()
	if details := d.detailsPaneHeight(); details > 0 {
		available -= details + 1
	}
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
	if width <= 3 {
		return truncateToWidth(s, width)
	}
	return truncateToWidth(s, width-3) + "..."
}

func truncateToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= width {
		return s
	}
	var b strings.Builder
	current := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if current+rw > width {
			break
		}
		b.WriteRune(r)
		current += rw
	}
	return b.String()
}

func wrapText(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	var lines []string
	for _, raw := range strings.Split(s, "\n") {
		if raw == "" {
			lines = append(lines, "")
			continue
		}
		runes := []rune(raw)
		start := 0
		for start < len(runes) {
			if runewidth.StringWidth(string(runes[start:])) <= width {
				lines = append(lines, string(runes[start:]))
				break
			}
			curWidth := 0
			lastSpace := -1
			end := start
			for ; end < len(runes); end++ {
				rw := runewidth.RuneWidth(runes[end])
				if curWidth+rw > width {
					break
				}
				curWidth += rw
				if unicode.IsSpace(runes[end]) {
					lastSpace = end
				}
			}
			split := end
			if lastSpace >= start {
				split = lastSpace
			}
			if split == start {
				split = end
				if split == start {
					split = start + 1
				}
			}
			line := strings.TrimRightFunc(string(runes[start:split]), unicode.IsSpace)
			lines = append(lines, line)
			start = split
			for start < len(runes) && unicode.IsSpace(runes[start]) {
				start++
			}
		}
	}
	return lines
}

func wrapLabelValue(label, value string, width int) []string {
	if width <= 0 {
		return []string{label + value}
	}
	labelWidth := runewidth.StringWidth(label)
	if labelWidth >= width {
		return wrapText(label+value, width)
	}
	valueLines := wrapText(value, width-labelWidth)
	if len(valueLines) == 0 {
		return []string{label}
	}
	lines := make([]string, 0, len(valueLines))
	lines = append(lines, label+valueLines[0])
	indent := strings.Repeat(" ", labelWidth)
	for _, line := range valueLines[1:] {
		lines = append(lines, indent+line)
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
