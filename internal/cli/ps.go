package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/orchapi"
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
	Verbose      bool
	NoGit        bool
	NoAlive      bool
}

type psIssueInfo struct {
	status  string
	display string
}

type agentAliveInfo struct {
	alive bool
	known bool
}

type psDeps struct {
	getAPI func() (orchapi.OrchAPI, error)
}

func defaultPsDeps() *psDeps {
	return &psDeps{getAPI: getAPIForListing}
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

	cmd.Flags().StringSliceVar(&opts.Status, "status", nil, "Filter by run status (queued,booting,running,waiting,rate_limited,pr_open,done,failed,canceled,unknown)")
	cmd.Flags().StringSliceVar(&opts.IssueStatus, "issue-status", nil, "Filter by issue status (open,resolved,closed)")
	cmd.Flags().StringVar(&opts.Issue, "issue", "", "Filter by issue ID")
	cmd.Flags().IntVar(&opts.Limit, "limit", 50, "Maximum number of runs to show")
	cmd.Flags().StringVar(&opts.Sort, "sort", "updated", "Sort by (updated|started)")
	cmd.Flags().StringVar(&opts.Since, "since", "", "Only show runs updated since (ISO8601)")
	cmd.Flags().BoolVar(&opts.AbsoluteTime, "absolute-time", false, "Show absolute timestamps instead of relative")
	cmd.Flags().BoolVarP(&opts.All, "all", "a", false, "Show all runs including those from resolved issues")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "Show additional debug info (daemon log location)")
	cmd.Flags().BoolVar(&opts.NoGit, "no-git", false, "Skip git merge state checks for faster listing")
	cmd.Flags().BoolVar(&opts.NoAlive, "no-alive", false, "Skip agent alive checks for faster listing")

	return cmd
}

func runPs(opts *psOptions) error {
	return runPsWithDeps(context.Background(), opts, defaultPsDeps())
}

func runPsWithDeps(ctx context.Context, opts *psOptions, deps *psDeps) error {
	api, err := deps.getAPI()
	if err != nil {
		return err
	}

	requestedLimit := opts.Limit

	statusFilter := make([]orchapi.RunStatus, len(opts.Status))
	for i, s := range opts.Status {
		statusFilter[i] = orchapi.NormalizeRunStatus(s)
	}

	limit := opts.Limit
	if len(opts.IssueStatus) > 0 || (!opts.All && len(opts.Status) == 0) {
		limit = 0
	}

	filter := &orchapi.ListRunsFilter{
		IssueID: opts.Issue,
		Status:  statusFilter,
		Limit:   limit,
	}

	result, err := api.ListRuns(ctx, filter)
	if err != nil {
		return err
	}

	runs := make([]*model.Run, len(result.Runs))
	aliveByRun := make(map[string]agentAliveInfo, len(result.Runs))
	branchStateByRun := make(map[string]string, len(result.Runs))
	issueCache := make(map[string]psIssueInfo, len(result.Runs))
	for i, r := range result.Runs {
		runs[i] = apiRunToModelRun(r)
		aliveByRun[r.RunID] = agentAliveInfo{alive: r.Alive, known: r.AliveKnown}
		branchStateByRun[r.RunID] = string(r.BranchState)
		if _, ok := issueCache[r.IssueID]; !ok {
			issueCache[r.IssueID] = psIssueInfo{status: r.IssueStatus, display: formatTopic(r.IssueTopic)}
		}
	}

	issueStatusFilter := make(map[string]bool)
	for _, status := range opts.IssueStatus {
		trimmed := strings.TrimSpace(status)
		if trimmed != "" {
			issueStatusFilter[trimmed] = true
		}
	}

	if len(issueStatusFilter) > 0 {
		filteredRuns := make([]*model.Run, 0, len(runs))
		for _, r := range runs {
			info := issueCache[r.IssueID]
			if info.status == "" {
				info = resolveIssueInfoAPI(ctx, api, issueCache, r.IssueID)
			}
			if !issueStatusFilter[info.status] {
				continue
			}
			filteredRuns = append(filteredRuns, r)
		}
		runs = filteredRuns
	}

	if requestedLimit > 0 && len(runs) > requestedLimit {
		runs = runs[:requestedLimit]
	}

	if opts.NoAlive {
		aliveByRun = nil
	}
	targetHostByRun := resolveTargetHostByRun(runs)

	now := time.Now()
	var outputErr error
	if globalOpts.JSON {
		outputErr = outputJSONWithIssueInfo(runs, now, issueCache, aliveByRun, branchStateByRun, targetHostByRun)
	} else if globalOpts.TSV {
		outputErr = outputTSVWithIssueInfo(runs, issueCache, aliveByRun, branchStateByRun, targetHostByRun)
	} else {
		outputErr = outputTableWithIssueInfoAndBranchStateWithDeps(ctx, runs, now, opts, issueCache, aliveByRun, branchStateByRun, targetHostByRun, deps)
	}

	if outputErr != nil {
		return outputErr
	}

	if opts.Verbose {
		fmt.Println()
		daemonStatus, err := api.GetDaemonStatus(ctx)
		if err != nil {
			fmt.Printf("Daemon running: unknown (error: %v)\n", err)
		} else {
			fmt.Printf("Daemon running: %v\n", daemonStatus.Running)
			if daemonStatus.Running {
				fmt.Printf("Daemon PID: %d\n", daemonStatus.PID)
			}
			fmt.Printf("Daemon log: %s\n", daemonStatus.LogPath)
		}
	}

	return nil
}

func resolveIssueInfoAPI(ctx context.Context, api orchapi.OrchAPI, cache map[string]psIssueInfo, issueID string) psIssueInfo {
	if info, ok := cache[issueID]; ok {
		return info
	}

	if api == nil {
		info := psIssueInfo{}
		cache[issueID] = info
		return info
	}

	issue, err := api.GetIssue(ctx, issueID)
	if err != nil || issue == nil {
		info := psIssueInfo{}
		cache[issueID] = info
		return info
	}

	info := psIssueInfo{
		status:  string(issue.Status),
		display: formatIssueTopicAPI(issue),
	}
	cache[issueID] = info
	return info
}

func formatIssueTopicAPI(issue *orchapi.Issue) string {
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

func outputJSON(runs []*model.Run, now time.Time) error {
	return outputJSONWithIssueInfo(runs, now, nil, nil, nil, nil)
}

func outputJSONWithIssueInfo(
	runs []*model.Run,
	now time.Time,
	issueCache map[string]psIssueInfo,
	aliveByRun map[string]agentAliveInfo,
	branchStateByRun map[string]string,
	targetHostByRun map[string]string,
) error {
	if targetHostByRun == nil {
		targetHostByRun = resolveTargetHostByRun(runs)
	}

	type runOutput struct {
		IssueID           string `json:"issue_id"`
		IssueStatus       string `json:"issue_status"`
		RunID             string `json:"run_id"`
		ShortID           string `json:"short_id"`
		CLI               string `json:"cli,omitempty"`
		Model             string `json:"model,omitempty"`
		ModelVariant      string `json:"model_variant,omitempty"`
		Target            string `json:"target,omitempty"`
		TargetHost        string `json:"target_host,omitempty"`
		Status            string `json:"status"`
		AgentStatus       string `json:"agent_status"`
		BranchStatus      string `json:"branch_status"`
		PRStatus          string `json:"pr_status"`
		AgentAlive        string `json:"agent_alive"`
		UpdatedAt         string `json:"updated_at"`
		UpdatedAgo        string `json:"updated_ago"`
		StartedAt         string `json:"started_at"`
		PRUrl             string `json:"pr_url,omitempty"`
		Branch            string `json:"branch,omitempty"`
		WorktreePath      string `json:"worktree_path,omitempty"`
		SessionName       string `json:"session_name,omitempty"`
		ServerPort        int    `json:"server_port,omitempty"`
		OpenCodeSessionID string `json:"opencode_session_id,omitempty"`
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
		branchStatus := "-"
		if branchStateByRun != nil {
			if state := branchStateByRun[r.RunID]; state != "" {
				branchStatus = branchStatusFromGitState(state)
			}
		}
		target := strings.TrimSpace(r.Target)
		targetHost := strings.TrimSpace(targetHostByRun[r.RunID])

		output.Items[i] = runOutput{
			IssueID:           r.IssueID,
			IssueStatus:       issueStatus,
			RunID:             r.RunID,
			ShortID:           r.ShortID(),
			CLI:               r.Agent,
			Model:             r.Model,
			ModelVariant:      r.ModelVariant,
			Target:            target,
			TargetHost:        targetHost,
			Status:            string(r.Status),
			AgentStatus:       shortAgentStatus(r.Status),
			BranchStatus:      branchStatus,
			PRStatus:          prStatusFromRun(r, branchStateByRun[r.RunID]),
			AgentAlive:        formatAliveText(aliveInfo),
			UpdatedAt:         r.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAgo:        formatRelativeTime(r.UpdatedAt, now),
			StartedAt:         r.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
			PRUrl:             r.PRUrl,
			Branch:            r.Branch,
			WorktreePath:      r.WorktreePath,
			SessionName:       r.SessionName,
			ServerPort:        r.ServerPort,
			OpenCodeSessionID: r.OpenCodeSessionID,
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

func outputTSV(runs []*model.Run) error {
	return outputTSVWithIssueInfo(runs, nil, nil, nil, nil)
}

func outputTSVWithIssueInfo(
	runs []*model.Run,
	issueCache map[string]psIssueInfo,
	aliveByRun map[string]agentAliveInfo,
	branchStateByRun map[string]string,
	targetHostByRun map[string]string,
) error {
	if targetHostByRun == nil {
		targetHostByRun = resolveTargetHostByRun(runs)
	}

	for _, r := range runs {
		issueStatus := ""
		if issueCache != nil {
			issueStatus = issueCache[r.IssueID].status
		}
		aliveInfo := agentAliveInfo{}
		if aliveByRun != nil {
			aliveInfo = aliveByRun[r.RunID]
		}
		branchStatus := "-"
		if branchStateByRun != nil {
			if state := branchStateByRun[r.RunID]; state != "" {
				branchStatus = branchStatusFromGitState(state)
			}
		}
		target := strings.TrimSpace(r.Target)
		targetHost := strings.TrimSpace(targetHostByRun[r.RunID])

		fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.IssueID,
			issueStatus,
			r.RunID,
			r.ShortID(),
			r.Agent,
			r.Model,
			r.ModelVariant,
			target,
			targetHost,
			r.Status,
			shortAgentStatus(r.Status),
			branchStatus,
			prStatusFromRun(r, branchStateByRun[r.RunID]),
			formatAliveText(aliveInfo),
			r.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
			r.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
			r.PRUrl,
			r.Branch,
			r.WorktreePath,
			r.SessionName,
		)
	}
	return nil
}

func outputTable(runs []*model.Run, now time.Time, absoluteTime bool) error {
	return outputTableWithIssueInfoAndBranchStateWithDeps(
		context.Background(),
		runs,
		now,
		&psOptions{AbsoluteTime: absoluteTime},
		nil,
		nil,
		nil,
		nil,
		defaultPsDeps(),
	)
}

func outputTableWithIssueInfoAndBranchState(
	runs []*model.Run,
	now time.Time,
	opts *psOptions,
	issueCache map[string]psIssueInfo,
	aliveByRun map[string]agentAliveInfo,
	branchStateByRun map[string]string,
	targetHostByRun map[string]string,
) error {
	return outputTableWithIssueInfoAndBranchStateWithDeps(
		context.Background(),
		runs,
		now,
		opts,
		issueCache,
		aliveByRun,
		branchStateByRun,
		targetHostByRun,
		defaultPsDeps(),
	)
}

func outputTableWithIssueInfoAndBranchStateWithDeps(
	ctx context.Context,
	runs []*model.Run,
	now time.Time,
	opts *psOptions,
	issueCache map[string]psIssueInfo,
	aliveByRun map[string]agentAliveInfo,
	branchStateByRun map[string]string,
	targetHostByRun map[string]string,
	deps *psDeps,
) error {
	if deps == nil {
		deps = defaultPsDeps()
	}

	if len(runs) == 0 {
		if !globalOpts.Quiet {
			fmt.Println("No runs found")
		}
		return nil
	}

	if issueCache == nil {
		issueCache = make(map[string]psIssueInfo)

		api, err := deps.getAPI()
		if err == nil && api != nil {
			for _, r := range runs {
				resolveIssueInfoAPI(ctx, api, issueCache, r.IssueID)
			}
		}
	}

	return outputTableWithGitStates(runs, now, opts, issueCache, aliveByRun, branchStateByRun, targetHostByRun)
}

func outputTableWithGitStates(
	runs []*model.Run,
	now time.Time,
	opts *psOptions,
	issueCache map[string]psIssueInfo,
	aliveByRun map[string]agentAliveInfo,
	gitStates map[string]string,
	targetHostByRun map[string]string,
) error {
	if targetHostByRun == nil {
		targetHostByRun = resolveTargetHostByRun(runs)
	}

	headers := []string{"ID", "ISSUE", "ISSUE-ST", "CLI", "MODEL", "TARGET", "HOST", "AGENT", "ALIVE", "BRANCH", "WORKTREE", "PR", "STARTED", "UPDATED", "TOPIC"}
	var rows [][]string

	for _, r := range runs {
		started := formatRelativeTime(r.StartedAt, now)
		if opts.AbsoluteTime {
			started = r.StartedAt.Format("01-02 15:04")
		}
		updated := formatRelativeTime(r.UpdatedAt, now)
		if opts.AbsoluteTime {
			updated = r.UpdatedAt.Format("01-02 15:04")
		}
		displayID := r.ShortID()
		if r.WorktreePath != "" && !r.WorktreeExists {
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

		gitState := "-"
		if state, ok := gitStates[r.RunID]; ok {
			gitState = state
		}

		branchStatus := branchStatusFromGitState(gitState)
		prStatus := prStatusFromRun(r, gitState)

		cliDisplay := agent.AgentDisplayName(r.Agent, r.Model, r.ModelVariant)
		modelDisplay := formatModelDisplay(r.Model, r.ModelVariant)
		targetDisplay := formatTargetDisplay(r.Target, targetMaxLen)
		targetHostDisplay := formatTargetDisplay(targetHostByRun[r.RunID], targetHostMaxLen)
		worktree := formatWorktreeDisplay(r.WorktreePath, worktreeMaxLen)
		aliveInfo := agentAliveInfo{}
		if aliveByRun != nil {
			aliveInfo = aliveByRun[r.RunID]
		}

		rows = append(rows, []string{
			displayID,
			r.IssueID,
			issueStatus,
			cliDisplay,
			modelDisplay,
			targetDisplay,
			targetHostDisplay,
			colorShortStatus(r.Status),
			colorAlive(aliveInfo),
			colorBranchStatus(branchStatus),
			worktree,
			colorPrStatus(prStatus),
			started,
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
	summaryMaxLen    = 40
	topicMaxLen      = 30
	topicMaxWords    = 5
	branchMaxLen     = 24
	worktreeMaxLen   = 40
	targetMaxLen     = 16
	targetHostMaxLen = 20
)

func shortAgentStatus(status model.Status) string {
	switch status {
	case model.StatusQueued:
		return "queue"
	case model.StatusBooting:
		return "boot"
	case model.StatusRunning:
		return "run"
	case model.StatusWaiting:
		return "wait"
	case model.StatusRateLimited:
		return "rlimit"
	case model.StatusPROpen:
		return "pr"
	case model.StatusDone:
		return "done"
	case model.StatusFailed:
		return "fail"
	case model.StatusCanceled:
		return "cancel"
	case model.StatusUnknown:
		return "?"
	default:
		return "?"
	}
}

func prStatusFromRun(r *model.Run, gitState string) string {
	if r.Status == model.StatusDone && r.PRUrl != "" {
		return "merged"
	}
	if gitState == "merged" && r.PRUrl != "" {
		return "merged"
	}
	if r.PRUrl != "" || r.Status == model.StatusPROpen {
		return "open"
	}
	return "-"
}

func branchStatusFromGitState(gitState string) string {
	switch gitState {
	case "clean":
		return "clean"
	case "dirty":
		return "dirty"
	case "merged":
		return "merged"
	case "conflict":
		return "conflict"
	case "uncommit":
		return "dirty"
	case "ahead":
		return "ahead"
	case "behind":
		return "behind"
	case "diverged":
		return "diverged"
	case "synced":
		return "synced"
	default:
		return "-"
	}
}

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

func formatTargetDisplay(target string, max int) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return "-"
	}
	if max <= 0 {
		return target
	}
	return truncateWithEllipsis(target, max)
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

func resolveTargetHostByRun(runs []*model.Run) map[string]string {
	resolved := make(map[string]string, len(runs))
	if len(runs) == 0 {
		return resolved
	}

	for _, run := range runs {
		if run == nil {
			continue
		}
		targetName := strings.TrimSpace(run.Target)
		if targetName == "" {
			continue
		}

		targetHost := strings.TrimSpace(run.TargetHost)
		if targetHost == "" {
			targetHost = targetName
		}
		resolved[run.RunID] = targetHost
	}

	return resolved
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
	colors := map[model.Status]string{
		model.StatusRunning:     "\033[32m",
		model.StatusWaiting:     "\033[33m",
		model.StatusRateLimited: "\033[33m",
		model.StatusFailed:      "\033[31m",
		model.StatusDone:        "\033[34m",
		model.StatusPROpen:      "\033[36m",
		model.StatusQueued:      "\033[37m",
		model.StatusBooting:     "\033[32m",
		model.StatusCanceled:    "\033[90m",
		model.StatusUnknown:     "\033[35m",
	}

	reset := "\033[0m"
	if color, ok := colors[status]; ok {
		return color + string(status) + reset
	}
	return string(status)
}

func colorShortStatus(status model.Status) string {
	colors := map[model.Status]string{
		model.StatusRunning:     "\033[32m",
		model.StatusWaiting:     "\033[33m",
		model.StatusRateLimited: "\033[33m",
		model.StatusFailed:      "\033[31m",
		model.StatusDone:        "\033[34m",
		model.StatusPROpen:      "\033[36m",
		model.StatusQueued:      "\033[37m",
		model.StatusBooting:     "\033[32m",
		model.StatusCanceled:    "\033[90m",
		model.StatusUnknown:     "\033[35m",
	}

	short := shortAgentStatus(status)
	reset := "\033[0m"
	if color, ok := colors[status]; ok {
		return color + short + reset
	}
	return short
}

func colorBranchStatus(status string) string {
	colors := map[string]string{
		"clean":    "\033[32m",
		"dirty":    "\033[33m",
		"merged":   "\033[34m",
		"conflict": "\033[31m",
		"ahead":    "\033[32m",
		"behind":   "\033[33m",
		"diverged": "\033[35m",
		"synced":   "\033[90m",
	}

	reset := "\033[0m"
	if color, ok := colors[status]; ok {
		return color + status + reset
	}
	return status
}

func colorPrStatus(status string) string {
	colors := map[string]string{
		"open":   "\033[36m",
		"merged": "\033[32m",
		"closed": "\033[90m",
	}

	reset := "\033[0m"
	if color, ok := colors[status]; ok {
		return color + status + reset
	}
	return status
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

// parseStatusList parses a comma-separated status list
func parseStatusList(s string) []model.Status {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	statuses := make([]model.Status, len(parts))
	for i, p := range parts {
		statuses[i] = model.NormalizeStatus(strings.TrimSpace(p))
	}
	return statuses
}
