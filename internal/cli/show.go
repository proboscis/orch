package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/s22625/orch/internal/model"
	"github.com/spf13/cobra"
)

type showOptions struct {
	Tail       int
	Questions  bool
	EventsOnly bool
}

func newShowCmd() *cobra.Command {
	opts := &showOptions{}

	cmd := &cobra.Command{
		Use:   "show RUN_REF",
		Short: "Show run details",
		Long: `Show details for a run including events, unanswered questions, and artifacts.

RUN_REF can be ISSUE_ID#RUN_ID or just ISSUE_ID (for latest run).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShow(args[0], opts)
		},
	}

	cmd.Flags().IntVar(&opts.Tail, "tail", 80, "Number of events to show")
	cmd.Flags().BoolVar(&opts.Questions, "questions", false, "Show only unanswered questions")
	cmd.Flags().BoolVar(&opts.EventsOnly, "events-only", false, "Show only events")

	return cmd
}

func runShow(refStr string, opts *showOptions) error {
	st, err := getStore()
	if err != nil {
		return err
	}

	// Resolve by short ID or run ref
	run, err := resolveRun(st, refStr)
	if err != nil {
		os.Exit(ExitRunNotFound)
		return err
	}

	if globalOpts.JSON {
		return showJSON(run, opts)
	}

	return showHuman(run, opts)
}

func showJSON(run *model.Run, opts *showOptions) error {
	type eventOutput struct {
		Timestamp string            `json:"timestamp"`
		Type      string            `json:"type"`
		Name      string            `json:"name"`
		Attrs     map[string]string `json:"attrs,omitempty"`
	}

	type questionOutput struct {
		ID       string `json:"id"`
		Text     string `json:"text"`
		Choices  string `json:"choices,omitempty"`
		Severity string `json:"severity,omitempty"`
	}

	output := struct {
		OK           bool             `json:"ok"`
		IssueID      string           `json:"issue_id"`
		RunID        string           `json:"run_id"`
		Status       string           `json:"status"`
		Branch       string           `json:"branch,omitempty"`
		WorktreePath string           `json:"worktree_path,omitempty"`
		TmuxSession  string           `json:"tmux_session,omitempty"`
		PRUrl        string           `json:"pr_url,omitempty"`
		Events       []eventOutput    `json:"events,omitempty"`
		Questions    []questionOutput `json:"unanswered_questions,omitempty"`
	}{
		OK:           true,
		IssueID:      run.IssueID,
		RunID:        run.RunID,
		Status:       string(run.Status),
		Branch:       run.Branch,
		WorktreePath: run.WorktreePath,
		TmuxSession:  run.TmuxSession,
		PRUrl:        run.PRUrl,
	}

	// Add events (tail)
	events := run.Events
	if opts.Tail > 0 && len(events) > opts.Tail {
		events = events[len(events)-opts.Tail:]
	}

	for _, e := range events {
		output.Events = append(output.Events, eventOutput{
			Timestamp: e.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
			Type:      string(e.Type),
			Name:      e.Name,
			Attrs:     e.Attrs,
		})
	}

	// Add unanswered questions
	for _, q := range run.UnansweredQuestions() {
		output.Questions = append(output.Questions, questionOutput{
			ID:       q.Name,
			Text:     q.Attrs["text"],
			Choices:  q.Attrs["choices"],
			Severity: q.Attrs["severity"],
		})
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

func showHuman(run *model.Run, opts *showOptions) error {
	// Header
	fmt.Printf("Run: %s#%s\n", run.IssueID, run.RunID)
	fmt.Printf("Status: %s", colorStatus(run.Status))
	fmt.Println()
	fmt.Println(strings.Repeat("-", 60))

	// Artifacts
	if !opts.EventsOnly && !opts.Questions {
		if run.Branch != "" {
			fmt.Printf("Branch:   %s\n", run.Branch)
		}
		if run.WorktreePath != "" {
			fmt.Printf("Worktree: %s\n", run.WorktreePath)
		}
		if run.TmuxSession != "" {
			fmt.Printf("Session:  %s\n", run.TmuxSession)
		}
		if run.PRUrl != "" {
			fmt.Printf("PR:       %s\n", run.PRUrl)
		}
		fmt.Println()
	}

	// Unanswered questions
	questions := run.UnansweredQuestions()
	if opts.Questions || (!opts.EventsOnly && len(questions) > 0) {
		if len(questions) > 0 {
			fmt.Println("Unanswered Questions:")
			for _, q := range questions {
				fmt.Printf("  [%s] %s\n", q.Name, q.Attrs["text"])
				if choices := q.Attrs["choices"]; choices != "" {
					fmt.Printf("         Choices: %s\n", choices)
				}
			}
			fmt.Println()
		} else if opts.Questions {
			fmt.Println("No unanswered questions")
		}

		if opts.Questions {
			return nil
		}
	}

	// Events
	if !opts.Questions {
		fmt.Println("Events:")
		events := run.Events
		if opts.Tail > 0 && len(events) > opts.Tail {
			fmt.Printf("  ... (%d earlier events not shown)\n", len(events)-opts.Tail)
			events = events[len(events)-opts.Tail:]
		}

		for _, e := range events {
			ts := e.Timestamp.Format("15:04:05")
			fmt.Printf("  %s %s | %s", ts, e.Type, e.Name)
			for k, v := range e.Attrs {
				if len(v) > 50 {
					v = v[:47] + "..."
				}
				fmt.Printf(" %s=%s", k, v)
			}
			fmt.Println()
		}
	}

	return nil
}
