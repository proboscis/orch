package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/orchapi"
	"github.com/spf13/cobra"
)

type deleteOptions struct {
	All          bool
	OlderThan    string
	Status       string
	Force        bool
	DryRun       bool
	WithWorktree bool
	WithBranch   bool
}

// deleteResult holds the result of a delete operation for JSON output
type deleteResult struct {
	Deleted []deletedRun `json:"deleted"`
	Errors  []string     `json:"errors,omitempty"`
}

type deletedRun struct {
	IssueID         string `json:"issue_id"`
	RunID           string `json:"run_id"`
	ShortID         string `json:"short_id"`
	WorktreeRemoved bool   `json:"worktree_removed,omitempty"`
	BranchRemoved   bool   `json:"branch_removed,omitempty"`
	SessionKilled   bool   `json:"session_killed,omitempty"`
}

func newDeleteCmd() *cobra.Command {
	opts := &deleteOptions{}

	cmd := &cobra.Command{
		Use:   "delete [RUN_REF | ISSUE_ID]",
		Short: "Delete runs and their associated resources",
		Long: `Delete runs by removing their documents and associated resources.

If given a specific RUN_REF (e.g., issue#run or short ID), deletes that run.
If given an ISSUE_ID with --all, deletes all runs for that issue.
If given --older-than without an argument, deletes runs older than the specified duration.

By default, prompts for confirmation unless --force is used.
Use --dry-run to see what would be deleted without actually deleting.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate options
			if opts.OlderThan != "" && len(args) > 0 {
				// --older-than with RUN_REF doesn't make sense
				if !opts.All {
					return fmt.Errorf("--older-than cannot be used with a specific run reference")
				}
			}

			if opts.OlderThan != "" && len(args) == 0 {
				return runDeleteByAge(opts)
			}

			if len(args) == 0 && !opts.All {
				return fmt.Errorf("RUN_REF required (or use --older-than)")
			}

			if len(args) == 0 && opts.All {
				return fmt.Errorf("ISSUE_ID required with --all")
			}

			return runDelete(args[0], opts)
		},
	}

	cmd.Flags().BoolVar(&opts.All, "all", false, "Delete all runs for the specified issue")
	cmd.Flags().StringVar(&opts.OlderThan, "older-than", "", "Delete runs older than duration (e.g., 7d, 2w, 1m)")
	cmd.Flags().StringVar(&opts.Status, "status", "", "Only delete runs with specific status (done/failed/canceled)")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be deleted without deleting")
	cmd.Flags().BoolVar(&opts.WithWorktree, "with-worktree", false, "Also remove git worktree")
	cmd.Flags().BoolVar(&opts.WithBranch, "with-branch", false, "Also remove git branch")

	return cmd
}

// durationToOlderThan parses a duration string like "7d", "2w", "1m" and returns
// an ISO8601 timestamp representing the cutoff time (now - duration).
// Runs updated before this time are considered "older than" the duration.
func durationToOlderThan(s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("empty duration")
	}

	re := regexp.MustCompile(`^(\d+)([dwmDWM])$`)
	matches := re.FindStringSubmatch(s)
	if matches == nil {
		return "", fmt.Errorf("invalid duration format: %s (use 7d, 2w, or 1m)", s)
	}

	value, _ := strconv.Atoi(matches[1])
	unit := strings.ToLower(matches[2])

	var duration time.Duration
	switch unit {
	case "d":
		duration = time.Duration(value) * 24 * time.Hour
	case "w":
		duration = time.Duration(value) * 7 * 24 * time.Hour
	case "m":
		duration = time.Duration(value) * 30 * 24 * time.Hour
	default:
		return "", fmt.Errorf("unknown duration unit: %s", unit)
	}

	cutoff := time.Now().Add(-duration)
	return cutoff.Format(time.RFC3339), nil
}

// parseStatus parses a status string into a model.Status slice
func parseStatus(s string) ([]model.Status, error) {
	if s == "" {
		return nil, nil
	}

	status := model.Status(s)
	switch status {
	case model.StatusDone, model.StatusFailed, model.StatusCanceled:
		return []model.Status{status}, nil
	case model.StatusRunning, model.StatusBooting, model.StatusBlocked, model.StatusBlockedAPI, model.StatusQueued:
		return nil, fmt.Errorf("cannot delete %s runs (use 'orch stop' first)", status)
	default:
		return nil, fmt.Errorf("unknown status: %s", s)
	}
}

func runDelete(refStr string, opts *deleteOptions) error {
	ctx := context.Background()
	api, err := getAPI()
	if err != nil {
		return err
	}

	ref, err := orchapi.ParseRunRef(refStr)
	if err != nil {
		return err
	}

	if ref.IsLatest() || opts.All {
		return deleteIssueRuns(ctx, api, ref.IssueID, opts)
	}

	run, err := api.ResolveRun(ctx, ref)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run not found: %s\n", refStr)
		os.Exit(ExitRunNotFound)
		return err
	}

	return deleteRuns(ctx, api, []*orchapi.Run{run}, opts)
}

func deleteIssueRuns(ctx context.Context, api orchapi.OrchAPI, issueID string, opts *deleteOptions) error {
	filter := &orchapi.ListRunsFilter{
		IssueID: issueID,
	}

	if opts.Status != "" {
		statuses, err := parseStatusAPI(opts.Status)
		if err != nil {
			return err
		}
		filter.Status = statuses
	} else if !opts.Force {
		// When no explicit status filter and not forcing, only fetch deletable statuses
		filter.Status = []orchapi.RunStatus{
			orchapi.RunStatusDone,
			orchapi.RunStatusFailed,
			orchapi.RunStatusCanceled,
		}
	}

	if opts.OlderThan != "" {
		olderThan, err := durationToOlderThan(opts.OlderThan)
		if err != nil {
			return err
		}
		filter.OlderThan = olderThan
	}

	result, err := api.ListRuns(ctx, filter)
	if err != nil {
		return err
	}

	if len(result.Runs) == 0 {
		if !globalOpts.Quiet {
			fmt.Printf("No runs found for issue: %s\n", issueID)
		}
		return nil
	}

	return deleteRuns(ctx, api, result.Runs, opts)
}

func runDeleteByAge(opts *deleteOptions) error {
	ctx := context.Background()
	api, err := getAPI()
	if err != nil {
		return err
	}

	olderThan, err := durationToOlderThan(opts.OlderThan)
	if err != nil {
		return err
	}

	filter := &orchapi.ListRunsFilter{
		OlderThan: olderThan,
	}

	if opts.Status != "" {
		statuses, err := parseStatusAPI(opts.Status)
		if err != nil {
			return err
		}
		filter.Status = statuses
	} else if !opts.Force {
		filter.Status = []orchapi.RunStatus{
			orchapi.RunStatusDone,
			orchapi.RunStatusFailed,
			orchapi.RunStatusCanceled,
		}
	}

	result, err := api.ListRuns(ctx, filter)
	if err != nil {
		return err
	}

	if len(result.Runs) == 0 {
		if !globalOpts.Quiet {
			fmt.Println("No runs found")
		}
		return nil
	}

	return deleteRuns(ctx, api, result.Runs, opts)
}

func parseStatusAPI(s string) ([]orchapi.RunStatus, error) {
	if s == "" {
		return nil, nil
	}

	status := orchapi.RunStatus(s)
	switch status {
	case orchapi.RunStatusDone, orchapi.RunStatusFailed, orchapi.RunStatusCanceled:
		return []orchapi.RunStatus{status}, nil
	case orchapi.RunStatusRunning, orchapi.RunStatusBooting, orchapi.RunStatusBlocked, orchapi.RunStatusBlockedAPI, orchapi.RunStatusQueued:
		return nil, fmt.Errorf("cannot delete %s runs (use 'orch stop' first)", status)
	default:
		return nil, fmt.Errorf("unknown status: %s", s)
	}
}

func deleteRuns(ctx context.Context, api orchapi.OrchAPI, runs []*orchapi.Run, opts *deleteOptions) error {
	if len(runs) == 0 {
		if !globalOpts.Quiet {
			fmt.Println("No matching runs to delete")
		}
		return nil
	}

	if !globalOpts.Quiet || opts.DryRun {
		action := "Deleting"
		if opts.DryRun {
			action = "Would delete"
		}
		fmt.Printf("%s %d run(s):\n", action, len(runs))
		for _, run := range runs {
			extras := []string{}
			if opts.WithWorktree && run.WorktreePath != "" {
				extras = append(extras, "worktree")
			}
			if opts.WithBranch && run.Branch != "" {
				extras = append(extras, "branch")
			}
			if run.SessionName != "" {
				extras = append(extras, "session")
			}
			extraStr := ""
			if len(extras) > 0 {
				extraStr = fmt.Sprintf(" (+%s)", strings.Join(extras, ", "))
			}
			fmt.Printf("  %s#%s [%s] %s%s\n", run.IssueID, run.RunID, run.ShortID, run.Status, extraStr)
		}
	}

	if opts.DryRun {
		return nil
	}

	if !opts.Force && !confirmDelete(len(runs)) {
		fmt.Println("Aborted")
		return nil
	}

	result := &deleteResult{
		Deleted: make([]deletedRun, 0, len(runs)),
	}

	deleteOpts := &orchapi.DeleteRunOptions{
		WithWorktree: opts.WithWorktree,
		WithBranch:   opts.WithBranch,
		Force:        opts.Force,
	}

	for _, run := range runs {
		ref := orchapi.RunRef{IssueID: run.IssueID, RunID: run.RunID}
		deleteResult, err := api.DeleteRun(ctx, ref, deleteOpts)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s#%s: %v", run.IssueID, run.RunID, err))
			if !globalOpts.Quiet {
				fmt.Fprintf(os.Stderr, "error deleting %s#%s: %v\n", run.IssueID, run.RunID, err)
			}
		} else {
			result.Deleted = append(result.Deleted, deletedRun{
				IssueID:         deleteResult.IssueID,
				RunID:           deleteResult.RunID,
				ShortID:         deleteResult.ShortID,
				WorktreeRemoved: deleteResult.WorktreeRemoved,
				BranchRemoved:   deleteResult.BranchRemoved,
				SessionKilled:   deleteResult.SessionKilled,
			})
			if !globalOpts.Quiet && !globalOpts.JSON {
				fmt.Printf("deleted: %s#%s\n", run.IssueID, run.RunID)
			}
		}
	}

	if globalOpts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	if len(result.Errors) > 0 {
		os.Exit(ExitInternalError)
	}

	return nil
}

func confirmDelete(count int) bool {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Delete %d run(s)? [y/N] ", count)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}
