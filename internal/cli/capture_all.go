package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
	"github.com/s22625/orch/internal/tmux"
	"github.com/spf13/cobra"
)

type captureAllOptions struct {
	Lines int
}

var captureAllHasSession = tmux.HasSession
var captureAllCapturePane = tmux.CapturePane

func newCaptureAllCmd() *cobra.Command {
	opts := &captureAllOptions{}

	cmd := &cobra.Command{
		Use:   "capture-all",
		Short: "Capture output from all running agents",
		Long: `Capture the latest output from all running agents.

Returns captured text agent-by-agent with status headers.
Useful for monitoring all active agents at once.

Examples:
  # Capture last 100 lines (default) from all running agents
  orch capture-all

  # Capture last 500 lines
  orch capture-all --lines 500

  # Output as JSON for scripting
  orch capture-all --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCaptureAll(opts)
		},
	}

	cmd.Flags().IntVar(&opts.Lines, "lines", 100, "Number of lines to capture per agent")

	return cmd
}

type captureAllItem struct {
	OK          bool   `json:"ok"`
	IssueID     string `json:"issue_id"`
	RunID       string `json:"run_id"`
	Status      string `json:"status"`
	TmuxSession string `json:"tmux_session"`
	Lines       int    `json:"lines"`
	Content     string `json:"content,omitempty"`
	Error       string `json:"error,omitempty"`
}

func runCaptureAll(opts *captureAllOptions) error {
	st, err := getStore()
	if err != nil {
		return err
	}

	runs, err := st.ListRuns(&store.ListRunsFilter{
		Status: captureAllStatuses(),
	})
	if err != nil {
		return err
	}

	if len(runs) == 0 {
		if globalOpts.JSON {
			return outputCaptureAllJSON([]captureAllItem{}, true)
		}
		if !globalOpts.Quiet {
			fmt.Println("No running agents found")
		}
		return nil
	}

	items := make([]captureAllItem, 0, len(runs))
	overallOK := true
	for _, run := range runs {
		item := captureAllItemForRun(run, opts.Lines)
		if !item.OK {
			overallOK = false
		}
		items = append(items, item)
	}

	if globalOpts.JSON {
		return outputCaptureAllJSON(items, overallOK)
	}

	outputCaptureAllPlain(items)
	return nil
}

func captureAllStatuses() []model.Status {
	return []model.Status{
		model.StatusRunning,
		model.StatusBooting,
		model.StatusBlocked,
		model.StatusBlockedAPI,
		model.StatusPROpen,
		model.StatusUnknown,
	}
}

func captureAllItemForRun(run *model.Run, lines int) captureAllItem {
	sessionName := run.TmuxSession
	if sessionName == "" {
		sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
	}

	item := captureAllItem{
		IssueID:     run.IssueID,
		RunID:       run.RunID,
		Status:      string(run.Status),
		TmuxSession: sessionName,
		Lines:       lines,
	}

	if !captureAllHasSession(sessionName) {
		item.Error = fmt.Sprintf("tmux session %q not found (run may not be active)", sessionName)
		return item
	}

	content, err := captureAllCapturePane(sessionName, lines)
	if err != nil {
		item.Error = fmt.Sprintf("failed to capture pane: %v", err)
		return item
	}

	item.OK = true
	item.Content = content
	return item
}

func outputCaptureAllJSON(items []captureAllItem, ok bool) error {
	output := struct {
		OK    bool             `json:"ok"`
		Items []captureAllItem `json:"items"`
	}{
		OK:    ok,
		Items: items,
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

func outputCaptureAllPlain(items []captureAllItem) {
	for i, item := range items {
		if i > 0 {
			fmt.Println()
		}
		fmt.Printf("=== %s#%s [%s] ===\n", item.IssueID, item.RunID, item.Status)
		if item.OK {
			fmt.Print(item.Content)
			if !strings.HasSuffix(item.Content, "\n") {
				fmt.Println()
			}
			continue
		}
		fmt.Printf("error: %s\n", item.Error)
	}
}
