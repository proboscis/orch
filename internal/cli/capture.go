package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/multiplexer"
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
}

// openCodeCaptureResult represents JSON output for OpenCode captures
type openCodeCaptureResult struct {
	OK        bool                     `json:"ok"`
	IssueID   string                   `json:"issue_id"`
	RunID     string                   `json:"run_id"`
	SessionID string                   `json:"session_id"`
	Messages  []openCodeCaptureMessage `json:"messages"`
}

type openCodeCaptureMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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

	// Check if this is an OpenCode run
	if run.Agent == string(agent.AgentOpenCode) {
		return captureOpenCode(run, opts)
	}

	// Existing tmux capture logic for other agents
	return captureTmux(run, opts)
}

func captureOpenCode(run *model.Run, opts *captureOptions) error {
	ctx := context.Background()

	// Check if we have required OpenCode fields
	if run.ServerPort == 0 {
		err := fmt.Errorf("OpenCode run has no server port recorded (run may have ended)")
		return outputCaptureError(err)
	}

	// Create client and check if server is running
	client := agent.NewOpenCodeClient(run.ServerPort)
	if !client.IsServerRunning(ctx) {
		err := fmt.Errorf("OpenCode server not running on port %d (run may have ended)", run.ServerPort)
		return outputCaptureError(err)
	}

	// Get session ID - use recorded session or empty for default
	sessionID := run.OpenCodeSessionID
	if sessionID == "" {
		err := fmt.Errorf("OpenCode run has no session ID recorded")
		return outputCaptureError(err)
	}

	// Fetch messages from OpenCode API
	messages, err := client.GetMessages(ctx, sessionID, run.WorktreePath)
	if err != nil {
		return outputCaptureError(fmt.Errorf("failed to get messages: %w", err))
	}

	// Limit messages based on --lines flag
	// Since --lines is designed for terminal lines, we'll interpret it as number of messages
	if len(messages) > opts.Lines {
		messages = messages[len(messages)-opts.Lines:]
	}

	// Output result
	if globalOpts.JSON {
		result := &openCodeCaptureResult{
			OK:        true,
			IssueID:   run.IssueID,
			RunID:     run.RunID,
			SessionID: sessionID,
			Messages:  make([]openCodeCaptureMessage, len(messages)),
		}
		for i, msg := range messages {
			result.Messages[i] = openCodeCaptureMessage{
				Role:    msg.Info.Role,
				Content: formatMessageParts(msg.Parts),
			}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	// Plain text output - format messages similar to conversation
	for _, msg := range messages {
		content := formatMessageParts(msg.Parts)
		fmt.Printf("[%s] %s\n", msg.Info.Role, content)
	}

	return nil
}

// formatMessageParts concatenates text parts of a message
func formatMessageParts(parts []agent.MessagePart) string {
	var texts []string
	for _, part := range parts {
		if part.Type == "text" && part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// outputCaptureError outputs an error in the appropriate format
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

func captureTmux(run *model.Run, opts *captureOptions) error {
	mux, err := multiplexer.GetAuto()
	if err != nil {
		return fmt.Errorf("no multiplexer available: %w", err)
	}

	sessionName := run.TmuxSession
	if sessionName == "" {
		sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
	}

	if !mux.HasSession(sessionName) {
		err := fmt.Errorf("session %q not found (run may not be active)", sessionName)
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

	content, err := mux.CapturePane(sessionName, opts.Lines)
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
