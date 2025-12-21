package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
	"github.com/s22625/orch/internal/tmux"
	"github.com/spf13/cobra"
)

type tickOptions struct {
	All         bool
	OnlyBlocked bool
	Agent       string
	Max         int
}

func newTickCmd() *cobra.Command {
	opts := &tickOptions{}

	cmd := &cobra.Command{
		Use:   "tick [RUN_REF]",
		Short: "Resume blocked runs",
		Long: `Trigger blocked runs to resume if their questions are answered.

With --all, processes all blocked runs. Otherwise, processes a single run.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var refStr string
			if len(args) > 0 {
				refStr = args[0]
			}
			return runTick(refStr, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.All, "all", false, "Process all blocked runs")
	cmd.Flags().BoolVar(&opts.OnlyBlocked, "only-blocked", true, "Only process blocked runs (default when --all)")
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
	st, err := getStore()
	if err != nil {
		return err
	}

	var runs []*model.Run

	if opts.All {
		// Get all blocked runs
		filter := &store.ListRunsFilter{
			Status: []model.Status{model.StatusBlocked},
			Limit:  opts.Max,
		}
		runs, err = st.ListRuns(filter)
		if err != nil {
			return err
		}
	} else {
		if refStr == "" {
			return fmt.Errorf("RUN_REF required (or use --all)")
		}

		// Resolve by short ID or run ref
		run, err := resolveRun(st, refStr)
		if err != nil {
			os.Exit(ExitRunNotFound)
			return err
		}
		runs = []*model.Run{run}
	}

	result := &tickResult{
		OK:        true,
		Processed: []tickedRun{},
		Skipped:   []skippedRun{},
	}

	for _, run := range runs {
		// Check if run is blocked (when processing single run)
		if opts.OnlyBlocked && run.Status != model.StatusBlocked {
			result.Skipped = append(result.Skipped, skippedRun{
				IssueID: run.IssueID,
				RunID:   run.RunID,
				Reason:  fmt.Sprintf("status is %s, not blocked", run.Status),
			})
			continue
		}

		// Check for unanswered questions
		unanswered := run.UnansweredQuestions()
		if len(unanswered) > 0 {
			result.Skipped = append(result.Skipped, skippedRun{
				IssueID: run.IssueID,
				RunID:   run.RunID,
				Reason:  fmt.Sprintf("%d unanswered questions", len(unanswered)),
			})
			continue
		}

		// Resume the run
		if err := resumeRun(st, run, opts.Agent); err != nil {
			result.Skipped = append(result.Skipped, skippedRun{
				IssueID: run.IssueID,
				RunID:   run.RunID,
				Reason:  err.Error(),
			})
			continue
		}

		result.Processed = append(result.Processed, tickedRun{
			IssueID: run.IssueID,
			RunID:   run.RunID,
			Status:  string(model.StatusRunning),
		})
	}

	// Output
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

func resumeRun(st store.Store, run *model.Run, agentType string) error {
	// Determine agent type
	if agentType == "" {
		agentType = "claude" // default
	}

	aType, err := agent.ParseAgentType(agentType)
	if err != nil {
		return err
	}

	adapter, err := agent.GetAdapter(aType)
	if err != nil {
		return err
	}

	// Build launch config for resumption
	issue, err := st.ResolveIssue(run.IssueID)
	if err != nil {
		return err
	}

	launchCfg := &agent.LaunchConfig{
		Type:        aType,
		WorkDir:     run.WorktreePath,
		IssueID:     run.IssueID,
		RunID:       run.RunID,
		RunPath:     run.Path,
		VaultPath:   st.VaultPath(),
		Branch:      run.Branch,
		Prompt:      buildResumePrompt(issue, run),
		Resume:      true,
		SessionName: run.TmuxSession,
	}

	agentCmd, err := adapter.LaunchCommand(launchCfg)
	if err != nil {
		return err
	}

	// Check if session exists, create new window for resume
	sessionName := run.TmuxSession
	if sessionName == "" {
		sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
	}

	if tmux.HasSession(sessionName) {
		// Create new window in existing session
		err = tmux.NewWindow(sessionName, "resume", run.WorktreePath, agentCmd)
	} else {
		// Create new session
		err = tmux.NewSession(&tmux.SessionConfig{
			SessionName: sessionName,
			WorkDir:     run.WorktreePath,
			Command:     agentCmd,
			Env:         launchCfg.Env(),
		})
	}

	if err != nil {
		return fmt.Errorf("failed to resume agent: %w", err)
	}

	// Update status
	st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusRunning))

	return nil
}

func buildResumePrompt(issue *model.Issue, run *model.Run) string {
	prompt := fmt.Sprintf("Resuming work on issue: %s\n\n", issue.ID)
	if issue.Title != "" {
		prompt += fmt.Sprintf("Title: %s\n\n", issue.Title)
	}
	prompt += "The previous session was blocked but all questions have been answered.\n"
	prompt += "Please continue from where you left off.\n"
	return prompt
}
