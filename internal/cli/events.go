package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/s22625/orch/internal/orchapi"
	"github.com/spf13/cobra"
)

func newEventsCmd() *cobra.Command {
	var follow bool
	var issueFilter string
	var runFilter string

	cmd := &cobra.Command{
		Use:   "events",
		Short: "Stream run state transition events",
		Long: `Subscribe to run state transitions emitted by the daemon.

Each event is printed as a single JSON line on stdout. The stream stays
open until the client is interrupted (Ctrl-C) or the daemon disconnects.

Useful for building external integrations (status mirrors, custom
notifiers) that react to run state changes without polling.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !follow {
				return fmt.Errorf("orch events currently requires --follow; one-shot history is not implemented yet")
			}
			return runEventsStream(cmd.Context(), issueFilter, runFilter)
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Stream events as they occur (required)")
	cmd.Flags().StringVar(&issueFilter, "issue", "", "Only emit events for this issue ID")
	cmd.Flags().StringVar(&runFilter, "run", "", "Only emit events for this run ID")
	return cmd
}

func runEventsStream(ctx context.Context, issueFilter, runFilter string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	api, err := getAPIForListing()
	if err != nil {
		return err
	}

	filter := &orchapi.RunEventFilter{
		IssueID: strings.TrimSpace(issueFilter),
		RunID:   strings.TrimSpace(runFilter),
	}

	stream, err := api.StreamRunEvents(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to start event stream: %w", err)
	}
	defer stream.Close()

	encoder := json.NewEncoder(os.Stdout)
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-stream.Events():
			if !ok {
				if err := stream.Err(); err != nil {
					return fmt.Errorf("event stream closed: %w", err)
				}
				return nil
			}
			if err := encoder.Encode(eventToJSON(ev)); err != nil {
				return fmt.Errorf("failed to encode event: %w", err)
			}
		}
	}
}

type eventJSON struct {
	Timestamp string `json:"timestamp"`
	IssueID   string `json:"issue_id"`
	RunID     string `json:"run_id"`
	ShortID   string `json:"short_id"`
	From      string `json:"from"`
	To        string `json:"to"`
	Source    string `json:"source"`
	ProjectID string `json:"project_id,omitempty"`
}

func eventToJSON(ev *orchapi.RunEvent) *eventJSON {
	return &eventJSON{
		Timestamp: ev.Timestamp.UTC().Format(time.RFC3339Nano),
		IssueID:   ev.IssueID,
		RunID:     ev.RunID,
		ShortID:   ev.ShortID,
		From:      string(ev.From),
		To:        string(ev.To),
		Source:    ev.Source,
		ProjectID: ev.ProjectID,
	}
}
