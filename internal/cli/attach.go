package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

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

type attachSessionMux interface {
	IsInsideSession() bool
	SwitchClient(session string) error
	AttachSession(session string) error
}

type attachStreams struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

type openCodeExecutor func(args []string, dir string, streams attachStreams) error

type attachDeps struct {
	getAPI             func() (orchapi.OrchAPI, error)
	parseRunRef        func(string) (orchapi.RunRef, error)
	parseMuxType       func(string) (multiplexer.Type, error)
	getMuxAuto         func() (attachSessionMux, error)
	getMuxWithFallback func(multiplexer.Type) (attachSessionMux, string, error)
	attachOpenCode     func(*orchapi.AttachInfo, attachStreams) (int, error)
	attachRemote       func(*orchapi.AttachInfo, attachStreams) (int, error)
	streams            attachStreams
	exit               func(int)
}

func defaultAttachDeps() *attachDeps {
	return &attachDeps{
		getAPI:       getAPIForListing,
		parseRunRef:  orchapi.ParseRunRef,
		parseMuxType: multiplexer.ParseType,
		getMuxAuto: func() (attachSessionMux, error) {
			return multiplexer.GetAuto()
		},
		getMuxWithFallback: func(t multiplexer.Type) (attachSessionMux, string, error) {
			return multiplexer.GetWithFallback(t)
		},
		attachOpenCode: attachOpenCodeFromInfoWithExecutor,
		attachRemote:   attachRemoteFromInfoWithExecutor,
		streams: attachStreams{
			stdin:  os.Stdin,
			stdout: os.Stdout,
			stderr: os.Stderr,
		},
		exit: os.Exit,
	}
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
	return runAttachWithDeps(refStr, opts, defaultAttachDeps())
}

func runAttachWithDeps(refStr string, opts *attachOptions, deps *attachDeps) error {
	ctx := context.Background()
	api, err := deps.getAPI()
	if err != nil {
		return err
	}

	ref, err := deps.parseRunRef(refStr)
	if err != nil {
		return err
	}

	info, err := api.GetAttachInfo(ctx, ref)
	if err != nil {
		if errors.Is(err, orchapi.ErrNotFound) {
			fmt.Fprintf(deps.streams.stderr, "run not found: %s\n", refStr)
			deps.exit(ExitRunNotFound)
			return err
		}
		return err
	}

	if !info.SessionExists && !isLocalControlHost(info.TargetHost) {
		fmt.Fprintf(deps.streams.stderr, "cannot attach: session not found (session: %s, worktree: %s)\n",
			info.SessionName, info.WorktreePath)
		deps.exit(ExitRunNotFound)
		return fmt.Errorf("cannot attach: session not found")
	}

	if info.TargetHost != "" && !isLocalControlHost(info.TargetHost) {
		exitCode, attachErr := deps.attachRemote(info, deps.streams)
		if exitCode != 0 {
			deps.exit(exitCode)
		}
		return attachErr
	}

	if info.Agent == string(agent.AgentOpenCode) {
		exitCode, attachErr := deps.attachOpenCode(info, deps.streams)
		if exitCode != 0 {
			deps.exit(exitCode)
		}
		return attachErr
	}

	sessionName := info.SessionName
	if sessionName == "" {
		sessionName = model.GenerateSessionName(info.IssueID, info.RunID)
	}

	cfg, _ := api.GetConfig(ctx)

	muxSetting := ""
	if cfg != nil {
		muxSetting = cfg.AgentMultiplexer
	}
	muxType, _ := deps.parseMuxType(muxSetting)
	if info.Multiplexer != "" {
		muxType, _ = deps.parseMuxType(string(info.Multiplexer))
	}

	var mux attachSessionMux
	if muxType == multiplexer.TypeAuto {
		mux, err = deps.getMuxAuto()
	} else {
		var warning string
		mux, warning, err = deps.getMuxWithFallback(muxType)
		if warning != "" {
			fmt.Fprintf(deps.streams.stderr, "warning: %s\n", warning)
		}
	}
	if err != nil {
		fmt.Fprintf(deps.streams.stderr, "no multiplexer available: %v\n", err)
		deps.exit(ExitTmuxError)
		return err
	}

	if mux.IsInsideSession() {
		if err := mux.SwitchClient(sessionName); err != nil {
			deps.exit(ExitTmuxError)
			return err
		}
	} else {
		if err := mux.AttachSession(sessionName); err != nil {
			deps.exit(ExitTmuxError)
			return err
		}
	}

	return nil
}

var runSSHCommand = func(args []string, streams attachStreams) error {
	cmd := exec.Command("ssh", args...)
	cmd.Stdin = streams.stdin
	cmd.Stdout = streams.stdout
	cmd.Stderr = streams.stderr
	return cmd.Run()
}

func attachRemoteFromInfoWithExecutor(info *orchapi.AttachInfo, streams attachStreams) (int, error) {
	if strings.EqualFold(info.Agent, string(agent.AgentOpenCode)) {
		script, err := buildRemoteOpenCodeAttachScript(info)
		if err != nil {
			fmt.Fprintf(streams.stderr, "%v\n", err)
			return ExitRunNotFound, err
		}
		if err := runSSHCommand(sshScriptArgs(info.TargetHost, true, script), streams); err != nil {
			fmt.Fprintf(streams.stderr, "failed to attach via ssh: %v\n", err)
			return ExitTmuxError, err
		}
		return 0, nil
	}

	sessionName := info.SessionName
	if sessionName == "" {
		sessionName = model.GenerateSessionName(info.IssueID, info.RunID)
	}

	var args []string
	switch info.Multiplexer {
	case orchapi.MultiplexerZellij:
		args = []string{"-t", info.TargetHost, "zellij", "attach", sessionName}
	default:
		args = []string{"-t", info.TargetHost, "tmux", "attach-session", "-t", sessionName}
	}

	if err := runSSHCommand(args, streams); err != nil {
		fmt.Fprintf(streams.stderr, "failed to attach via ssh: %v\n", err)
		return ExitTmuxError, err
	}

	return 0, nil
}

func attachOpenCodeFromInfo(info *orchapi.AttachInfo) error {
	exitCode, err := attachOpenCodeFromInfoWithExecutor(info, attachStreams{
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
	})
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return err
}

func attachOpenCodeFromInfoWithExecutor(info *orchapi.AttachInfo, streams attachStreams) (int, error) {
	if info.ServerPort == 0 && info.OpenCodeSessionID == "" {
		fmt.Fprintf(streams.stderr, "no server port or session found for opencode run: %s#%s\n", info.IssueID, info.RunID)
		return ExitRunNotFound, fmt.Errorf("no server port or session found")
	}

	if info.ServerPort > 0 {
		return attachToRunningOpenCodeWithExecutor(info, streams)
	}

	if info.OpenCodeSessionID != "" && info.WorktreePath != "" {
		return resumeOpenCodeSessionWithExecutor(info, streams)
	}

	fmt.Fprintf(streams.stderr, "no opencode server port and no session to resume\n")
	return ExitRunNotFound, fmt.Errorf("cannot attach")
}

var runOpenCodeCommand = func(args []string, dir string, streams attachStreams) error {
	cmd := exec.Command("opencode", args...)
	cmd.Dir = dir
	cmd.Stdin = streams.stdin
	cmd.Stdout = streams.stdout
	cmd.Stderr = streams.stderr
	return cmd.Run()
}

func attachToRunningOpenCode(info *orchapi.AttachInfo) error {
	exitCode, err := attachToRunningOpenCodeWithExecutor(info, attachStreams{
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
	})
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return err
}

func attachToRunningOpenCodeWithExecutor(info *orchapi.AttachInfo, streams attachStreams) (int, error) {
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", info.ServerPort)

	fmt.Fprintf(streams.stderr, "Attaching to opencode server: %s\n", serverURL)
	fmt.Fprintf(streams.stderr, "Session: %s\n", info.OpenCodeSessionID)
	fmt.Fprintf(streams.stderr, "Worktree: %s\n\n", info.WorktreePath)

	args := []string{"attach", serverURL}
	if info.OpenCodeSessionID != "" {
		args = append(args, "--session", info.OpenCodeSessionID)
	}
	if info.WorktreePath != "" {
		args = append(args, "--dir", info.WorktreePath)
	}

	if err := runOpenCodeCommand(args, "", streams); err != nil {
		fmt.Fprintf(streams.stderr, "failed to attach to opencode: %v\n", err)
		fmt.Fprintf(streams.stderr, "\nManual: opencode attach %s --session %s --dir %s\n",
			serverURL, info.OpenCodeSessionID, info.WorktreePath)
		return ExitTmuxError, err
	}

	return 0, nil
}

func resumeOpenCodeSession(info *orchapi.AttachInfo) error {
	exitCode, err := resumeOpenCodeSessionWithExecutor(info, attachStreams{
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
	})
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return err
}

func resumeOpenCodeSessionWithExecutor(info *orchapi.AttachInfo, streams attachStreams) (int, error) {
	fmt.Fprintf(streams.stderr, "Server not running, resuming session in worktree\n")
	fmt.Fprintf(streams.stderr, "Session: %s\n", info.OpenCodeSessionID)
	fmt.Fprintf(streams.stderr, "Worktree: %s\n\n", info.WorktreePath)

	args := []string{"--session", info.OpenCodeSessionID, info.WorktreePath}

	if err := runOpenCodeCommand(args, info.WorktreePath, streams); err != nil {
		fmt.Fprintf(streams.stderr, "failed to resume opencode session: %v\n", err)
		fmt.Fprintf(streams.stderr, "\nManual: cd %s && opencode --session %s\n",
			info.WorktreePath, info.OpenCodeSessionID)
		return ExitTmuxError, err
	}

	return 0, nil
}
