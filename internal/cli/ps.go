package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
	"github.com/spf13/cobra"
)

type psOptions struct {
	Status       []string
	Issue        string
	Limit        int
	Sort         string
	Since        string
	AbsoluteTime bool
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

	cmd.Flags().StringSliceVar(&opts.Status, "status", nil, "Filter by status (running,blocked,blocked_api,failed,pr_open,done)")
	cmd.Flags().StringVar(&opts.Issue, "issue", "", "Filter by issue ID")
	cmd.Flags().IntVar(&opts.Limit, "limit", 50, "Maximum number of runs to show")
	cmd.Flags().StringVar(&opts.Sort, "sort", "updated", "Sort by (updated|started)")
	cmd.Flags().StringVar(&opts.Since, "since", "", "Only show runs updated since (ISO8601)")
	cmd.Flags().BoolVar(&opts.AbsoluteTime, "absolute-time", false, "Show absolute timestamps instead of relative")

	return cmd
}

func runPs(opts *psOptions) error {
	st, err := getStore()
	if err != nil {
		return err
	}

	// Build filter
	filter := &store.ListRunsFilter{
		IssueID: opts.Issue,
		Limit:   opts.Limit,
		Since:   opts.Since,
	}

	for _, s := range opts.Status {
		filter.Status = append(filter.Status, model.Status(s))
	}

	runs, err := st.ListRuns(filter)
	if err != nil {
		return err
	}

	// Output based on format
	now := time.Now()
	if globalOpts.JSON {
		return outputJSON(runs, now)
	}
	if globalOpts.TSV {
		return outputTSV(runs)
	}
	return outputTable(runs, now, opts.AbsoluteTime)
}

func outputJSON(runs []*model.Run, now time.Time) error {
	type runOutput struct {
		IssueID      string `json:"issue_id"`
		RunID        string `json:"run_id"`
		ShortID      string `json:"short_id"`
		Status       string `json:"status"`
		Phase        string `json:"phase,omitempty"`
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
		output.Items[i] = runOutput{
			IssueID:      r.IssueID,
			RunID:        r.RunID,
			ShortID:      r.ShortID(),
			Status:       string(r.Status),
			Phase:        string(r.Phase),
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
// issue_id, run_id, short_id, status, phase, updated_at, pr_url, branch, worktree_path, tmux_session
func outputTSV(runs []*model.Run) error {
	for _, r := range runs {
		fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.IssueID,
			r.RunID,
			r.ShortID(),
			r.Status,
			r.Phase,
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
	if len(runs) == 0 {
		if !globalOpts.Quiet {
			fmt.Println("No runs found")
		}
		return nil
	}

	// Get store to fetch issue summaries
	st, err := getStore()
	if err != nil {
		return err
	}

	// Build issue summary cache
	issueSummaries := make(map[string]string)
	for _, r := range runs {
		if _, ok := issueSummaries[r.IssueID]; !ok {
			if issue, err := st.ResolveIssue(r.IssueID); err == nil {
				issueSummaries[r.IssueID] = issue.Summary
			}
		}
	}

	mergedBranches := mergedBranchesForRuns(runs)

	// Collect data rows
	headers := []string{"ID", "ISSUE", "STATUS", "PHASE", "MERGED", "UPDATED", "SUMMARY"}
	var rows [][]string

	for _, r := range runs {
		updated := formatRelativeTime(r.UpdatedAt, now)
		if absoluteTime {
			updated = r.UpdatedAt.Format("01-02 15:04")
		}

		// Get issue summary, truncate if too long
		summary := issueSummaries[r.IssueID]
		if summary == "" {
			summary = "-"
		} else if len(summary) > 40 {
			summary = summary[:37] + "..."
		}

		merged := "-"
		if r.Branch != "" && mergedBranches[r.Branch] {
			merged = "yes"
		}

		phase := string(r.Phase)
		if phase == "" {
			phase = "-"
		}

		rows = append(rows, []string{
			r.ShortID(),
			r.IssueID,
			colorStatus(r.Status),
			phase,
			merged,
			updated,
			summary,
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

func mergedBranchesForRuns(runs []*model.Run) map[string]bool {
	for _, r := range runs {
		if r.Branch != "" {
			repoRoot, err := git.FindRepoRoot("")
			if err != nil {
				return nil
			}
			merged, err := git.GetMergedBranches(repoRoot, "main")
			if err != nil {
				return nil
			}
			return merged
		}
	}
	return nil
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
