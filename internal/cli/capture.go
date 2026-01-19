package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/s22625/orch/internal/agent"
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
		Long: `Capture the latest output from an agent's tmux pane or OpenCode session.

Returns the captured text to stdout for programmatic consumption.
Useful for monitoring agent status or building automation workflows.

For tmux-based agents (claude, codex, gemini): captures terminal output.
For OpenCode agents: captures recent messages from the session.

Examples:
  # Capture last 100 lines/messages (default) from an agent
  orch capture orch-023#20231220-100000

  # Capture using short ID
  orch capture a3b4c5

  # Capture last 500 lines/messages
  orch capture orch-023 --lines 500

  # Output as JSON for scripting
  orch capture orch-023 --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCapture(args[0], opts)
		},
	}

	cmd.Flags().IntVar(&opts.Lines, "lines", 100, "Number of lines (tmux) or messages (OpenCode) to capture")

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

// openCodeCaptureResult is used for JSON output of OpenCode captures
type openCodeCaptureResult struct {
	OK        bool                   `json:"ok"`
	IssueID   string                 `json:"issue_id"`
	RunID     string                 `json:"run_id"`
	SessionID string                 `json:"session_id"`
	Port      int                    `json:"port"`
	Messages  []openCodeMessageEntry `json:"messages"`
}

type openCodeMessageEntry struct {
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	Content   string    `json:"content"`
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
	if run.Agent == "opencode" {
		return captureOpenCode(run, opts)
	}

	// Existing tmux capture logic for other agents
	return captureTmux(run, opts)
}

// captureOpenCode captures messages from an OpenCode session via the API
func captureOpenCode(run *model.Run, opts *captureOptions) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Check if server port is available
	if run.ServerPort == 0 {
		err := fmt.Errorf("no OpenCode server port recorded for this run")
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

	client := agent.NewOpenCodeClient(run.ServerPort)

	// Check if server is running
	if !client.IsServerRunning(ctx) {
		err := fmt.Errorf("OpenCode server not running on port %d (run may not be active)", run.ServerPort)
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

	// Check if session ID is available
	if run.OpenCodeSessionID == "" {
		err := fmt.Errorf("no OpenCode session ID recorded for this run")
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

	// Get messages from the session
	messages, err := client.GetMessages(ctx, run.OpenCodeSessionID, run.WorktreePath)
	if err != nil {
		if globalOpts.JSON {
			result := map[string]interface{}{
				"ok":    false,
				"error": fmt.Sprintf("failed to get messages: %v", err),
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(result)
		} else {
			fmt.Fprintf(os.Stderr, "error: failed to get messages: %v\n", err)
		}
		os.Exit(ExitTmuxError)
		return err
	}

	// Limit messages based on opts.Lines
	// For OpenCode, --lines means number of messages
	startIdx := 0
	if len(messages) > opts.Lines {
		startIdx = len(messages) - opts.Lines
	}
	messages = messages[startIdx:]

	// Output result
	if globalOpts.JSON {
		entries := make([]openCodeMessageEntry, len(messages))
		for i, msg := range messages {
			entries[i] = openCodeMessageEntry{
				Role:      msg.Info.Role,
				CreatedAt: msg.Info.CreatedAt,
				Content:   formatMessageParts(msg.Parts),
			}
		}

		result := &openCodeCaptureResult{
			OK:        true,
			IssueID:   run.IssueID,
			RunID:     run.RunID,
			SessionID: run.OpenCodeSessionID,
			Port:      run.ServerPort,
			Messages:  entries,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	// Plain text output - format messages similar to chat transcript
	for _, msg := range messages {
		content := formatMessageParts(msg.Parts)
		fmt.Printf("[%s] %s\n", msg.Info.Role, content)
	}

	return nil
}

// formatMessageParts extracts and concatenates text content from message parts
func formatMessageParts(parts []agent.MessagePart) string {
	var texts []string
	for _, part := range parts {
		if part.Type == "text" && part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// captureTmux captures output from a tmux pane (original behavior)
func captureTmux(run *model.Run, opts *captureOptions) error {
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
