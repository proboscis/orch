package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/daemon"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/multiplexer"
	"github.com/spf13/cobra"
)

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
	client, err := requireDaemon()
	if err != nil {
		return err
	}

	var resp *daemon.GetAttachInfoResponse

	if shortIDRegex.MatchString(refStr) {
		resp, err = client.GetAttachInfo("", "", refStr)
	} else {
		ref, parseErr := model.ParseRunRef(refStr)
		if parseErr != nil {
			return parseErr
		}
		resp, err = client.GetAttachInfo(ref.IssueID, ref.RunID, "")
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "run not found: %s\n", refStr)
		os.Exit(ExitRunNotFound)
		return err
	}

	if resp.Agent == string(agent.AgentOpenCode) {
		return attachOpenCodeFromInfo(resp)
	}

	sessionName := resp.TmuxSession
	if sessionName == "" {
		sessionName = model.GenerateTmuxSession(resp.IssueID, resp.RunID)
	}

	cfg, _ := config.Load()
	muxType, _ := multiplexer.ParseType(cfg.GetMultiplexer())
	if resp.Multiplexer != "" {
		muxType, _ = multiplexer.ParseType(resp.Multiplexer)
	}

	var mux multiplexer.Multiplexer
	if muxType == multiplexer.TypeAuto {
		mux, err = multiplexer.GetAuto()
	} else {
		var warning string
		mux, warning, err = multiplexer.GetWithFallback(muxType)
		if warning != "" {
			fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "no multiplexer available: %v\n", err)
		os.Exit(ExitTmuxError)
		return err
	}

	if !mux.HasSession(sessionName) {
		if resp.WorktreePath == "" {
			fmt.Fprintf(os.Stderr, "session not found and no worktree path: %s\n", sessionName)
			os.Exit(ExitRunNotFound)
			return fmt.Errorf("session not found: %s", sessionName)
		}

		fmt.Fprintf(os.Stderr, "session not found, creating: %s\n", sessionName)
		err := mux.NewSession(&multiplexer.SessionConfig{
			SessionName: sessionName,
			WorkDir:     resp.WorktreePath,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to create session: %v\n", err)
			os.Exit(ExitTmuxError)
			return err
		}
	}

	if mux.IsInsideSession() {
		if err := mux.SwitchClient(sessionName); err != nil {
			os.Exit(ExitTmuxError)
			return err
		}
	} else {
		if err := mux.AttachSession(sessionName); err != nil {
			os.Exit(ExitTmuxError)
			return err
		}
	}

	return nil
}

func attachOpenCodeFromInfo(info *daemon.GetAttachInfoResponse) error {
	if info.ServerPort == 0 {
		fmt.Fprintf(os.Stderr, "no server port found for opencode run: %s#%s\n", info.IssueID, info.RunID)
		os.Exit(ExitRunNotFound)
		return fmt.Errorf("no server port found")
	}

	serverURL := fmt.Sprintf("http://127.0.0.1:%d", info.ServerPort)

	fmt.Fprintf(os.Stderr, "Attaching to opencode server: %s\n", serverURL)
	fmt.Fprintf(os.Stderr, "Session: %s\n", info.OpenCodeSessionID)
	fmt.Fprintf(os.Stderr, "Worktree: %s\n\n", info.WorktreePath)

	args := []string{"attach", serverURL}
	if info.OpenCodeSessionID != "" {
		args = append(args, "--session", info.OpenCodeSessionID)
	}
	if info.WorktreePath != "" {
		args = append(args, "--dir", info.WorktreePath)
	}

	cmd := exec.Command("opencode", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to attach to opencode: %v\n", err)
		fmt.Fprintf(os.Stderr, "\nManual: opencode attach %s --session %s --dir %s\n",
			serverURL, info.OpenCodeSessionID, info.WorktreePath)
		os.Exit(ExitTmuxError)
		return err
	}

	return nil
}
