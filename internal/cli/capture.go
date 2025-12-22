package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/tmux"
	"github.com/spf13/cobra"
)

type captureOptions struct {
	Lines int
}

func newCaptureCmd() *cobra.Command {
	opts := &captureOptions{}

	cmd := &cobra.Command{
		Use:   "capture <RUN_REF>",
		Short: "Capture output from a running agent",
		Long: `Capture the latest output from an agent's tmux pane.

Returns the captured text to stdout for programmatic consumption.
Useful for monitoring agent status or building automation workflows.

Examples:
  # Capture last 100 lines (default) from an agent
  orch capture orch-023#20231220-100000

  # Capture using short ID
  orch capture a3b4c5

  # Capture last 500 lines
  orch capture orch-023 --lines 500

  # Output as JSON for scripting
  orch capture orch-023 --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCapture(args[0], opts)
		},
	}

	cmd.Flags().IntVar(&opts.Lines, "lines", 100, "Number of lines to capture")

	return cmd
}

type captureResult struct {
	OK          bool   `json:"ok"`
	IssueID     string `json:"issue_id"`
	RunID       string `json:"run_id"`
	TmuxSession string `json:"tmux_session"`
	Lines       int    `json:"lines"`
	Content     string `json:"content"`
}

func runCapture(refStr string, opts *captureOptions) error {
	st, err := getStore()
	if err != nil {
		return err
	}

	// Resolve the run
	run, err := resolveRun(st, refStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(ExitRunNotFound)
		return err
	}

	// Get tmux session name
	sessionName := run.TmuxSession
	if sessionName == "" {
		sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
	}

	// Check if session exists
	if !tmux.HasSession(sessionName) {
		err := fmt.Errorf("tmux session %q not found (run may not be active)", sessionName)
		if globalOpts.JSON {
			result := map[string]interface{}{
				"ok":    false,
				"error": err.Error(),
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(result)
		} else {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		os.Exit(ExitTmuxError)
		return err
	}

	// Capture the pane content
	content, err := tmux.CapturePane(sessionName, opts.Lines)
	if err != nil {
		if globalOpts.JSON {
			result := map[string]interface{}{
				"ok":    false,
				"error": err.Error(),
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(result)
		} else {
			fmt.Fprintf(os.Stderr, "error: failed to capture pane: %v\n", err)
		}
		os.Exit(ExitTmuxError)
		return err
	}

	// Output result
	if globalOpts.JSON {
		result := &captureResult{
			OK:          true,
			IssueID:     run.IssueID,
			RunID:       run.RunID,
			TmuxSession: sessionName,
			Lines:       opts.Lines,
			Content:     content,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	// Plain text output - just print the content
	fmt.Print(content)

	return nil
}
