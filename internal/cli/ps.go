package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
	"github.com/spf13/cobra"
)

type psOptions struct {
	Status []string
	Issue  string
	Limit  int
	Sort   string
	Since  string
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

	cmd.Flags().StringSliceVar(&opts.Status, "status", nil, "Filter by status (running,blocked,failed,pr_open,done)")
	cmd.Flags().StringVar(&opts.Issue, "issue", "", "Filter by issue ID")
	cmd.Flags().IntVar(&opts.Limit, "limit", 50, "Maximum number of runs to show")
	cmd.Flags().StringVar(&opts.Sort, "sort", "updated", "Sort by (updated|started)")
	cmd.Flags().StringVar(&opts.Since, "since", "", "Only show runs updated since (ISO8601)")

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
	if globalOpts.JSON {
		return outputJSON(runs)
	}
	if globalOpts.TSV {
		return outputTSV(runs)
	}
	return outputTable(runs)
}

func outputJSON(runs []*model.Run) error {
	type runOutput struct {
		IssueID      string `json:"issue_id"`
		RunID        string `json:"run_id"`
		Status       string `json:"status"`
		Phase        string `json:"phase,omitempty"`
		UpdatedAt    string `json:"updated_at"`
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
			Status:       string(r.Status),
			Phase:        string(r.Phase),
			UpdatedAt:    r.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
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
// issue_id, run_id, status, phase, updated_at, pr_url, branch, worktree_path, tmux_session
func outputTSV(runs []*model.Run) error {
	for _, r := range runs {
		fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.IssueID,
			r.RunID,
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

func outputTable(runs []*model.Run) error {
	if len(runs) == 0 {
		if !globalOpts.Quiet {
			fmt.Println("No runs found")
		}
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ISSUE\tRUN\tSTATUS\tPHASE\tUPDATED\tBRANCH")

	for _, r := range runs {
		// Truncate branch for display
		branch := r.Branch
		if len(branch) > 30 {
			branch = "..." + branch[len(branch)-27:]
		}

		// Format updated time as relative or short form
		updated := r.UpdatedAt.Format("01-02 15:04")

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			r.IssueID,
			r.RunID,
			colorStatus(r.Status),
			r.Phase,
			updated,
			branch,
		)
	}

	return w.Flush()
}

func colorStatus(status model.Status) string {
	// ANSI color codes for terminal
	colors := map[model.Status]string{
		model.StatusRunning:  "\033[32m", // green
		model.StatusBlocked:  "\033[33m", // yellow
		model.StatusFailed:   "\033[31m", // red
		model.StatusDone:     "\033[34m", // blue
		model.StatusPROpen:   "\033[36m", // cyan
		model.StatusQueued:   "\033[37m", // white
		model.StatusBooting:  "\033[32m", // green
		model.StatusCanceled: "\033[90m", // gray
		model.StatusUnknown:  "\033[35m", // magenta - agent exited unexpectedly
	}

	reset := "\033[0m"
	if color, ok := colors[status]; ok {
		return color + string(status) + reset
	}
	return string(status)
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
