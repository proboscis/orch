package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/daemon"
	"github.com/spf13/cobra"
)

type sendOptions struct {
	NoEnter bool
}

func newSendCmd() *cobra.Command {
	opts := &sendOptions{}

	cmd := &cobra.Command{
		Use:   "send <RUN_REF> <MESSAGE>",
		Short: "Send a message to a running agent",
		Long: `Send a message to a running agent.

For tmux-based agents (claude, codex, gemini), the message is sent via send-keys.
For opencode agents, the message is sent via HTTP API.

By default, Enter is pressed after the message for tmux agents.
The --no-enter flag is ignored for opencode agents.

Examples:
  # Send a message to an agent
  orch send orch-023#20231220-100000 "Please focus on the UI tests first"

  # Send using short ID
  orch send a3b4c5 "Continue with the implementation"

  # Send text without pressing Enter (tmux agents only)
  orch send orch-023 "partial input" --no-enter`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSend(args[0], args[1], opts)
		},
	}

	cmd.Flags().BoolVar(&opts.NoEnter, "no-enter", false, "Don't press Enter after sending the message")

	return cmd
}

type sendResult struct {
	OK      bool   `json:"ok"`
	IssueID string `json:"issue_id"`
	RunID   string `json:"run_id"`
	Message string `json:"message"`
}

func runSend(refStr, message string, opts *sendOptions) error {
	st, err := getStore()
	if err != nil {
		return err
	}

	run, err := resolveRun(st, refStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(ExitRunNotFound)
		return err
	}

	isOpenCode := run.Agent == string(agent.AgentOpenCode)

	if isOpenCode && daemon.IsDaemonSocketAvailable(st.VaultPath()) {
		err = daemon.SendViaDaemon(st.VaultPath(), run, message, opts.NoEnter)
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
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
			}
			os.Exit(ExitAgentError)
			return err
		}
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		manager := agent.GetManager(run)
		sendOpts := &agent.SendOptions{NoEnter: opts.NoEnter}

		err = manager.SendMessage(ctx, run, message, sendOpts)
		if err != nil {
			exitCode := ExitAgentError
			var sessionErr *agent.SessionNotFoundError
			if errors.As(err, &sessionErr) {
				exitCode = ExitTmuxError
			}

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
			os.Exit(exitCode)
			return err
		}
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
