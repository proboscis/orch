package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/orchapi"
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
	TmuxSession string `json:"tmux_session,omitempty"`
	Lines       int    `json:"lines"`
	Content     string `json:"content"`
	Source      string `json:"source,omitempty"`
}

func runCapture(refStr string, opts *captureOptions) error {
	ctx := context.Background()

	api, err := getAPI()
	if err != nil {
		return outputCaptureError(err)
	}

	run, err := resolveRunAPI(ctx, api, refStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(ExitRunNotFound)
		return err
	}

	ref := orchapi.RunRef{IssueID: run.IssueID, RunID: run.RunID}
	captureResult, err := api.CaptureSession(ctx, ref, opts.Lines)
	if err != nil {
		return outputCaptureError(err)
	}

	return outputCaptureResultAPI(run, captureResult, opts)
}

func outputCaptureResultAPI(run *orchapi.Run, resp *orchapi.CaptureResult, opts *captureOptions) error {
	if globalOpts.JSON {
		result := &captureResult{
			OK:          true,
			IssueID:     run.IssueID,
			RunID:       run.RunID,
			TmuxSession: run.TmuxSession,
			Lines:       opts.Lines,
			Content:     resp.Content,
			Source:      resp.Source,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Print(resp.Content)
	return nil
}

func outputCaptureError(err error) error {
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

type openCodeCaptureResult struct {
	OK        bool                     `json:"ok"`
	IssueID   string                   `json:"issue_id"`
	RunID     string                   `json:"run_id"`
	SessionID string                   `json:"session_id"`
	Messages  []openCodeCaptureMessage `json:"messages"`
}

type openCodeCaptureMessage struct {
	Role    string              `json:"role"`
	Content string              `json:"content"`
	Parts   []agent.MessagePart `json:"parts"`
}

func formatMessageParts(parts []agent.MessagePart) string {
	var texts []string
	for _, part := range parts {
		switch part.Type {
		case "text":
			if part.Text != "" {
				texts = append(texts, part.Text)
			}
		case "tool_use":
			toolName := part.ToolName
			if toolName == "" {
				toolName = "unknown"
			}
			texts = append(texts, fmt.Sprintf("<tool: %s>", toolName))
		case "tool_result":
			resultText := truncateText(part.Text, 100)
			if resultText == "" {
				resultText = "..."
			}
			texts = append(texts, fmt.Sprintf("<result: %s>", resultText))
		case "thinking", "redacted_thinking":
			texts = append(texts, "<thinking...>")
		}
	}
	return strings.Join(texts, "\n")
}

func truncateText(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
