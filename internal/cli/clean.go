package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/orchapi"
	"github.com/spf13/cobra"
)

type cleanOptions struct {
	All       bool
	OlderThan string
	Status    string
	Force     bool
	DryRun    bool
}

type cleanDeps struct {
	getAPI func() (orchapi.OrchAPI, error)
}

type cleanResult struct {
	Cleaned []cleanedRun      `json:"cleaned"`
	Skipped []cleanSkippedRun `json:"skipped,omitempty"`
	Errors  []string          `json:"errors,omitempty"`
}

type cleanedRun struct {
	IssueID      string `json:"issue_id"`
	RunID        string `json:"run_id"`
	ShortID      string `json:"short_id"`
	WorktreePath string `json:"worktree_path,omitempty"`
}

type cleanSkippedRun struct {
	IssueID string `json:"issue_id"`
	RunID   string `json:"run_id"`
	ShortID string `json:"short_id"`
	Reason  string `json:"reason"`
}

func defaultCleanDeps() *cleanDeps {
	return &cleanDeps{getAPI: getAPI}
}

func newCleanCmd() *cobra.Command {
	opts := &cleanOptions{}

	cmd := &cobra.Command{
		Use:   "clean [RUN_REF | ISSUE_ID]",
		Short: "Remove worktrees for stopped runs",
		Long: `Remove run worktrees while preserving run history.

If given a specific RUN_REF (e.g., issue#run or short ID), cleans that run's worktree.
If given --all with no ISSUE_ID, cleans matching runs across all issues.
If given an ISSUE_ID with --all, cleans matching runs for that issue.
If given --older-than without an argument, cleans matching runs older than the specified duration.

By default, bulk cleanup targets failed and canceled runs only.
Use --status to include done runs explicitly.
Use --dry-run to see what would be cleaned without making changes.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCleanCommand(context.Background(), args, opts, defaultCleanDeps())
		},
	}

	cmd.Flags().BoolVar(&opts.All, "all", false, "Clean all matching runs, or all matching runs for the specified issue")
	cmd.Flags().StringVar(&opts.OlderThan, "older-than", "", "Clean runs older than duration (e.g., 7d, 2w, 1m)")
	cmd.Flags().StringVar(&opts.Status, "status", "", "Only clean runs with specific status (failed/canceled/done)")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be cleaned without cleaning")

	return cmd
}

func runCleanCommand(ctx context.Context, args []string, opts *cleanOptions, deps *cleanDeps) error {
	if opts.OlderThan != "" && len(args) > 0 && !opts.All {
		return fmt.Errorf("--older-than cannot be used with a specific run reference")
	}

	if opts.OlderThan != "" && len(args) == 0 {
		return runCleanByAge(ctx, opts, deps)
	}

	if len(args) == 0 && !opts.All {
		return fmt.Errorf("RUN_REF required (or use --older-than)")
	}

	if len(args) == 0 && opts.All {
		return runCleanAll(ctx, opts, deps)
	}

	return runClean(ctx, args[0], opts, deps)
}

func runClean(ctx context.Context, refStr string, opts *cleanOptions, deps *cleanDeps) error {
	api, err := deps.getAPI()
	if err != nil {
		return err
	}

	ref, err := orchapi.ParseRunRef(refStr)
	if err != nil {
		return err
	}

	if opts.All {
		return cleanIssueRuns(ctx, api, ref.IssueID, opts)
	}
	if ref.IsLatest() {
		return cleanLatestIssueRun(ctx, api, ref.IssueID, opts)
	}

	run, err := api.ResolveRun(ctx, ref)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run not found: %s\n", refStr)
		os.Exit(ExitRunNotFound)
		return err
	}

	return cleanRuns(ctx, api, []*orchapi.Run{run}, opts)
}

func runCleanByAge(ctx context.Context, opts *cleanOptions, deps *cleanDeps) error {
	api, err := deps.getAPI()
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

	statuses, err := cleanStatusFilter(opts.Status)
	if err != nil {
		return err
	}
	filter.Status = statuses

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

	return cleanRuns(ctx, api, result.Runs, opts)
}

func runCleanAll(ctx context.Context, opts *cleanOptions, deps *cleanDeps) error {
	api, err := deps.getAPI()
	if err != nil {
		return err
	}

	statuses, err := cleanStatusFilter(opts.Status)
	if err != nil {
		return err
	}

	result, err := api.ListRuns(ctx, &orchapi.ListRunsFilter{
		Status: statuses,
	})
	if err != nil {
		return err
	}

	if len(result.Runs) == 0 {
		if !globalOpts.Quiet {
			fmt.Println("No runs found")
		}
		return nil
	}

	return cleanRuns(ctx, api, result.Runs, opts)
}

func cleanIssueRuns(ctx context.Context, api orchapi.OrchAPI, issueID model.IssueID, opts *cleanOptions) error {
	filter := &orchapi.ListRunsFilter{
		IssueID: issueID,
	}

	statuses, err := cleanStatusFilter(opts.Status)
	if err != nil {
		return err
	}
	filter.Status = statuses

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

	return cleanRuns(ctx, api, result.Runs, opts)
}

func cleanLatestIssueRun(ctx context.Context, api orchapi.OrchAPI, issueID model.IssueID, opts *cleanOptions) error {
	statuses, err := cleanStatusFilter(opts.Status)
	if err != nil {
		return err
	}

	result, err := api.ListRuns(ctx, &orchapi.ListRunsFilter{
		IssueID: issueID,
		Status:  statuses,
		Limit:   1,
	})
	if err != nil {
		return err
	}

	if len(result.Runs) == 0 {
		if !globalOpts.Quiet {
			fmt.Printf("No runs found for issue: %s\n", issueID)
		}
		return nil
	}

	return cleanRuns(ctx, api, []*orchapi.Run{result.Runs[0]}, opts)
}

func cleanStatusFilter(input string) ([]orchapi.RunStatus, error) {
	if strings.TrimSpace(input) == "" {
		return []orchapi.RunStatus{
			orchapi.RunStatusFailed,
			orchapi.RunStatusCanceled,
		}, nil
	}

	parts := strings.Split(input, ",")
	statuses := make([]orchapi.RunStatus, 0, len(parts))
	seen := make(map[orchapi.RunStatus]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(strings.ToLower(part))
		if part == "" {
			continue
		}
		if part == "stopped" || part == "cancelled" {
			part = string(orchapi.RunStatusCanceled)
		}

		status, err := orchapi.NormalizeRunStatus(part)
		if err != nil {
			return nil, err
		}
		switch status {
		case orchapi.RunStatusDone, orchapi.RunStatusFailed, orchapi.RunStatusCanceled:
			if _, ok := seen[status]; ok {
				continue
			}
			seen[status] = struct{}{}
			statuses = append(statuses, status)
		case orchapi.RunStatusRunning, orchapi.RunStatusBooting, orchapi.RunStatusWaiting, orchapi.RunStatusRateLimited, orchapi.RunStatusQueued, orchapi.RunStatusPROpen, orchapi.RunStatusUnknown:
			return nil, fmt.Errorf("cannot clean %s runs (use 'orch stop' first)", status)
		default:
			return nil, fmt.Errorf("unknown status: %s", part)
		}
	}

	if len(statuses) == 0 {
		return nil, fmt.Errorf("no valid statuses specified")
	}

	return statuses, nil
}

func cleanRuns(ctx context.Context, api orchapi.OrchAPI, runs []*orchapi.Run, opts *cleanOptions) error {
	if len(runs) == 0 {
		if !globalOpts.Quiet {
			fmt.Println("No matching runs to clean")
		}
		return nil
	}

	if !globalOpts.Quiet || opts.DryRun {
		action := "Cleaning"
		if opts.DryRun {
			action = "Would clean"
		}
		fmt.Printf("%s %d run(s):\n", action, len(runs))
		for _, run := range runs {
			path := ""
			if run.WorktreePath != "" {
				path = " " + run.WorktreePath
			}
			fmt.Printf("  %s#%s [%s] %s%s\n", run.IssueID, run.RunID, run.ShortID, run.Status, path)
		}
	}

	if opts.DryRun {
		return nil
	}

	if !opts.Force && !confirmClean(len(runs)) {
		fmt.Println("Aborted")
		return nil
	}

	result := &cleanResult{
		Cleaned: make([]cleanedRun, 0, len(runs)),
	}

	for _, run := range runs {
		ref := orchapi.RunRef{IssueID: run.IssueID, RunID: run.RunID}
		cleaned, err := api.CleanRunWorktree(ctx, ref)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s#%s: %v", run.IssueID, run.RunID, err))
			if !globalOpts.Quiet {
				fmt.Fprintf(os.Stderr, "error cleaning %s#%s: %v\n", run.IssueID, run.RunID, err)
			}
			continue
		}

		if cleaned.Skipped {
			result.Skipped = append(result.Skipped, cleanSkippedRun{
				IssueID: string(cleaned.IssueID),
				RunID:   string(cleaned.RunID),
				ShortID: string(cleaned.ShortID),
				Reason:  cleaned.Reason,
			})
			if !globalOpts.Quiet && !globalOpts.JSON {
				fmt.Printf("skipped: %s#%s (%s)\n", cleaned.IssueID, cleaned.RunID, cleaned.Reason)
			}
			continue
		}

		result.Cleaned = append(result.Cleaned, cleanedRun{
			IssueID:      string(cleaned.IssueID),
			RunID:        string(cleaned.RunID),
			ShortID:      string(cleaned.ShortID),
			WorktreePath: cleaned.WorktreePath,
		})
		if !globalOpts.Quiet && !globalOpts.JSON {
			fmt.Printf("cleaned: %s#%s\n", cleaned.IssueID, cleaned.RunID)
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

func confirmClean(count int) bool {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Clean worktrees for %d run(s)? [y/N] ", count)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}
