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

// RunRow holds display data for a run.
type RunRow struct {
	Index   int
	ShortID string
	IssueID string
	Status  model.Status
	Summary string
	Updated time.Time
	Run     *model.Run
}

type dashboardMode int

const (
	modeDashboard dashboardMode = iota
	modeAnswerSelectRun
	modeAnswerSelectQuestion
	modeAnswerInput
	modeStopSelectRun
	modeNewSelectIssue
)

type answerRun struct {
	windowIndex int
	run         *model.Run
	questions   []*model.Event
}

type answerState struct {
	runs             []answerRun
	selectedRun      int
	selectedQuestion int
	input            string
}

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
	width  int
	height int

	mode    dashboardMode
	message string

	answer answerState
	stop   stopState
	newRun newRunState

	keymap KeyMap
	styles Styles
}

type refreshMsg struct {
	rows []RunRow
}

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
		monitor: m,
		keymap:  DefaultKeyMap(),
		styles:  DefaultStyles(),
		mode:    modeDashboard,
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
	return d.refreshCmd()
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
		if d.cursor >= len(d.runs) {
			d.cursor = len(d.runs) - 1
			if d.cursor < 0 {
				d.cursor = 0
			}
		}
		return d, nil
	case issuesMsg:
		d.newRun.issues = msg.issues
		d.newRun.cursor = 0
		d.newRun.loading = false
		return d, nil
	case infoMsg:
		d.message = msg.text
		return d, d.refreshCmd()
	case errMsg:
		d.message = msg.err.Error()
		return d, nil
	case tea.KeyMsg:
		return d.handleKey(msg)
	default:
		return d, nil
	}
}

// View implements tea.Model.
func (d *Dashboard) View() string {
	switch d.mode {
	case modeAnswerSelectRun:
		return d.styles.Box.Render(d.viewAnswerRuns())
	case modeAnswerSelectQuestion:
		return d.styles.Box.Render(d.viewAnswerQuestions())
	case modeAnswerInput:
		return d.styles.Box.Render(d.viewAnswerInput())
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
	case modeAnswerSelectRun:
		return d.handleAnswerRunKey(msg)
	case modeAnswerSelectQuestion:
		return d.handleAnswerQuestionKey(msg)
	case modeAnswerInput:
		return d.handleAnswerInputKey(msg)
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
	case "r":
		return d, d.refreshCmd()
	case "a":
		return d.enterAnswerMode()
	case "s":
		return d.enterStopMode()
	case "n":
		return d.enterNewRunMode()
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
		if d.cursor >= 0 && d.cursor < len(d.runs) {
			index := d.runs[d.cursor].Index
			if err := d.monitor.SwitchWindow(index); err != nil {
				d.message = err.Error()
			}
		}
		return d, nil
	case "?":
		d.message = d.keymap.HelpLine()
		return d, nil
	}

	if index, ok := parseNumberKey(msg); ok {
		if err := d.monitor.SwitchWindow(index); err != nil {
			d.message = err.Error()
		}
	}
	return d, nil
}

func (d *Dashboard) handleAnswerRunKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		d.mode = modeDashboard
		return d, nil
	case "q":
		return d.quit()
	}

	if index, ok := parseNumberKey(msg); ok {
		if selectionIndex := d.answerRunIndexByWindowIndex(index); selectionIndex >= 0 {
			d.answer.selectedRun = selectionIndex
			d.answer.selectedQuestion = 0
			d.answer.input = ""
			if len(d.answer.runs[selectionIndex].questions) > 1 {
				d.mode = modeAnswerSelectQuestion
			} else {
				d.mode = modeAnswerInput
			}
		} else {
			d.message = "run not in answer list"
		}
	}
	return d, nil
}

func (d *Dashboard) handleAnswerQuestionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		d.mode = modeAnswerSelectRun
		return d, nil
	case "q":
		return d.quit()
	}

	if index, ok := parseNumberKey(msg); ok {
		selection := d.currentAnswerRun()
		if selection == nil {
			d.mode = modeDashboard
			return d, nil
		}
		if index < 1 || index > len(selection.questions) {
			d.message = "question not in list"
			return d, nil
		}
		d.answer.selectedQuestion = index - 1
		d.answer.input = ""
		d.mode = modeAnswerInput
	}
	return d, nil
}

func (d *Dashboard) handleAnswerInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		d.mode = modeAnswerSelectQuestion
		return d, nil
	case "enter":
		selection := d.currentAnswerRun()
		if selection == nil {
			d.mode = modeDashboard
			return d, nil
		}
		if d.answer.selectedQuestion < 0 || d.answer.selectedQuestion >= len(selection.questions) {
			d.message = "question not selected"
			return d, nil
		}
		if strings.TrimSpace(d.answer.input) == "" {
			d.message = "answer text is required"
			return d, nil
		}
		question := selection.questions[d.answer.selectedQuestion]
		cmd := d.answerCmd(selection.run, question.Name, d.answer.input)
		d.mode = modeDashboard
		d.answer.input = ""
		return d, cmd
	}

	switch msg.Type {
	case tea.KeyBackspace, tea.KeyDelete:
		if len(d.answer.input) > 0 {
			runes := []rune(d.answer.input)
			d.answer.input = string(runes[:len(runes)-1])
		}
		return d, nil
	case tea.KeyRunes:
		d.answer.input += string(msg.Runes)
		return d, nil
	default:
		return d, nil
	}
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

func (d *Dashboard) enterAnswerMode() (tea.Model, tea.Cmd) {
	var runs []answerRun
	for _, row := range d.runs {
		if row.Run == nil {
			continue
		}
		if row.Status != model.StatusBlocked && row.Status != model.StatusBlockedAPI {
			continue
		}
		questions := row.Run.UnansweredQuestions()
		if len(questions) == 0 {
			continue
		}
		runs = append(runs, answerRun{
			windowIndex: row.Index,
			run:         row.Run,
			questions:   questions,
		})
	}

	if len(runs) == 0 {
		d.message = "no blocked runs with unanswered questions"
		return d, nil
	}

	d.answer = answerState{runs: runs}
	d.mode = modeAnswerSelectRun
	return d, nil
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

func (d *Dashboard) answerCmd(run *model.Run, questionID, text string) tea.Cmd {
	return func() tea.Msg {
		if run == nil {
			return errMsg{err: fmt.Errorf("run not found")}
		}
		if err := d.monitor.AnswerQuestion(run, questionID, text); err != nil {
			return errMsg{err: err}
		}
		return infoMsg{text: fmt.Sprintf("answered %s for %s#%s", questionID, run.IssueID, run.RunID)}
	}
}

func (d *Dashboard) viewDashboard() string {
	title := d.styles.Title.Render("ORCH MONITOR")
	table := d.renderTable()
	stats := d.renderStats()
	footer := d.renderFooter()
	message := ""
	if d.message != "" {
		message = d.styles.Faint.Render(d.message)
	}

	lines := []string{
		title,
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

func (d *Dashboard) viewAnswerRuns() string {
	lines := []string{
		d.styles.Title.Render("ANSWER MODE"),
		"",
		"Blocked runs with unanswered questions:",
		"",
	}
	for _, run := range d.answer.runs {
		line := fmt.Sprintf("  [%d] %s - %d questions", run.windowIndex, run.run.IssueID, len(run.questions))
		lines = append(lines, line)
		for i, q := range run.questions {
			label := fmt.Sprintf("      Q%d: %s", i+1, q.Attrs["text"])
			lines = append(lines, truncate(label, d.safeWidth()-4))
		}
		lines = append(lines, "")
	}
	lines = append(lines, "Select run [1-9], or [Esc] to cancel.")
	return strings.Join(lines, "\n")
}

func (d *Dashboard) viewAnswerQuestions() string {
	current := d.currentAnswerRun()
	if current == nil {
		return "No blocked runs found."
	}
	lines := []string{
		d.styles.Title.Render(fmt.Sprintf("ANSWER: %s", current.run.IssueID)),
		"",
		"Select question:",
		"",
	}
	for i, q := range current.questions {
		label := fmt.Sprintf("  [%d] %s", i+1, q.Attrs["text"])
		lines = append(lines, truncate(label, d.safeWidth()-2))
	}
	lines = append(lines, "", "Select question [1-9], or [Esc] to cancel.")
	return strings.Join(lines, "\n")
}

func (d *Dashboard) viewAnswerInput() string {
	current := d.currentAnswerRun()
	if current == nil {
		return "No blocked runs found."
	}
	q := current.questions[d.answer.selectedQuestion]
	lines := []string{
		d.styles.Title.Render(fmt.Sprintf("ANSWER: %s", current.run.IssueID)),
		"",
		"Question:",
	}
	lines = append(lines, wrapText(q.Attrs["text"], d.safeWidth()-2)...)
	lines = append(lines, "", "Your answer:", fmt.Sprintf("> %s", d.answer.input))
	lines = append(lines, "", "[Enter] submit  [Esc] back")
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

func (d *Dashboard) renderTable() string {
	if len(d.runs) == 0 {
		return "No runs found."
	}

	idxW, idW, issueW, statusW, agoW, summaryW := d.tableWidths()

	header := d.renderRow(idxW, idW, issueW, statusW, agoW, summaryW,
		"#", "ID", "ISSUE", "STATUS", "AGO", "SUMMARY", true, nil)

	var rows []string
	for i, row := range d.runs {
		r := d.renderRow(idxW, idW, issueW, statusW, agoW, summaryW,
			fmt.Sprintf("%d", row.Index),
			row.ShortID,
			row.IssueID,
			string(row.Status),
			formatRelativeTime(row.Updated, time.Now()),
			row.Summary,
			false,
			&row,
		)
		if i == d.cursor {
			r = d.styles.Selected.Render(r)
		}
		rows = append(rows, r)
	}

	return strings.Join(append([]string{header}, rows...), "\n")
}

func (d *Dashboard) renderRow(idxW, idW, issueW, statusW, agoW, summaryW int, idx, id, issue, status, ago, summary string, header bool, row *RunRow) string {
	baseStyle := d.styles.Text
	headerStyle := d.styles.Header

	idxCol := d.pad(idx, idxW, baseStyle)
	idCol := d.pad(id, idW, baseStyle)
	issueCol := d.pad(issue, issueW, baseStyle)
	agoCol := d.pad(ago, agoW, baseStyle)
	summaryCol := d.pad(summary, summaryW, baseStyle)
	statusCol := d.pad(status, statusW, baseStyle)

	if header {
		idxCol = d.pad(idx, idxW, headerStyle)
		idCol = d.pad(id, idW, headerStyle)
		issueCol = d.pad(issue, issueW, headerStyle)
		agoCol = d.pad(ago, agoW, headerStyle)
		summaryCol = d.pad(summary, summaryW, headerStyle)
		statusCol = d.pad(status, statusW, headerStyle)
	}

	if row != nil {
		if style, ok := d.styles.Status[row.Status]; ok {
			statusCol = d.pad(status, statusW, style)
		}
	}

	return strings.Join([]string{idxCol, idCol, issueCol, statusCol, agoCol, summaryCol}, "  ")
}

func (d *Dashboard) renderStats() string {
	counts := make(map[model.Status]int)
	for _, row := range d.runs {
		counts[row.Status]++
	}

	stats := []string{
		fmt.Sprintf("running: %d", counts[model.StatusRunning]),
		fmt.Sprintf("blocked: %d", counts[model.StatusBlocked]),
		fmt.Sprintf("blocked_api: %d", counts[model.StatusBlockedAPI]),
		fmt.Sprintf("done: %d", counts[model.StatusDone]),
		fmt.Sprintf("failed: %d", counts[model.StatusFailed]),
	}

	return strings.Join(stats, "  ")
}

func (d *Dashboard) renderFooter() string {
	return d.keymap.HelpLine()
}

func (d *Dashboard) tableWidths() (idxW, idW, issueW, statusW, agoW, summaryW int) {
	idxW = 2
	idW = 6
	issueW = 12
	statusW = 10
	agoW = 6

	contentWidth := d.safeWidth()
	fixed := idxW + idW + issueW + statusW + agoW + 10
	summaryW = contentWidth - fixed
	if summaryW < 20 {
		summaryW = 20
	}
	return
}

func (d *Dashboard) safeWidth() int {
	if d.width > 2 {
		return d.width - 2
	}
	return 80
}

func (d *Dashboard) pad(s string, width int, style lipgloss.Style) string {
	return style.Width(width).Render(truncate(s, width))
}

func (d *Dashboard) answerRunIndexByWindowIndex(windowIndex int) int {
	for i, run := range d.answer.runs {
		if run.windowIndex == windowIndex {
			return i
		}
	}
	return -1
}

func (d *Dashboard) currentAnswerRun() *answerRun {
	if d.answer.selectedRun < 0 || d.answer.selectedRun >= len(d.answer.runs) {
		return nil
	}
	return &d.answer.runs[d.answer.selectedRun]
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
