package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/monitor"
	"github.com/s22625/orch/internal/multiplexer"
	"github.com/s22625/orch/internal/orchapi"
	"github.com/spf13/cobra"
)

const (
	controlAgentFileName    = "control-agent.json"
	controlAgentSessionName = "orch-control-agent"
)

// ControlAgentState persists state about the control agent session.
type ControlAgentState struct {
	Backend            string    `json:"backend"`                       // Agent backend (opencode, claude, etc.)
	CreatedAt          time.Time `json:"created_at"`                    // When the session was created
	SessionID          string    `json:"session_id,omitempty"`          // For opencode: native session ID
	MultiplexerSession string    `json:"multiplexer_session,omitempty"` // For claude/codex: tmux/zellij session name
	Multiplexer        string    `json:"multiplexer,omitempty"`         // For claude/codex: "tmux" or "zellij"
}

type agentOptions struct {
	New     bool
	Backend string
	Kill    bool
}

func newAgentCmd() *cobra.Command {
	opts := &agentOptions{}

	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Launch and manage control agent sessions",
		Long: `Launch and manage control agent sessions.

By default, attaches to an existing control agent session if one exists,
otherwise creates a new one.

For opencode backend: Uses native --session flag for persistence (no multiplexer).
For claude/codex/gemini: Uses tmux or zellij based on multiplexer config.

Examples:
  orch agent                    # Attach to existing or create new
  orch agent --new              # Force create a new session
  orch agent --backend claude   # Use claude backend (with multiplexer)
  orch agent --kill             # Terminate control agent session`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgent(opts)
		},
	}

	cmd.Flags().BoolVar(&opts.New, "new", false, "Force create a new control agent session")
	cmd.Flags().StringVar(&opts.Backend, "backend", "", "Agent backend (opencode, claude, codex, gemini)")
	cmd.Flags().BoolVar(&opts.Kill, "kill", false, "Terminate the control agent session")

	return cmd
}

func runAgent(opts *agentOptions) error {
	projectRoot, err := getProjectRoot()
	if err != nil {
		return fmt.Errorf("project root required for agent: %w", err)
	}

	orchDir := monitor.GetOrchDir(projectRoot)

	// Handle --kill flag
	if opts.Kill {
		return killControlAgent(orchDir)
	}

	// Determine backend from flag or config
	backend := opts.Backend
	if backend == "" {
		ctx := context.Background()
		api, err := getAPI()
		if err == nil {
			cfg, err := api.GetConfig(ctx, projectRoot)
			if err == nil && cfg.Agent != "" {
				backend = cfg.Agent
			}
		}
	}
	if backend == "" {
		return fmt.Errorf("no agent configured: set 'agent' in .orch/config.yaml or use --backend flag")
	}

	// Validate backend
	agentType, err := agent.ParseAgentType(backend)
	if err != nil {
		return fmt.Errorf("invalid backend: %w", err)
	}

	// Route to appropriate handler based on backend
	if agentType == agent.AgentOpenCode {
		return runOpenCodeAgent(orchDir, opts)
	}
	return runMultiplexerAgent(orchDir, opts, agentType)
}

// runOpenCodeAgent handles opencode backend using native --session flag (no tmux)
func runOpenCodeAgent(orchDir string, opts *agentOptions) error {
	// Load existing state
	state := loadControlAgentState(orchDir)

	// If --new flag, clear existing session state
	if opts.New {
		if err := clearControlAgentState(orchDir); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to clear existing state: %v\n", err)
		}
		state = nil
	}

	// Check if existing session is alive via opencode's session mechanism
	var sessionID string
	if state != nil && state.SessionID != "" && state.Backend == "opencode" {
		// opencode handles session liveness internally via --session flag
		sessionID = state.SessionID
		fmt.Fprintf(os.Stderr, "Resuming opencode session: %s\n", sessionID)
	}

	issuesRoot, err := getIssuesRoot()
	if err != nil {
		return fmt.Errorf("failed to get issues root: %w", err)
	}

	promptPath, err := writeControlPromptViaAPI(issuesRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to write control prompt: %v\n", err)
	}

	binary, err := exec.LookPath("opencode")
	if err != nil {
		return fmt.Errorf("opencode not found in PATH")
	}

	args := []string{}

	// Resume existing session or start fresh
	if sessionID != "" {
		args = append(args, "--session", sessionID)
	}

	// Add prompt instruction
	args = append(args, "--prompt", monitor.GetControlPromptInstruction())

	if promptPath != "" {
		fmt.Fprintf(os.Stderr, "Control prompt: %s\n", promptPath)
	}

	// Create new state before running (opencode will use/create session)
	if state == nil || opts.New {
		// Generate a session ID for opencode to use
		newSessionID := fmt.Sprintf("orch-control-%d", time.Now().Unix())
		args = []string{"--session", newSessionID, "--prompt", monitor.GetControlPromptInstruction()}

		newState := &ControlAgentState{
			Backend:   "opencode",
			CreatedAt: time.Now(),
			SessionID: newSessionID,
		}
		if err := saveControlAgentState(orchDir, newState); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to save state: %v\n", err)
		}
		fmt.Fprintf(os.Stderr, "Creating opencode session: %s\n", newSessionID)
	}

	// Run opencode interactively (no tmux wrapper)
	cmd := exec.Command(binary, args...)
	cmd.Dir, _ = os.Getwd()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// runMultiplexerAgent handles claude/codex/gemini using tmux or zellij
func runMultiplexerAgent(orchDir string, opts *agentOptions, agentType agent.AgentType) error {
	// Get multiplexer from config (use agent multiplexer, default: tmux)
	muxType := multiplexer.TypeTmux
	ctx := context.Background()
	api, apiErr := getAPI()
	if apiErr == nil {
		projectRoot, _ := getProjectRoot()
		cfg, cfgErr := api.GetConfig(ctx, projectRoot)
		if cfgErr == nil && cfg.AgentMultiplexer != "" {
			parsed, parseErr := multiplexer.ParseType(cfg.AgentMultiplexer)
			if parseErr == nil && parsed != multiplexer.TypeAuto {
				muxType = parsed
			}
		}
	}

	// Get multiplexer instance
	var mux multiplexer.Multiplexer
	var err error
	if muxType == multiplexer.TypeAuto {
		mux, err = multiplexer.GetAuto()
	} else {
		mux, _, err = multiplexer.GetWithFallback(muxType)
	}
	if err != nil {
		return fmt.Errorf("no multiplexer available: %w", err)
	}

	// Load existing state
	state := loadControlAgentState(orchDir)

	// If --new flag, kill existing session
	if opts.New {
		if mux.HasSession(controlAgentSessionName) {
			_ = mux.KillSession(controlAgentSessionName)
		}
		if err := clearControlAgentState(orchDir); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to clear existing state: %v\n", err)
		}
		state = nil
	}

	// Liveness check: use multiplexer's has-session
	sessionExists := mux.HasSession(controlAgentSessionName)

	// Also verify state matches
	if state != nil && state.MultiplexerSession != "" {
		if !mux.HasSession(state.MultiplexerSession) {
			// Stale state, clear it
			_ = clearControlAgentState(orchDir)
			state = nil
			sessionExists = false
		}
	}

	if sessionExists {
		fmt.Fprintf(os.Stderr, "Attaching to existing control agent session (%s)\n", mux.Type())
		return attachToMuxSession(mux, controlAgentSessionName)
	}

	// Create new session
	return createMultiplexerSession(orchDir, agentType, mux)
}

func createMultiplexerSession(orchDir string, agentType agent.AgentType, mux multiplexer.Multiplexer) error {
	adapter, err := agent.GetAdapter(agentType)
	if err != nil {
		return fmt.Errorf("failed to get adapter: %w", err)
	}

	if !adapter.IsAvailable() {
		return fmt.Errorf("%s CLI is not available", agentType)
	}

	issuesRoot, err := getIssuesRoot()
	if err != nil {
		return fmt.Errorf("failed to get issues root: %w", err)
	}

	promptPath, err := writeControlPromptViaAPI(issuesRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to write control prompt: %v\n", err)
	}

	// Get model settings from config
	var modelName, modelVariant, profile string
	var extraArgs []string
	ctx := context.Background()
	api, apiErr := getAPI()
	if apiErr == nil {
		projectRoot, _ := getProjectRoot()
		cfg, cfgErr := api.GetConfig(ctx, projectRoot)
		if cfgErr == nil {
			modelName = cfg.ControlModel
			if modelName == "" {
				modelName = cfg.Model
			}
			modelVariant = cfg.ControlModelVariant
			if modelVariant == "" {
				modelVariant = cfg.ModelVariant
			}
			// Get extra args for control agent (use control-specific args)
			extraArgs = getControlExtraArgs(cfg, string(agentType))
			// profile not in global config, only in presets
		}
	}

	launchCfg := &agent.LaunchConfig{
		Type:         agentType,
		IssuesRoot:   issuesRoot,
		Prompt:       monitor.GetControlPromptInstruction(),
		Model:        modelName,
		ModelVariant: modelVariant,
		Profile:      profile,
		ExtraArgs:    extraArgs,
	}

	// Get launch command
	launchCmd, err := adapter.LaunchCommand(launchCfg)
	if err != nil {
		return fmt.Errorf("failed to create launch command: %w", err)
	}

	// Get working directory
	workDir, err := os.Getwd()
	if err != nil {
		workDir, _ = getProjectRoot()
	}

	// Create multiplexer session
	sessionCfg := &multiplexer.SessionConfig{
		SessionName: controlAgentSessionName,
		WorkDir:     workDir,
		Command:     launchCmd,
		WindowName:  "agent",
	}

	fmt.Fprintf(os.Stderr, "Creating control agent session (%s, %s)...\n", agentType, mux.Type())

	if err := mux.NewSession(sessionCfg); err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	// Save state
	state := &ControlAgentState{
		Backend:            string(agentType),
		CreatedAt:          time.Now(),
		MultiplexerSession: controlAgentSessionName,
		Multiplexer:        string(mux.Type()),
	}

	if err := saveControlAgentState(orchDir, state); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save state: %v\n", err)
	}

	if promptPath != "" {
		fmt.Fprintf(os.Stderr, "Control prompt: %s\n", promptPath)
	}
	fmt.Fprintf(os.Stderr, "Attaching to control agent...\n")

	return attachToMuxSession(mux, controlAgentSessionName)
}

func attachToMuxSession(mux multiplexer.Multiplexer, session string) error {
	if mux.IsInsideSession() {
		return mux.SwitchClient(session)
	}
	return mux.AttachSession(session)
}

func killControlAgent(orchDir string) error {
	state := loadControlAgentState(orchDir)

	if state == nil {
		// No state file means no control agent session is tracked.
		// Without state, we cannot reliably determine which multiplexer
		// was used or if a session exists. User should use multiplexer
		// commands directly if needed (e.g., tmux kill-session).
		fmt.Println("No control agent session found")
		return nil
	}

	// For opencode, just clear the state (opencode manages its own sessions)
	if state.Backend == "opencode" {
		if err := clearControlAgentState(orchDir); err != nil {
			return fmt.Errorf("failed to clear state: %w", err)
		}
		fmt.Println("Control agent session cleared")
		return nil
	}

	// For multiplexer-based sessions
	if state.MultiplexerSession != "" {
		muxType := multiplexer.TypeTmux
		if state.Multiplexer != "" {
			parsed, _ := multiplexer.ParseType(state.Multiplexer)
			if parsed != "" && parsed != multiplexer.TypeAuto {
				muxType = parsed
			}
		}

		mux, err := multiplexer.GetMultiplexer(muxType)
		if err == nil && mux.HasSession(state.MultiplexerSession) {
			if err := mux.KillSession(state.MultiplexerSession); err != nil {
				return fmt.Errorf("failed to kill session: %w", err)
			}
		}
	}

	// Clear state file
	if err := clearControlAgentState(orchDir); err != nil {
		return fmt.Errorf("failed to clear state: %w", err)
	}

	fmt.Println("Control agent session terminated")
	return nil
}

func loadControlAgentState(orchDir string) *ControlAgentState {
	if orchDir == "" {
		return nil
	}

	api, err := getAPI()
	if err != nil {
		return nil
	}

	path := filepath.Join(orchDir, controlAgentFileName)
	ctx := context.Background()
	data, err := api.ReadFile(ctx, path)
	if err != nil {
		return nil
	}

	var state ControlAgentState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil
	}

	return &state
}

func saveControlAgentState(orchDir string, state *ControlAgentState) error {
	if orchDir == "" {
		return nil
	}

	api, err := getAPI()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(orchDir, controlAgentFileName)
	ctx := context.Background()
	return api.WriteFile(ctx, path, data, 0600)
}

func clearControlAgentState(orchDir string) error {
	if orchDir == "" {
		return nil
	}
	path := filepath.Join(orchDir, controlAgentFileName)
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func writeControlPromptViaAPI(issuesRoot string) (string, error) {
	ctx := context.Background()
	api, err := getAPI()
	if err != nil {
		return "", fmt.Errorf("failed to get API: %w", err)
	}
	return monitor.WriteControlPromptFileViaAPI(ctx, api, issuesRoot)
}

func getControlExtraArgs(cfg *orchapi.Config, agentType string) []string {
	if cfg == nil {
		return nil
	}
	switch agentType {
	case "opencode":
		return cfg.OpenCode.ControlExtraArgs
	case "claude":
		return cfg.Claude.ControlExtraArgs
	case "codex":
		return cfg.Codex.ControlExtraArgs
	case "gemini":
		return cfg.Gemini.ControlExtraArgs
	}
	return nil
}
