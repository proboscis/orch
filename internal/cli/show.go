package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/orchapi"
	"github.com/spf13/cobra"
)

type showOptions struct {
	Tail       int
	EventsOnly bool
}

func newShowCmd() *cobra.Command {
	opts := &showOptions{}

	cmd := &cobra.Command{
		Use:   "show RUN_REF",
		Short: "Show run details",
		Long: `Show details for a run including events and artifacts.

RUN_REF can be ISSUE_ID#RUN_ID or just ISSUE_ID (for latest run).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShow(args[0], opts)
		},
	}

	cmd.Flags().IntVar(&opts.Tail, "tail", 80, "Number of events to show")
	cmd.Flags().BoolVar(&opts.EventsOnly, "events-only", false, "Show only events")

	return cmd
}

func runShow(refStr string, opts *showOptions) error {
	api, err := getAPIForListing()
	if err != nil {
		return err
	}

	ctx := context.Background()
	run, err := resolveRunAPI(ctx, api, refStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run not found: %s\n", refStr)
		os.Exit(ExitRunNotFound)
		return err
	}

	if globalOpts.JSON {
		return showJSON(run, opts)
	}

	return showHuman(run, opts)
}

func showJSON(run *orchapi.Run, opts *showOptions) error {
	type eventOutput struct {
		Timestamp string            `json:"timestamp"`
		Type      string            `json:"type"`
		Name      string            `json:"name"`
		Attrs     map[string]string `json:"attrs,omitempty"`
	}

	output := struct {
		OK            bool          `json:"ok"`
		IssueID       string        `json:"issue_id"`
		RunID         string        `json:"run_id"`
		Status        string        `json:"status"`
		Phase         string        `json:"phase,omitempty"`
		Agent         string        `json:"agent,omitempty"`
		Profile       string        `json:"profile,omitempty"`
		ContinuedFrom string        `json:"continued_from,omitempty"`
		Branch        string        `json:"branch,omitempty"`
		WorktreePath  string        `json:"worktree_path,omitempty"`
		Target        string        `json:"target,omitempty"`
		TargetHost    string        `json:"target_host,omitempty"`
		SessionName   string        `json:"session_name,omitempty"`
		Multiplexer   string        `json:"multiplexer,omitempty"`
		PRUrl         string        `json:"pr_url,omitempty"`
		Events        []eventOutput `json:"events,omitempty"`
	}{
		OK:            true,
		IssueID:       string(run.IssueID),
		RunID:         string(run.RunID),
		Status:        string(run.Status),
		Phase:         string(run.Phase),
		Agent:         run.Agent,
		Profile:       run.Profile,
		ContinuedFrom: run.ContinuedFrom,
		Branch:        run.Branch,
		WorktreePath:  run.WorktreePath,
		Target:        run.Target,
		TargetHost:    run.TargetHost,
		SessionName:   run.SessionName,
		Multiplexer:   string(run.Multiplexer),
		PRUrl:         run.PRUrl,
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

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

func showHuman(run *orchapi.Run, opts *showOptions) error {
	// Header
	fmt.Printf("Run: %s#%s\n", run.IssueID, run.RunID)
	fmt.Printf("Status: %s", colorStatus(model.NormalizeStatus(string(run.Status))))
	fmt.Println()
	fmt.Println(strings.Repeat("-", 60))

	// Artifacts
	if !opts.EventsOnly {
		if run.Agent != "" {
			fmt.Printf("Agent:    %s\n", run.Agent)
		}
		if run.Profile != "" {
			fmt.Printf("Profile:  %s\n", run.Profile)
		}
		if run.Branch != "" {
			fmt.Printf("Branch:   %s\n", run.Branch)
		}
		if run.WorktreePath != "" {
			fmt.Printf("Worktree: %s\n", run.WorktreePath)
		}
		if run.Target != "" {
			fmt.Printf("Target:   %s\n", run.Target)
			if run.TargetHost != "" {
				fmt.Printf("Host:     %s\n", run.TargetHost)
			}
		}
		if run.ContinuedFrom != "" {
			fmt.Printf("Continued From: %s\n", run.ContinuedFrom)
		}
		if run.SessionName != "" {
			fmt.Printf("Session:  %s\n", run.SessionName)
		}
		if run.PRUrl != "" {
			fmt.Printf("PR:       %s\n", run.PRUrl)
		}
		fmt.Println()
	}

	// Events
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

	return nil
}
