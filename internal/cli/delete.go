package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
	"github.com/s22625/orch/internal/tmux"
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

// parseDuration parses a duration string like "7d", "2w", "1m" into a time.Duration
func parseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}

	// Match number followed by unit
	re := regexp.MustCompile(`^(\d+)([dwmDWM])$`)
	matches := re.FindStringSubmatch(s)
	if matches == nil {
		return 0, fmt.Errorf("invalid duration format: %s (use 7d, 2w, or 1m)", s)
	}

	value, _ := strconv.Atoi(matches[1])
	unit := strings.ToLower(matches[2])

	switch unit {
	case "d":
		return time.Duration(value) * 24 * time.Hour, nil
	case "w":
		return time.Duration(value) * 7 * 24 * time.Hour, nil
	case "m":
		return time.Duration(value) * 30 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown duration unit: %s", unit)
	}
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
	st, err := getStore()
	if err != nil {
		return err
	}

	// Try as short ID first
	if shortIDRegex.MatchString(refStr) {
		run, err := st.GetRunByShortID(refStr)
		if err == nil {
			return deleteRuns(st, []*model.Run{run}, opts)
		}
		// Fall through to try as regular ref
	}

	ref, err := model.ParseRunRef(refStr)
	if err != nil {
		return err
	}

	// If --all flag is set or no specific run ID, delete all runs for issue
	if ref.IsLatest() || opts.All {
		return deleteIssueRuns(st, ref.IssueID, opts)
	}

	// Delete specific run
	run, err := st.GetRun(ref)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run not found: %s\n", refStr)
		os.Exit(ExitRunNotFound)
		return err
	}

	return deleteRuns(st, []*model.Run{run}, opts)
}

func deleteIssueRuns(st store.Store, issueID string, opts *deleteOptions) error {
	// Build filter
	filter := &store.ListRunsFilter{
		IssueID: issueID,
	}

	// Apply status filter
	if opts.Status != "" {
		statuses, err := parseStatus(opts.Status)
		if err != nil {
			return err
		}
		filter.Status = statuses
	}

	runs, err := st.ListRuns(filter)
	if err != nil {
		return err
	}

	if len(runs) == 0 {
		if !globalOpts.Quiet {
			fmt.Printf("No runs found for issue: %s\n", issueID)
		}
		return nil
	}

	// Apply age filter if specified
	if opts.OlderThan != "" {
		runs, err = filterByAge(runs, opts.OlderThan)
		if err != nil {
			return err
		}
	}

	return deleteRuns(st, runs, opts)
}

func runDeleteByAge(opts *deleteOptions) error {
	st, err := getStore()
	if err != nil {
		return err
	}

	// Build filter
	filter := &store.ListRunsFilter{}

	// Apply status filter
	if opts.Status != "" {
		statuses, err := parseStatus(opts.Status)
		if err != nil {
			return err
		}
		filter.Status = statuses
	}

	runs, err := st.ListRuns(filter)
	if err != nil {
		return err
	}

	if len(runs) == 0 {
		if !globalOpts.Quiet {
			fmt.Println("No runs found")
		}
		return nil
	}

	// Filter by age
	runs, err = filterByAge(runs, opts.OlderThan)
	if err != nil {
		return err
	}

	return deleteRuns(st, runs, opts)
}

func filterByAge(runs []*model.Run, olderThan string) ([]*model.Run, error) {
	duration, err := parseDuration(olderThan)
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().Add(-duration)
	var filtered []*model.Run
	for _, run := range runs {
		if run.UpdatedAt.Before(cutoff) {
			filtered = append(filtered, run)
		}
	}
	return filtered, nil
}

func deleteRuns(st store.Store, runs []*model.Run, opts *deleteOptions) error {
	if len(runs) == 0 {
		if !globalOpts.Quiet {
			fmt.Println("No matching runs to delete")
		}
		return nil
	}

	// Check for active runs that shouldn't be deleted
	var activeRuns []*model.Run
	var deletableRuns []*model.Run
	for _, run := range runs {
		if run.Status == model.StatusRunning || run.Status == model.StatusBooting || run.Status == model.StatusBlocked || run.Status == model.StatusBlockedAPI {
			activeRuns = append(activeRuns, run)
		} else {
			deletableRuns = append(deletableRuns, run)
		}
	}

	if len(activeRuns) > 0 && !opts.Force {
		fmt.Fprintf(os.Stderr, "Skipping %d active run(s) (use 'orch stop' first or --force to delete anyway):\n", len(activeRuns))
		for _, run := range activeRuns {
			fmt.Fprintf(os.Stderr, "  %s#%s (%s)\n", run.IssueID, run.RunID, run.Status)
		}
		runs = deletableRuns
		if len(runs) == 0 {
			return nil
		}
	} else if opts.Force {
		// Force includes active runs
		runs = append(deletableRuns, activeRuns...)
	} else {
		runs = deletableRuns
	}

	// Show what will be deleted
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
			if run.TmuxSession != "" && tmux.HasSession(run.TmuxSession) {
				extras = append(extras, "session")
			}
			extraStr := ""
			if len(extras) > 0 {
				extraStr = fmt.Sprintf(" (+%s)", strings.Join(extras, ", "))
			}
			fmt.Printf("  %s#%s [%s] %s%s\n", run.IssueID, run.RunID, run.ShortID(), run.Status, extraStr)
		}
	}

	// Dry run stops here
	if opts.DryRun {
		return nil
	}

	// Confirmation prompt
	if !opts.Force && !confirmDelete(len(runs)) {
		fmt.Println("Aborted")
		return nil
	}

	// Perform deletion
	result := &deleteResult{
		Deleted: make([]deletedRun, 0, len(runs)),
	}

	for _, run := range runs {
		deleted, err := performDelete(st, run, opts)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s#%s: %v", run.IssueID, run.RunID, err))
			if !globalOpts.Quiet {
				fmt.Fprintf(os.Stderr, "error deleting %s#%s: %v\n", run.IssueID, run.RunID, err)
			}
		} else {
			result.Deleted = append(result.Deleted, *deleted)
			if !globalOpts.Quiet && !globalOpts.JSON {
				fmt.Printf("deleted: %s#%s\n", run.IssueID, run.RunID)
			}
		}
	}

	// Output
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

func performDelete(st store.Store, run *model.Run, opts *deleteOptions) (*deletedRun, error) {
	result := &deletedRun{
		IssueID: run.IssueID,
		RunID:   run.RunID,
		ShortID: run.ShortID(),
	}

	// 1. Kill tmux session if running
	sessionName := run.TmuxSession
	if sessionName == "" {
		sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
	}
	if tmux.HasSession(sessionName) {
		if err := tmux.KillSession(sessionName); err != nil {
			// Log warning but continue
			fmt.Fprintf(os.Stderr, "warning: failed to kill session %s: %v\n", sessionName, err)
		} else {
			result.SessionKilled = true
		}
	}

	// 2. Remove worktree if requested
	if opts.WithWorktree && run.WorktreePath != "" {
		repoRoot, err := git.FindRepoRoot("")
		if err == nil {
			if err := git.RemoveWorktree(repoRoot, run.WorktreePath); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to remove worktree %s: %v\n", run.WorktreePath, err)
			} else {
				result.WorktreeRemoved = true
			}
		}
	}

	// 3. Remove branch if requested
	if opts.WithBranch && run.Branch != "" {
		repoRoot, err := git.FindRepoRoot("")
		if err == nil {
			// Delete branch (force delete in case it's not fully merged)
			cmd := exec.Command("git", "-C", repoRoot, "branch", "-D", run.Branch)
			if err := cmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to delete branch %s: %v\n", run.Branch, err)
			} else {
				result.BranchRemoved = true
			}
		}
	}

	// 4. Remove run document
	if err := os.Remove(run.Path); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to remove run document: %w", err)
		}
	}

	// 5. Remove log directory if exists
	logDir := strings.TrimSuffix(run.Path, ".md") + ".log"
	if info, err := os.Stat(logDir); err == nil && info.IsDir() {
		if err := os.RemoveAll(logDir); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to remove log directory %s: %v\n", logDir, err)
		}
	}

	return result, nil
}
