package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/monitor"
	"github.com/s22625/orch/internal/multiplexer"
	"github.com/s22625/orch/internal/store"
	"github.com/spf13/cobra"
)

const (
	controlAgentFileName    = "control-agent.json"
	controlAgentSessionName = "orch-control-agent"
)

// ControlAgentState persists state about the control agent session.
type ControlAgentState struct {
	SessionID   string    `json:"session_id"`
	Backend     string    `json:"backend"`
	CreatedAt   time.Time `json:"created_at"`
	TmuxSession string    `json:"tmux_session"`
	Port        int       `json:"port,omitempty"`
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

Examples:
  orch agent                    # Attach to existing or create new
  orch agent --new              # Force create a new session
  orch agent --backend claude   # Use claude backend
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
	mux := multiplexer.GetDefault()

	// Handle --kill flag
	if opts.Kill {
		return killControlAgent(orchDir, mux)
	}

	// If --new flag, clear existing session state first
	if opts.New {
		if err := clearControlAgentState(orchDir); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to clear existing state: %v\n", err)
		}
		// Also kill existing tmux session if present
		if mux.HasSession(controlAgentSessionName) {
			_ = mux.KillSession(controlAgentSessionName)
		}
	}

	// Load existing state
	state := loadControlAgentState(orchDir)

	// Check if existing session is still alive
	if state != nil && mux.HasSession(state.TmuxSession) {
		fmt.Fprintf(os.Stderr, "Attaching to existing control agent (session: %s)\n", state.TmuxSession)
		return attachToSession(mux, state.TmuxSession)
	}

	// Create new session
	return createControlAgent(orchDir, opts, mux, projectRoot)
}

func createControlAgent(orchDir string, opts *agentOptions, mux multiplexer.Multiplexer, projectRoot string) error {
	// Determine backend
	backend := opts.Backend
	if backend == "" {
		cfg, err := config.Load()
		if err == nil {
			if cfg.ControlAgent != "" {
				backend = cfg.ControlAgent
			} else if cfg.Agent != "" {
				backend = cfg.Agent
			}
		}
	}
	if backend == "" {
		backend = "opencode"
	}

	// Validate backend
	agentType, err := agent.ParseAgentType(backend)
	if err != nil {
		return fmt.Errorf("invalid backend: %w", err)
	}

	adapter, err := agent.GetAdapter(agentType)
	if err != nil {
		return fmt.Errorf("failed to get adapter for %s: %w", backend, err)
	}

	if !adapter.IsAvailable() {
		return fmt.Errorf("%s CLI is not available", backend)
	}

	// Get store for control prompt
	st, err := getStore()
	if err != nil {
		return fmt.Errorf("failed to get store: %w", err)
	}

	// Write control prompt file using monitor's function
	promptPath, err := writeControlPromptForAgent(st)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to write control prompt: %v\n", err)
	}

	// Determine model settings from config
	var modelName, modelVariant string
	cfg, cfgErr := config.Load()
	if cfgErr == nil {
		modelName = cfg.ControlModel
		if modelName == "" {
			modelName = cfg.Model
		}
		modelVariant = cfg.ControlModelVariant
		if modelVariant == "" {
			modelVariant = cfg.ModelVariant
		}
	}

	// Prepare launch config
	port := 4096
	launchCfg := &agent.LaunchConfig{
		Type:            agentType,
		IssuesRoot:      st.RootPath(),
		Prompt:          monitor.GetControlPromptInstruction(),
		ContinueSession: true,
		Port:            port,
		Model:           modelName,
		ModelVariant:    modelVariant,
	}

	// Get launch command
	launchCmd, err := adapter.LaunchCommand(launchCfg)
	if err != nil {
		return fmt.Errorf("failed to create launch command: %w", err)
	}

	// Get working directory (project root)
	workDir, err := os.Getwd()
	if err != nil {
		workDir = projectRoot
	}

	// Create tmux session
	sessionCfg := &multiplexer.SessionConfig{
		SessionName: controlAgentSessionName,
		WorkDir:     workDir,
		Command:     launchCmd,
		WindowName:  "agent",
	}

	fmt.Fprintf(os.Stderr, "Creating control agent session (%s)...\n", backend)

	if err := mux.NewSession(sessionCfg); err != nil {
		return fmt.Errorf("failed to create tmux session: %w", err)
	}

	// Save state
	state := &ControlAgentState{
		Backend:     backend,
		CreatedAt:   time.Now(),
		TmuxSession: controlAgentSessionName,
		Port:        port,
	}

	// For opencode, we need to get/create the session via HTTP
	if agentType == agent.AgentOpenCode {
		state.SessionID = getOrCreateOpenCodeSession(orchDir, port)
	}

	if err := saveControlAgentState(orchDir, state); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save state: %v\n", err)
	}

	// Attach to session
	if promptPath != "" {
		fmt.Fprintf(os.Stderr, "Control prompt: %s\n", promptPath)
	}
	fmt.Fprintf(os.Stderr, "Attaching to control agent...\n")

	return attachToSession(mux, controlAgentSessionName)
}

func attachToSession(mux multiplexer.Multiplexer, session string) error {
	if mux.IsInsideSession() {
		return mux.SwitchClient(session)
	}
	return mux.AttachSession(session)
}

func killControlAgent(orchDir string, mux multiplexer.Multiplexer) error {
	state := loadControlAgentState(orchDir)
	if state == nil {
		// Still try to kill session by name
		if mux.HasSession(controlAgentSessionName) {
			if err := mux.KillSession(controlAgentSessionName); err != nil {
				return fmt.Errorf("failed to kill session: %w", err)
			}
			fmt.Println("Control agent session terminated")
			return nil
		}
		fmt.Println("No control agent session found")
		return nil
	}

	sessionName := state.TmuxSession
	if sessionName == "" {
		sessionName = controlAgentSessionName
	}

	if mux.HasSession(sessionName) {
		if err := mux.KillSession(sessionName); err != nil {
			return fmt.Errorf("failed to kill session: %w", err)
		}
	}

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

	path := filepath.Join(orchDir, controlAgentFileName)
	data, err := os.ReadFile(path)
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

	if err := os.MkdirAll(orchDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(orchDir, controlAgentFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}

	return os.Rename(tmp, path)
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

func getOrCreateOpenCodeSession(orchDir string, port int) string {
	client := agent.NewOpenCodeClient(port)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Wait for server to be healthy
	if err := client.WaitForHealthy(ctx, 30*time.Second); err != nil {
		return ""
	}

	// Check if we have an existing stored session for this opencode instance
	stored := monitor.LoadControlSession(orchDir)
	if stored != nil && stored.SessionID != "" {
		// Verify session still exists
		cwd, _ := os.Getwd()
		session, err := client.GetSession(ctx, stored.SessionID, cwd)
		if err == nil && session != nil {
			return stored.SessionID
		}
	}

	// Create new session
	cwd, _ := os.Getwd()
	session, err := client.CreateSession(ctx, "control-agent", cwd)
	if err != nil {
		return ""
	}

	// Also save to the monitor-compatible file for compatibility
	_ = monitor.SaveControlSession(orchDir, &monitor.ControlSession{
		SessionID: session.ID,
		Port:      port,
	})

	return session.ID
}

// writeControlPromptForAgent writes the control prompt file using the monitor package
func writeControlPromptForAgent(st store.Store) (string, error) {
	return monitor.WriteControlPromptFile(st)
}
