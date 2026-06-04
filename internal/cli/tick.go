package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/s22625/orch/internal/orchapi"
	"github.com/spf13/cobra"
)

type tickOptions struct {
	All         bool
	OnlyWaiting bool
	Agent       string
	Max         int
}

func newTickCmd() *cobra.Command {
	opts := &tickOptions{}

	cmd := &cobra.Command{
		Use:   "tick [RUN_REF]",
		Short: "Resume waiting runs",
		Long: `Trigger waiting runs to resume if their questions are answered.

With --all, processes all waiting runs. Otherwise, processes a single run.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var refStr string
			if len(args) > 0 {
				refStr = args[0]
			}
			return runTick(refStr, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.All, "all", false, "Process all waiting runs")
	cmd.Flags().BoolVar(&opts.OnlyWaiting, "only-waiting", true, "Only process waiting runs (default when --all)")
	cmd.Flags().StringVar(&opts.Agent, "agent", "", "Agent to use for resumption")
	cmd.Flags().IntVar(&opts.Max, "max", 10, "Maximum runs to process with --all")

	return cmd
}

type tickResult struct {
	OK        bool         `json:"ok"`
	Processed []tickedRun  `json:"processed"`
	Skipped   []skippedRun `json:"skipped,omitempty"`
}

type tickedRun struct {
	IssueID string `json:"issue_id"`
	RunID   string `json:"run_id"`
	Status  string `json:"status"`
}

type skippedRun struct {
	IssueID string `json:"issue_id"`
	RunID   string `json:"run_id"`
	Reason  string `json:"reason"`
}

func runTick(refStr string, opts *tickOptions) error {
	ctx := context.Background()

	api, err := getAPI()
	if err != nil {
		return err
	}

	var runs []*orchapi.Run

	if opts.All {
		filter := &orchapi.ListRunsFilter{
			Status: []orchapi.RunStatus{orchapi.RunStatusWaiting, orchapi.RunStatusRateLimited},
			Limit:  opts.Max,
		}
		listResult, err := api.ListRuns(ctx, filter)
		if err != nil {
			return err
		}
		runs = listResult.Runs
	} else {
		if refStr == "" {
			return fmt.Errorf("RUN_REF required (or use --all)")
		}

		run, err := resolveRunAPI(ctx, api, refStr)
		if err != nil {
			os.Exit(ExitRunNotFound)
			return err
		}
		runs = []*orchapi.Run{run}
	}

	result := &tickResult{
		OK:        true,
		Processed: []tickedRun{},
		Skipped:   []skippedRun{},
	}

	for _, run := range runs {
		// Only check blocked status for single-run case; --all already filters at daemon level
		if !opts.All && opts.OnlyWaiting && run.Status != orchapi.RunStatusWaiting && run.Status != orchapi.RunStatusRateLimited {
			result.Skipped = append(result.Skipped, skippedRun{
				IssueID: string(run.IssueID),
				RunID:   string(run.RunID),
				Reason:  fmt.Sprintf("status is %s, not waiting", run.Status),
			})
			continue
		}

		if err := resumeRun(ctx, api, run, opts.Agent); err != nil {
			result.Skipped = append(result.Skipped, skippedRun{
				IssueID: string(run.IssueID),
				RunID:   string(run.RunID),
				Reason:  err.Error(),
			})
			continue
		}

		result.Processed = append(result.Processed, tickedRun{
			IssueID: string(run.IssueID),
			RunID:   string(run.RunID),
			Status:  string(orchapi.RunStatusRunning),
		})
	}

	if globalOpts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	if len(result.Processed) > 0 {
		fmt.Printf("Resumed %d run(s):\n", len(result.Processed))
		for _, r := range result.Processed {
			fmt.Printf("  %s#%s\n", r.IssueID, r.RunID)
		}
	}

	if len(result.Skipped) > 0 && !globalOpts.Quiet {
		fmt.Printf("Skipped %d run(s):\n", len(result.Skipped))
		for _, r := range result.Skipped {
			fmt.Printf("  %s#%s: %s\n", r.IssueID, r.RunID, r.Reason)
		}
	}

	if len(result.Processed) == 0 && len(result.Skipped) == 0 {
		if !globalOpts.Quiet {
			fmt.Println("No runs to process")
		}
	}

	return nil
}

func resumeRun(ctx context.Context, api orchapi.OrchAPI, run *orchapi.Run, agentType string) error {
	// Record the resume event. The daemon will handle session management and agent launching.
	// The daemon's event processor will detect the resume event and take appropriate action.
	_, err := api.AppendEvent(ctx, run.Ref(), &orchapi.Event{
		Type: "resume",
		Name: "tick",
		Attrs: map[string]string{
			"agent": agentType,
		},
	})
	return err
}

func buildResumePrompt(issue *orchapi.Issue, run *orchapi.Run) string {
	prompt := fmt.Sprintf("Resuming work on issue: %s\n\n", issue.ID)
	if issue.Title != "" {
		prompt += fmt.Sprintf("Title: %s\n\n", issue.Title)
	}
	if run.Status == orchapi.RunStatusRateLimited {
		prompt += "The previous session was rate limited by API usage limits.\n"
	} else {
		prompt += "The previous session was waiting for input.\n"
	}
	prompt += "Please continue from where you left off.\n"
	return prompt
}
