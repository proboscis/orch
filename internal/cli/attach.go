package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/multiplexer"
	"github.com/s22625/orch/internal/orchapi"
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
	ctx := context.Background()
	api, err := getAPI()
	if err != nil {
		return err
	}

	ref, err := orchapi.ParseRunRef(refStr)
	if err != nil {
		return err
	}

	info, err := api.GetAttachInfo(ctx, ref)
	if err != nil {
		if errors.Is(err, orchapi.ErrNotFound) {
			fmt.Fprintf(os.Stderr, "run not found: %s\n", refStr)
			os.Exit(ExitRunNotFound)
			return err
		}
		return err
	}

	if !info.SessionExists {
		fmt.Fprintf(os.Stderr, "cannot attach: session not found (session: %s, worktree: %s)\n",
			info.SessionName, info.WorktreePath)
		os.Exit(ExitRunNotFound)
		return fmt.Errorf("cannot attach: session not found")
	}

	if info.Agent == string(agent.AgentOpenCode) {
		return attachOpenCodeFromInfo(info)
	}

	sessionName := info.SessionName
	if sessionName == "" {
		sessionName = model.GenerateSessionName(info.IssueID, info.RunID)
	}

	projectRoot, _ := getProjectRoot()
	cfg, _ := api.GetConfig(ctx, projectRoot)

	muxType, _ := multiplexer.ParseType(cfg.AgentMultiplexer)
	if info.Multiplexer != "" {
		muxType, _ = multiplexer.ParseType(string(info.Multiplexer))
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

func attachOpenCodeFromInfo(info *orchapi.AttachInfo) error {
	if info.ServerPort == 0 && info.OpenCodeSessionID == "" {
		fmt.Fprintf(os.Stderr, "no server port or session found for opencode run: %s#%s\n", info.IssueID, info.RunID)
		os.Exit(ExitRunNotFound)
		return fmt.Errorf("no server port or session found")
	}

	if info.ServerPort > 0 && isPortOpen(info.ServerPort) {
		return attachToRunningOpenCode(info)
	}

	if info.OpenCodeSessionID != "" && info.WorktreePath != "" {
		return resumeOpenCodeSession(info)
	}

	fmt.Fprintf(os.Stderr, "opencode server not running and no session to resume\n")
	os.Exit(ExitRunNotFound)
	return fmt.Errorf("cannot attach")
}

func isPortOpen(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func attachToRunningOpenCode(info *orchapi.AttachInfo) error {
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

func resumeOpenCodeSession(info *orchapi.AttachInfo) error {
	fmt.Fprintf(os.Stderr, "Server not running, resuming session in worktree\n")
	fmt.Fprintf(os.Stderr, "Session: %s\n", info.OpenCodeSessionID)
	fmt.Fprintf(os.Stderr, "Worktree: %s\n\n", info.WorktreePath)

	args := []string{"--session", info.OpenCodeSessionID, info.WorktreePath}

	cmd := exec.Command("opencode", args...)
	cmd.Dir = info.WorktreePath
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to resume opencode session: %v\n", err)
		fmt.Fprintf(os.Stderr, "\nManual: cd %s && opencode --session %s\n",
			info.WorktreePath, info.OpenCodeSessionID)
		os.Exit(ExitTmuxError)
		return err
	}

	return nil
}
