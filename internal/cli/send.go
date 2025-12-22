package cli

import (
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
		Use:   "send RUN_REF MESSAGE",
		Short: "Send a message to a run's agent session",
		Long: `Send a message to the tmux session for a run.

The message is sent to the agent's input and automatically submitted with Enter.
Use --no-enter to send text without pressing Enter (useful for multi-part input).

Examples:
  orch send orch-001 "Please fix the bug in main.go"
  orch send 66ff6 "Continue with the implementation"
  orch send orch-001#20251222-100000 "Run the tests"
  orch send orch-001 --no-enter "partial text"`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSend(args[0], args[1], opts)
		},
	}

	cmd.Flags().BoolVar(&opts.NoEnter, "no-enter", false, "Send text without pressing Enter")

	return cmd
}

func runSend(refStr, message string, opts *sendOptions) error {
	st, err := getStore()
	if err != nil {
		return err
	}

	// Resolve by short ID or run ref
	run, err := resolveRun(st, refStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run not found: %s\n", refStr)
		os.Exit(ExitRunNotFound)
		return err
	}

	sessionName := run.TmuxSession
	if sessionName == "" {
		// Generate session name if not stored
		sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
	}

	// Check if session exists
	if !tmux.HasSession(sessionName) {
		fmt.Fprintf(os.Stderr, "session not found: %s\n", sessionName)
		os.Exit(ExitTmuxError)
		return fmt.Errorf("session not found: %s", sessionName)
	}

	// Send the message
	var sendErr error
	if opts.NoEnter {
		sendErr = tmux.SendText(sessionName, message)
	} else {
		sendErr = tmux.SendKeys(sessionName, message)
	}

	if sendErr != nil {
		fmt.Fprintf(os.Stderr, "failed to send message: %v\n", sendErr)
		os.Exit(ExitTmuxError)
		return sendErr
	}

	if !globalOpts.Quiet {
		fmt.Printf("sent message to %s#%s\n", run.IssueID, run.RunID)
	}

	return nil
}
