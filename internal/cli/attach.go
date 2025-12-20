package cli

import (
	"fmt"
	"os"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/tmux"
	"github.com/spf13/cobra"
)

type attachOptions struct {
	Pane   string
	Window string
	Create bool
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
	cmd.Flags().BoolVar(&opts.Create, "create", false, "Create session if it doesn't exist")

	return cmd
}

func runAttach(refStr string, opts *attachOptions) error {
	st, err := getStore()
	if err != nil {
		return err
	}

	ref, err := model.ParseRunRef(refStr)
	if err != nil {
		return err
	}

	run, err := st.GetRun(ref)
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
		if opts.Create {
			// Create the session
			err := tmux.NewSession(&tmux.SessionConfig{
				SessionName: sessionName,
				WorkDir:     run.WorktreePath,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to create session: %v\n", err)
				os.Exit(ExitTmuxError)
				return err
			}
		} else {
			fmt.Fprintf(os.Stderr, "session not found: %s\n", sessionName)
			fmt.Fprintf(os.Stderr, "Use --create to create a new session\n")
			os.Exit(ExitRunNotFound)
			return fmt.Errorf("session not found: %s", sessionName)
		}
	}

	// Attach to session
	if err := tmux.AttachSession(sessionName); err != nil {
		os.Exit(ExitTmuxError)
		return err
	}

	return nil
}
