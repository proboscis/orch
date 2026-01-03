package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
	"github.com/spf13/cobra"
)

type psOptions struct {
	Status       []string
	IssueStatus  []string
	Issue        string
	Limit        int
	Sort         string
	Since        string
	AbsoluteTime bool
	All          bool
}

type psIssueInfo struct {
	status  string
	display string
}

type agentAliveInfo struct {
	alive bool
	known bool
}

func newPsCmd() *cobra.Command {
	opts := &psOptions{}

	cmd := &cobra.Command{
		Use:   "ps",
		Short: "List runs",
		Long:  `List runs with optional filtering by status, issue, and time.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPs(opts)
		},
	}

	cmd.Flags().StringSliceVar(&opts.Status, "status", nil, "Filter by run status (queued,booting,running,blocked,blocked_api,pr_open,done,failed,canceled,unknown)")
	cmd.Flags().StringSliceVar(&opts.IssueStatus, "issue-status", nil, "Filter by issue status (open,resolved,closed)")
	cmd.Flags().StringVar(&opts.Issue, "issue", "", "Filter by issue ID")
	cmd.Flags().IntVar(&opts.Limit, "limit", 50, "Maximum number of runs to show")
	cmd.Flags().StringVar(&opts.Sort, "sort", "updated", "Sort by (updated|started)")
	cmd.Flags().StringVar(&opts.Since, "since", "", "Only show runs updated since (ISO8601)")
	cmd.Flags().BoolVar(&opts.AbsoluteTime, "absolute-time", false, "Show absolute timestamps instead of relative")
	cmd.Flags().BoolVarP(&opts.All, "all", "a", false, "Show all runs including those from resolved issues")

	return cmd
}

func runPs(opts *psOptions) error {
	st, err := getStore()
	if err != nil {
		return err
	}

	// Build filter
	requestedLimit := opts.Limit
	filter := &store.ListRunsFilter{
		IssueID: opts.Issue,
		Limit:   opts.Limit,
		Since:   opts.Since,
	}
	if len(opts.IssueStatus) > 0 {
		filter.Limit = 0
	}

	if len(opts.Status) > 0 {
		for _, s := range opts.Status {
			filter.Status = append(filter.Status, model.Status(s))
		}
	} else if !opts.All {
		filter.Limit = 0
	}

	runs, err := st.ListRuns(filter)
	if err != nil {
		return err
	}

	issueStatusFilter := make(map[string]bool)
	for _, status := range opts.IssueStatus {
		trimmed := strings.TrimSpace(status)
		if trimmed != "" {
			issueStatusFilter[trimmed] = true
		}
	}

	// By default, exclude runs from resolved issues unless --all is set or specific issue status is requested
	excludeResolvedIssues := !opts.All && len(issueStatusFilter) == 0

	issueCache := make(map[string]psIssueInfo)
	filteredRuns := make([]*model.Run, 0, len(runs))
	for _, r := range runs {
		info := resolveIssueInfo(st, issueCache, r.IssueID)
		if len(issueStatusFilter) > 0 && !issueStatusFilter[info.status] {
			continue
		}
		// Filter out runs from resolved issues by default
		if excludeResolvedIssues && info.status == string(model.IssueStatusResolved) {
			continue
		}
		filteredRuns = append(filteredRuns, r)
	}
	runs = filteredRuns

	if requestedLimit > 0 && len(runs) > requestedLimit {
		runs = runs[:requestedLimit]
	}

	populatePRUrls(runs)
	aliveByRun := resolveAgentAliveInfo(runs)

	// Output based on format
	now := time.Now()
	if globalOpts.JSON {
		return outputJSONWithIssueInfo(runs, now, issueCache, aliveByRun)
	}
	if globalOpts.TSV {
		return outputTSVWithIssueInfo(runs, issueCache, aliveByRun)
	}
	return outputTableWithIssueInfo(runs, now, opts.AbsoluteTime, issueCache, aliveByRun)
}

func resolveIssueInfo(st store.Store, cache map[string]psIssueInfo, issueID string) psIssueInfo {
	if info, ok := cache[issueID]; ok {
		return info
	}

	if st == nil {
		info := psIssueInfo{}
		cache[issueID] = info
		return info
	}

	issue, err := st.ResolveIssue(issueID)
	if err != nil {
		info := psIssueInfo{}
		cache[issueID] = info
		return info
	}

	info := psIssueInfo{
		status:  string(issue.Status),
		display: formatIssueTopic(issue),
	}
	cache[issueID] = info
	return info
}

func outputJSON(runs []*model.Run, now time.Time) error {
	return outputJSONWithIssueInfo(runs, now, nil, nil)
}

func outputJSONWithIssueInfo(runs []*model.Run, now time.Time, issueCache map[string]psIssueInfo, aliveByRun map[string]agentAliveInfo) error {
	type runOutput struct {
		IssueID      string `json:"issue_id"`
		IssueStatus  string `json:"issue_status"`
		RunID        string `json:"run_id"`
		ShortID      string `json:"short_id"`
		Agent        string `json:"agent,omitempty"`
		Model        string `json:"model,omitempty"`
		ModelVariant string `json:"model_variant,omitempty"`
		Status       string `json:"status"`
		AgentAlive   string `json:"agent_alive"`
		UpdatedAt    string `json:"updated_at"`
		UpdatedAgo   string `json:"updated_ago"`
		StartedAt    string `json:"started_at"`
		PRUrl        string `json:"pr_url,omitempty"`
		Branch       string `json:"branch,omitempty"`
		WorktreePath string `json:"worktree_path,omitempty"`
		TmuxSession  string `json:"tmux_session,omitempty"`
	}

	output := struct {
		OK    bool        `json:"ok"`
		Items []runOutput `json:"items"`
	}{
		OK:    true,
		Items: make([]runOutput, len(runs)),
	}

	for i, r := range runs {
		issueStatus := ""
		if issueCache != nil {
			issueStatus = issueCache[r.IssueID].status
		}
		aliveInfo := agentAliveInfo{}
		if aliveByRun != nil {
			aliveInfo = aliveByRun[r.RunID]
		}

		output.Items[i] = runOutput{
			IssueID:      r.IssueID,
			IssueStatus:  issueStatus,
			RunID:        r.RunID,
			ShortID:      r.ShortID(),
			Agent:        r.Agent,
			Model:        r.Model,
			ModelVariant: r.ModelVariant,
			Status:       string(r.Status),
			AgentAlive:   formatAliveText(aliveInfo),
			UpdatedAt:    r.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAgo:   formatRelativeTime(r.UpdatedAt, now),
			StartedAt:    r.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
			PRUrl:        r.PRUrl,
			Branch:       r.Branch,
			WorktreePath: r.WorktreePath,
			TmuxSession:  r.TmuxSession,
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

// TSV columns (fixed order per spec):
// issue_id, issue_status, run_id, short_id, agent, status, alive, updated_at, pr_url, branch, worktree_path, tmux_session
func outputTSV(runs []*model.Run) error {
	return outputTSVWithIssueInfo(runs, nil, nil)
}

func outputTSVWithIssueInfo(runs []*model.Run, issueCache map[string]psIssueInfo, aliveByRun map[string]agentAliveInfo) error {
	for _, r := range runs {
		issueStatus := ""
		if issueCache != nil {
			issueStatus = issueCache[r.IssueID].status
		}
		aliveInfo := agentAliveInfo{}
		if aliveByRun != nil {
			aliveInfo = aliveByRun[r.RunID]
		}

		fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.IssueID,
			issueStatus,
			r.RunID,
			r.ShortID(),
			r.Agent,
			r.Model,
			r.ModelVariant,
			r.Status,
			formatAliveText(aliveInfo),
			r.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
			r.PRUrl,
			r.Branch,
			r.WorktreePath,
			r.TmuxSession,
		)
	}
	return nil
}

func outputTable(runs []*model.Run, now time.Time, absoluteTime bool) error {
	return outputTableWithIssueInfo(runs, now, absoluteTime, nil, nil)
}

func outputTableWithIssueInfo(runs []*model.Run, now time.Time, absoluteTime bool, issueCache map[string]psIssueInfo, aliveByRun map[string]agentAliveInfo) error {
	if len(runs) == 0 {
		if !globalOpts.Quiet {
			fmt.Println("No runs found")
		}
		return nil
	}

	if issueCache == nil {
		st, err := getStore()
		if err != nil {
			return err
		}
		issueCache = make(map[string]psIssueInfo)
		for _, r := range runs {
			resolveIssueInfo(st, issueCache, r.IssueID)
		}
	}

	baseBranch := ""
	if cfg, err := config.Load(); err == nil && cfg.BaseBranch != "" {
		baseBranch = cfg.BaseBranch
	}

	gitStates := gitStatesForRuns(runs, baseBranch)

	// Collect data rows
	headers := []string{"ID", "ISSUE", "ISSUE-ST", "AGENT", "MODEL", "STATUS", "ALIVE", "BRANCH", "WORKTREE", "PR", "MERGED", "UPDATED", "TOPIC"}
	var rows [][]string

	for _, r := range runs {
		updated := formatRelativeTime(r.UpdatedAt, now)
		if absoluteTime {
			updated = r.UpdatedAt.Format("01-02 15:04")
		}
		displayID := r.ShortID()
		if _, err := os.Stat(r.WorktreePath); os.IsNotExist(err) {
			displayID += "*"
		}

		info := issueCache[r.IssueID]
		display := info.display
		if display == "" {
			display = "-"
		}

		issueStatus := info.status
		if issueStatus == "" {
			issueStatus = "-"
		}

		merged := "-"
		if state, ok := gitStates[r.RunID]; ok {
			merged = state
		}

		pr := "-"
		if r.PRUrl != "" || r.Status == model.StatusPROpen {
			pr = "yes"
		}

		agentDisplay := agent.AgentDisplayName(r.Agent, r.Model, r.ModelVariant)

		modelDisplay := formatModelDisplay(r.Model, r.ModelVariant)

		branch := formatBranchDisplay(r.Branch, branchMaxLen)
		worktree := formatWorktreeDisplay(r.WorktreePath, worktreeMaxLen)
		aliveInfo := agentAliveInfo{}
		if aliveByRun != nil {
			aliveInfo = aliveByRun[r.RunID]
		}

		rows = append(rows, []string{
			displayID,
			r.IssueID,
			issueStatus,
			agentDisplay,
			modelDisplay,
			colorStatus(r.Status),
			colorAlive(aliveInfo),
			branch,
			worktree,
			pr,
			merged,
			updated,
			display,
		})
	}

	// Calculate column widths
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}

	for _, row := range rows {
		for i, cell := range row {
			l := visibleLen(cell)
			if l > widths[i] {
				widths[i] = l
			}
		}
	}

	// Print table
	// Print header
	printRow(headers, widths)

	// Print rows
	for _, row := range rows {
		printRow(row, widths)
	}

	return nil
}

func printRow(row []string, widths []int) {
	for i, cell := range row {
		padding := widths[i] - visibleLen(cell)
		fmt.Print(cell)
		if i < len(row)-1 {
			fmt.Print(strings.Repeat(" ", padding+2)) // +2 space gutter
		}
	}
	fmt.Println()
}

// ansiRegex matches ANSI escape codes
// \033 is octal for ESC (27)
var ansiRegex = regexp.MustCompile(`\033\[[0-9;]*m`)

func visibleLen(s string) int {
	stripped := ansiRegex.ReplaceAllString(s, "")
	return len(stripped)
}

const (
	summaryMaxLen  = 40
	topicMaxLen    = 30
	topicMaxWords  = 5
	branchMaxLen   = 24
	worktreeMaxLen = 40
)

func formatIssueTopic(issue *model.Issue) string {
	if issue == nil {
		return ""
	}

	topic := formatTopic(issue.Topic)
	if topic != "" {
		return topic
	}

	summary := strings.TrimSpace(issue.Summary)
	if summary == "" {
		return ""
	}
	return truncateWithEllipsis(summary, summaryMaxLen)
}

func formatTopic(topic string) string {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return ""
	}

	words := strings.Fields(topic)
	if len(words) > topicMaxWords {
		topic = strings.Join(words[:topicMaxWords], " ") + "..."
	}

	if len(topic) > topicMaxLen {
		topic = truncateWithEllipsis(topic, topicMaxLen)
	}

	return topic
}

func truncateWithEllipsis(text string, max int) string {
	if len(text) <= max {
		return text
	}
	if max <= 3 {
		return text[:max]
	}
	return text[:max-3] + "..."
}

func formatModelDisplay(model, variant string) string {
	if model == "" {
		return "-"
	}
	parts := strings.Split(model, "/")
	display := model
	if len(parts) == 2 {
		display = parts[1]
	}
	if variant != "" {
		display = display + ":" + variant
	}
	return truncateWithEllipsis(display, 20)
}

func formatBranchDisplay(branch string, max int) string {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "-"
	}
	if max <= 0 {
		return branch
	}
	return truncateWithEllipsis(branch, max)
}

func formatWorktreeDisplay(path string, max int) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "-"
	}
	if max <= 0 {
		return path
	}
	path = abbreviateHome(path)
	short := shortenPath(path)
	return truncateLeading(short, max)
}

func abbreviateHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	homePrefix := home + string(os.PathSeparator)
	if strings.HasPrefix(path, homePrefix) {
		return "~" + path[len(home):]
	}
	return path
}

func shortenPath(path string) string {
	cleaned := filepath.Clean(path)
	sep := string(os.PathSeparator)
	parts := strings.Split(cleaned, sep)
	if len(parts) < 2 {
		return cleaned
	}
	suffix := filepath.Join(parts[len(parts)-2], parts[len(parts)-1])
	if suffix == cleaned {
		return cleaned
	}
	return "..." + sep + suffix
}

func truncateLeading(text string, max int) string {
	if len(text) <= max {
		return text
	}
	if max <= 3 {
		return text[:max]
	}
	return "..." + text[len(text)-(max-3):]
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

func colorStatus(status model.Status) string {
	// ANSI color codes for terminal
	colors := map[model.Status]string{
		model.StatusRunning:    "\033[32m", // green
		model.StatusBlocked:    "\033[33m", // yellow
		model.StatusBlockedAPI: "\033[33m", // yellow
		model.StatusFailed:     "\033[31m", // red
		model.StatusDone:       "\033[34m", // blue
		model.StatusPROpen:     "\033[36m", // cyan
		model.StatusQueued:     "\033[37m", // white
		model.StatusBooting:    "\033[32m", // green
		model.StatusCanceled:   "\033[90m", // gray
		model.StatusUnknown:    "\033[35m", // magenta - agent exited unexpectedly
	}

	reset := "\033[0m"
	if color, ok := colors[status]; ok {
		return color + string(status) + reset
	}
	return string(status)
}

func resolveAgentAliveInfo(runs []*model.Run) map[string]agentAliveInfo {
	if len(runs) == 0 {
		return nil
	}

	aliveByRun := make(map[string]agentAliveInfo, len(runs))

	for _, r := range runs {
		if r == nil {
			continue
		}
		manager := agent.GetManager(r)
		alive := manager.IsAlive(r)
		aliveByRun[r.RunID] = agentAliveInfo{alive: alive, known: true}
	}

	return aliveByRun
}

func formatAliveText(info agentAliveInfo) string {
	if !info.known {
		return "-"
	}
	if info.alive {
		return "yes"
	}
	return "no"
}

func colorAlive(info agentAliveInfo) string {
	text := formatAliveText(info)
	if !info.known {
		return "\033[90m" + text + "\033[0m"
	}
	if info.alive {
		return "\033[32m" + text + "\033[0m"
	}
	return "\033[31m" + text + "\033[0m"
}

func gitStatesForRuns(runs []*model.Run, target string) map[string]string {
	repoRoot, err := git.FindRepoRoot("")
	if err != nil {
		return nil
	}

	targetRef, merged, err := git.MergedBranchesForTarget(repoRoot, target)
	if err != nil {
		return nil
	}

	commitTimes, err := git.GetBranchCommitTimes(repoRoot)
	if err != nil {
		return nil
	}

	states := make(map[string]string)
	branchToRun := make(map[string]*model.Run)
	var unmergedBranches []string

	for _, r := range runs {
		if r.Branch == "" {
			continue
		}

		isMerged := merged[r.Branch]
		commitTime, hasCommitTime := commitTimes[r.Branch]
		isNewWork := hasCommitTime && (r.StartedAt.IsZero() || !commitTime.Before(r.StartedAt))

		if isMerged {
			if isNewWork {
				states[r.RunID] = "merged"
			} else {
				states[r.RunID] = "no change"
			}
			continue
		}

		branchToRun[r.Branch] = r
		unmergedBranches = append(unmergedBranches, r.Branch)
	}

	if len(unmergedBranches) == 0 {
		return states
	}

	aheadCounts := git.GetBranchesAheadCounts(repoRoot, targetRef, unmergedBranches)

	var branchesWithChanges []string
	for branch, r := range branchToRun {
		ahead := aheadCounts[branch]
		if ahead == 0 {
			states[r.RunID] = "no change"
		} else {
			branchesWithChanges = append(branchesWithChanges, branch)
		}
	}

	if len(branchesWithChanges) == 0 {
		return states
	}

	conflicts := git.CheckMergeConflicts(repoRoot, targetRef, branchesWithChanges)

	for _, branch := range branchesWithChanges {
		r := branchToRun[branch]
		if conflicts[branch] {
			states[r.RunID] = "conflict"
		} else {
			states[r.RunID] = "clean"
		}
	}

	return states
}

// parseStatusList parses a comma-separated status list
func parseStatusList(s string) []model.Status {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	statuses := make([]model.Status, len(parts))
	for i, p := range parts {
		statuses[i] = model.Status(strings.TrimSpace(p))
	}
	return statuses
}
