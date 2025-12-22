package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/tmux"
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
		Long: `Send a message to a running agent via tmux.

The message is sent to the agent's tmux session using send-keys.
By default, Enter is pressed after the message to submit it.

Examples:
  # Send a message to an agent
  orch send orch-023#20231220-100000 "Please focus on the UI tests first"

  # Send using short ID
  orch send a3b4c5 "Continue with the implementation"

  # Send text without pressing Enter
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
	OK          bool   `json:"ok"`
	IssueID     string `json:"issue_id"`
	RunID       string `json:"run_id"`
	TmuxSession string `json:"tmux_session"`
	Message     string `json:"message"`
}

func runSend(refStr, message string, opts *sendOptions) error {
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

	// Send the message
	if opts.NoEnter {
		err = tmux.SendKeysLiteral(sessionName, message)
	} else {
		err = tmux.SendKeys(sessionName, message)
	}

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
			fmt.Fprintf(os.Stderr, "error: failed to send message: %v\n", err)
		}
		os.Exit(ExitTmuxError)
		return err
	}

	// Output result
	result := &sendResult{
		OK:          true,
		IssueID:     run.IssueID,
		RunID:       run.RunID,
		TmuxSession: sessionName,
		Message:     message,
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
