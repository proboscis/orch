package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/orchapi"
	"github.com/spf13/cobra"
)

type sendOptions struct {
	NoEnter bool
	DryRun  bool
}

func newSendCmd() *cobra.Command {
	opts := &sendOptions{}

	cmd := &cobra.Command{
		Use:   "send <RUN_REF> [MESSAGE]",
		Short: "Send a message to a running agent",
		Long: `Send a message to a running agent.

This is the primary way to interact with waiting runs.
Capture the latest output with orch capture first, then reply with orch send.

Provide the message as an argument, or omit it and redirect stdin via a
pipe/heredoc for multi-line input.

For tmux-based agents (claude, codex, gemini), the message is sent via send-keys.
For opencode agents, the message is sent via HTTP API.

By default, Enter is pressed after the message for tmux agents.
The --no-enter flag is ignored for opencode agents.

If sending fails, treat it as an infrastructure issue first:
  1. orch capture <RUN_REF>
  2. orch ps
  3. Check the multiplexer directly (tmux list-sessions / zellij list-sessions)
  4. Write feedback into ORCH_PROMPT.md in the run worktree
  5. Use native multiplexer send (tmux send-keys / zellij action write-chars)

Do NOT use orch restart-from unless the run is failed, canceled, or unknown.

Examples:
  # Send a message to an agent
  orch send orch-023#20231220-100000 "Please focus on the UI tests first"

  # Send using short ID
  orch send a3b4c5 "Continue with the implementation"

  # Send a multi-line message via heredoc
  orch send orch-023 <<'EOF'
  Please fix the failing login test.
  Then rerun the focused auth suite.
  EOF

  # Send text without pressing Enter (tmux agents only)
  orch send orch-023 "partial input" --no-enter

  # Validate the run is ready to receive messages (without sending)
  orch send orch-023 --dry-run`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			message, err := resolveSendMessage(args, opts, os.Stdin, stdinIsTerminal(os.Stdin))
			if err != nil {
				return err
			}
			return runSend(args[0], message, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.NoEnter, "no-enter", false, "Don't press Enter after sending the message")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Validate config without sending the message")

	return cmd
}

func resolveSendMessage(args []string, opts *sendOptions, stdin io.Reader, stdinIsTTY bool) (string, error) {
	if len(args) >= 2 {
		return args[1], nil
	}
	if opts != nil && opts.DryRun {
		return "", nil
	}
	if stdinIsTTY {
		return "", fmt.Errorf("MESSAGE required: pass it as an argument or redirect stdin with a pipe/heredoc")
	}

	data, err := io.ReadAll(stdin)
	if err != nil {
		return "", fmt.Errorf("read stdin message: %w", err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("MESSAGE required: stdin was empty")
	}

	return trimSingleTrailingNewline(string(data)), nil
}

func trimSingleTrailingNewline(message string) string {
	message = strings.TrimSuffix(message, "\n")
	message = strings.TrimSuffix(message, "\r")
	return message
}

func stdinIsTerminal(file *os.File) bool {
	if file == nil {
		return true
	}
	fd := file.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

type sendResult struct {
	OK      bool   `json:"ok"`
	IssueID string `json:"issue_id"`
	RunID   string `json:"run_id"`
	Message string `json:"message"`
}

func runSend(refStr, message string, opts *sendOptions) error {
	ctx := context.Background()

	api, err := getAPI()
	if err != nil {
		return err
	}

	run, err := resolveRunAPI(ctx, api, refStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(ExitRunNotFound)
		return err
	}

	isOpenCode := run.Agent == string(agent.AgentOpenCode)

	if opts.DryRun {
		return validateSendConfig(run, isOpenCode)
	}

	ref := orchapi.RunRef{IssueID: run.IssueID, RunID: run.RunID}
	info, err := getRunAttachInfo(ctx, api, ref)
	if err != nil {
		formattedErr := formatSendFailureMessage(err, run)
		exitCode := ExitAgentError
		if strings.Contains(strings.ToLower(err.Error()), "session not found") {
			exitCode = ExitTmuxError
		}
		if globalOpts.JSON {
			result := map[string]interface{}{
				"ok":    false,
				"error": formattedErr,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(result)
		} else {
			fmt.Fprintf(os.Stderr, "error: %s\n", formattedErr)
		}
		os.Exit(exitCode)
		return err
	}

	if shouldHandleRunLocally(info) {
		err = sendLocalFromInfo(info, message, opts.NoEnter)
	} else if strings.TrimSpace(info.TargetHost) != "" {
		err = sendRemoteFromInfo(info, message, opts.NoEnter)
	} else {
		err = api.SendMessage(ctx, ref, message)
	}
	if err != nil {
		formattedErr := formatSendFailureMessage(err, run)
		exitCode := ExitAgentError
		if strings.Contains(err.Error(), "has ended") {
			exitCode = ExitRunEnded
		}
		if strings.Contains(err.Error(), "session") && strings.Contains(err.Error(), "not found") {
			exitCode = ExitTmuxError
		}

		if globalOpts.JSON {
			result := map[string]interface{}{
				"ok":    false,
				"error": formattedErr,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(result)
		} else {
			fmt.Fprintf(os.Stderr, "error: %s\n", formattedErr)
		}
		os.Exit(exitCode)
		return err
	}

	result := &sendResult{
		OK:      true,
		IssueID: run.IssueID,
		RunID:   run.RunID,
		Message: message,
	}

	if globalOpts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	if !globalOpts.Quiet {
		fmt.Printf("Sent message to %s#%s\n", run.IssueID, run.RunID)
	}

	return nil
}

func formatSendFailureMessage(err error, run *orchapi.Run) string {
	if err == nil {
		return ""
	}

	runRef := "<RUN_REF>"
	promptPath := "ORCH_PROMPT.md"
	if run != nil {
		if run.IssueID != "" && run.RunID != "" {
			runRef = run.IssueID + "#" + run.RunID
		} else if run.IssueID != "" {
			runRef = run.IssueID
		}
		if run.WorktreePath != "" {
			promptPath = filepath.Join(run.WorktreePath, "ORCH_PROMPT.md")
		}
	}

	return fmt.Sprintf("%s\n\nSend failed. Try this escalation path before assuming the run is broken:\n  1. orch capture %s\n  2. orch ps\n  3. Check the multiplexer directly (tmux list-sessions / zellij list-sessions)\n  4. Write feedback into %s\n  5. Use native multiplexer send (tmux send-keys / zellij action write-chars)\n\nDo NOT use orch restart-from - the run is likely still alive.", err.Error(), runRef, promptPath)
}

func validateSendConfig(run *orchapi.Run, isOpenCode bool) error {
	if isOpenCode {
		var issues []string

		if run.ServerPort <= 0 {
			issues = append(issues, "missing server port (agent may not be running)")
		}
		if run.OpenCodeSessionID == "" {
			issues = append(issues, "missing session ID (agent may still be booting)")
		}

		if len(issues) > 0 {
			if globalOpts.JSON {
				result := map[string]interface{}{
					"ok":       false,
					"issue_id": run.IssueID,
					"run_id":   run.RunID,
					"errors":   issues,
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}
			fmt.Fprintf(os.Stderr, "Run %s#%s is not ready to receive messages:\n", run.IssueID, run.RunID)
			for _, issue := range issues {
				fmt.Fprintf(os.Stderr, "  - %s\n", issue)
			}
			os.Exit(ExitAgentError)
			return nil
		}

		if globalOpts.JSON {
			result := map[string]interface{}{
				"ok":         true,
				"issue_id":   run.IssueID,
				"run_id":     run.RunID,
				"agent":      run.Agent,
				"port":       run.ServerPort,
				"session_id": run.OpenCodeSessionID,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		}

		fmt.Printf("Run %s#%s is ready to receive messages\n", run.IssueID, run.RunID)
		fmt.Printf("  Agent: %s\n", run.Agent)
		fmt.Printf("  Port: %d\n", run.ServerPort)
		fmt.Printf("  Session: %s\n", run.OpenCodeSessionID)
		return nil
	}

	// For tmux-based agents, check session exists
	if globalOpts.JSON {
		result := map[string]interface{}{
			"ok":       true,
			"issue_id": run.IssueID,
			"run_id":   run.RunID,
			"agent":    run.Agent,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Printf("Run %s#%s is ready to receive messages\n", run.IssueID, run.RunID)
	fmt.Printf("  Agent: %s\n", run.Agent)
	return nil
}
