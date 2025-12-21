package cli

import (
	"fmt"
	"os"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/tmux"
	"github.com/spf13/cobra"
)

// attach accepts short ID or run ref

type attachOptions struct {
	Pane   string
	Window string
}

func newAttachCmd() *cobra.Command {
	opts := &attachOptions{}

	cmd := &cobra.Command{
		Use:   "attach RUN_REF",
		Short: "Attach to a run's tmux session",
		Long: `Attach to the tmux session for a run.

This allows manual interaction with the agent, including image paste support.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAttach(args[0], opts)
		},
	}

	cmd.Flags().StringVar(&opts.Pane, "pane", "", "Pane to attach to (log|shell)")
	cmd.Flags().StringVar(&opts.Window, "window", "", "Window to attach to")

	return cmd
}

func runAttach(refStr string, opts *attachOptions) error {
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

	// Check if session exists, auto-create if missing
	if !tmux.HasSession(sessionName) {
		if run.WorktreePath == "" {
			fmt.Fprintf(os.Stderr, "session not found and no worktree path: %s\n", sessionName)
			os.Exit(ExitRunNotFound)
			return fmt.Errorf("session not found: %s", sessionName)
		}

		// Auto-create the session in the run's worktree
		fmt.Fprintf(os.Stderr, "session not found, creating: %s\n", sessionName)
		err := tmux.NewSession(&tmux.SessionConfig{
			SessionName: sessionName,
			WorkDir:     run.WorktreePath,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to create session: %v\n", err)
			os.Exit(ExitTmuxError)
			return err
		}
	}

	// Attach to session - use switch-client if already inside tmux
	if tmux.IsInsideTmux() {
		if err := tmux.SwitchClient(sessionName); err != nil {
			os.Exit(ExitTmuxError)
			return err
		}
	} else {
		if err := tmux.AttachSession(sessionName); err != nil {
			os.Exit(ExitTmuxError)
			return err
		}
	}

	return nil
}
