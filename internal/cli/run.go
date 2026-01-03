package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/tmux"
	"github.com/spf13/cobra"
)

type runOptions struct {
	New            bool
	Reuse          bool
	RunID          string
	Agent          string
	AgentCmd       string
	AgentProfile   string
	BaseBranch     string
	Branch         string
	WorktreeDir    string
	RepoRoot       string
	Tmux           bool
	TmuxSession    string
	DryRun         bool
	NoPR           bool   // Skip PR instructions in prompt
	PromptTemplate string // Custom prompt template file
	PRTargetBranch string // Default PR target branch for prompt
	Model          string // Model for opencode (provider/model format)
	ModelVariant   string // Model variant (e.g., "max" for max thinking)
}

func newRunCmd() *cobra.Command {
	opts := &runOptions{}

	cmd := &cobra.Command{
		Use:   "run ISSUE_ID",
		Short: "Create and start a new run",
		Long: `Create a new run for an issue, set up a git worktree, and launch an agent.

The run will be started in a tmux session by default.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRun(args[0], opts)
		},
	}

	cmd.Flags().BoolVar(&opts.New, "new", true, "Always create a new run (default)")
	cmd.Flags().BoolVar(&opts.Reuse, "reuse", false, "Reuse the latest run if blocked or blocked_api")
	cmd.Flags().StringVar(&opts.RunID, "run-id", "", "Manually specify run ID")
	cmd.Flags().StringVar(&opts.Agent, "agent", "", "Agent type (claude|codex|gemini|opencode|custom)")
	cmd.Flags().StringVar(&opts.AgentCmd, "agent-cmd", "", "Custom agent command (when --agent=custom)")
	cmd.Flags().StringVar(&opts.AgentProfile, "profile", "", "Agent profile (e.g., claude --profile)")
	cmd.Flags().StringVar(&opts.BaseBranch, "base-branch", "", "Base branch for worktree")
	cmd.Flags().StringVar(&opts.Branch, "branch", "", "Branch name (default: issue/<ID>/run-<RUN_ID>)")
	cmd.Flags().StringVar(&opts.WorktreeDir, "worktree-dir", "", "Directory for worktrees (default: ~/.orch/worktrees)")
	cmd.Flags().StringVar(&opts.RepoRoot, "repo-root", "", "Git repository root (default: auto-detect)")
	cmd.Flags().BoolVar(&opts.Tmux, "tmux", true, "Run in tmux session")
	cmd.Flags().StringVar(&opts.TmuxSession, "tmux-session", "", "Tmux session name (default: run-<ISSUE>-<RUN>)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be done without doing it")
	cmd.Flags().BoolVar(&opts.NoPR, "no-pr", false, "Skip PR creation instructions in agent prompt")
	cmd.Flags().StringVar(&opts.PromptTemplate, "prompt-template", "", "Custom prompt template file")
	cmd.Flags().StringVar(&opts.Model, "model", "", "Model for opencode (provider/model format, e.g., anthropic/claude-opus-4-5)")
	cmd.Flags().StringVar(&opts.ModelVariant, "model-variant", "", "Model variant (e.g., 'max' for max thinking)")

	return cmd
}

type runResult struct {
	OK           bool   `json:"ok"`
	IssueID      string `json:"issue_id"`
	RunID        string `json:"run_id"`
	RunPath      string `json:"run_path"`
	Branch       string `json:"branch"`
	WorktreePath string `json:"worktree_path"`
	TmuxSession  string `json:"tmux_session"`
	Status       string `json:"status"`
	Error        string `json:"error,omitempty"`
}

func runRun(issueID string, opts *runOptions) error {
	st, err := getStore()
	if err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	// Apply config defaults for prompt options
	if err := applyPromptConfigDefaults(opts); err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	// Resolve issue first
	issue, err := st.ResolveIssue(issueID)
	if err != nil {
		return exitWithCode(fmt.Errorf("issue not found: %s", issueID), ExitIssueNotFound)
	}

	// Determine run ID
	runID := opts.RunID
	if runID == "" {
		if opts.Reuse {
			// Try to get latest run
			latestRun, err := st.GetLatestRun(issueID)
			if err == nil && (latestRun.Status == model.StatusBlocked || latestRun.Status == model.StatusBlockedAPI) {
				runID = latestRun.RunID
			}
		}
		if runID == "" {
			runID = model.GenerateRunID()
		}
	}

	// Determine branch name
	branch := opts.Branch
	if branch == "" {
		branch = model.GenerateBranchName(issueID, runID)
	}

	// Determine tmux session name
	tmuxSession := opts.TmuxSession
	if tmuxSession == "" {
		tmuxSession = model.GenerateTmuxSession(issueID, runID)
	}

	// Find repo root - use main repo root to handle running from inside worktrees
	repoRoot := opts.RepoRoot
	if repoRoot == "" {
		repoRoot, err = git.FindMainRepoRoot("")
		if err != nil {
			return exitWithCode(fmt.Errorf("could not find git repository: %w", err), ExitWorktreeError)
		}
	}

	// Compute worktree path (absolute to ensure correct directory regardless of cwd)
	worktreeName := model.GenerateWorktreeName(issueID, runID, opts.Agent)
	var worktreePath string
	if filepath.IsAbs(opts.WorktreeDir) {
		// Absolute path: use directly without joining with repoRoot
		worktreePath = filepath.Join(opts.WorktreeDir, issueID, worktreeName)
	} else {
		// Relative path: join with repoRoot
		worktreePath = filepath.Join(repoRoot, opts.WorktreeDir, issueID, worktreeName)
	}

	result := &runResult{
		OK:           true,
		IssueID:      issueID,
		RunID:        runID,
		Branch:       branch,
		WorktreePath: worktreePath,
		TmuxSession:  tmuxSession,
		Status:       string(model.StatusQueued),
	}

	// Dry run - just output what would happen
	if opts.DryRun {
		// Build the command that would be run (for display purposes)
		agentType, _ := agent.ParseAgentType(opts.Agent)
		adapter, _ := agent.GetAdapter(agentType)
		launchCfg := &agent.LaunchConfig{
			Type:      agentType,
			CustomCmd: opts.AgentCmd,
			WorkDir:   worktreePath,
			IssueID:   issueID,
			RunID:     runID,
			VaultPath: "",
			Branch:    branch,
			Prompt:    promptFileInstruction,
			Profile:   opts.AgentProfile,
		}
		agentCmd, _ := adapter.LaunchCommand(launchCfg)

		if globalOpts.JSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		}
		fmt.Printf("Would create run:\n")
		fmt.Printf("  Issue:     %s\n", issueID)
		fmt.Printf("  Run ID:    %s\n", runID)
		fmt.Printf("  Branch:    %s\n", branch)
		fmt.Printf("  Worktree:  %s\n", worktreePath)
		fmt.Printf("  Session:   %s\n", tmuxSession)
		fmt.Printf("  Command:   %s\n", agentCmd)
		return nil
	}

	// Create run document
	metadata := map[string]string{
		"agent": opts.Agent,
	}
	if opts.Model != "" {
		metadata["model"] = opts.Model
	}
	if opts.ModelVariant != "" {
		metadata["model_variant"] = opts.ModelVariant
	}
	run, err := st.CreateRun(issueID, runID, metadata)
	if err != nil {
		return exitWithCode(fmt.Errorf("failed to create run: %w", err), ExitInternalError)
	}
	result.RunPath = run.Path

	// Append initial status event
	if err := st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusQueued)); err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	// Create worktree
	worktreeResult, err := git.CreateWorktree(&git.WorktreeConfig{
		RepoRoot:    repoRoot,
		WorktreeDir: opts.WorktreeDir,
		IssueID:     issueID,
		RunID:       runID,
		Agent:       opts.Agent,
		BaseBranch:  opts.BaseBranch,
		Branch:      branch,
	})
	if err != nil {
		st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusFailed))
		return exitWithCode(fmt.Errorf("failed to create worktree: %w", err), ExitWorktreeError)
	}

	result.WorktreePath = worktreeResult.WorktreePath
	result.Branch = worktreeResult.Branch

	// Record artifacts
	st.AppendEvent(run.Ref(), model.NewArtifactEvent("worktree", map[string]string{
		"path": worktreeResult.WorktreePath,
	}))
	st.AppendEvent(run.Ref(), model.NewArtifactEvent("branch", map[string]string{
		"name": worktreeResult.Branch,
	}))

	// Get agent adapter
	agentType, err := agent.ParseAgentType(opts.Agent)
	if err != nil {
		return exitWithCode(err, ExitAgentError)
	}

	adapter, err := agent.GetAdapter(agentType)
	if err != nil {
		return exitWithCode(err, ExitAgentError)
	}

	if !adapter.IsAvailable() {
		return exitWithCode(fmt.Errorf("agent %s is not available", opts.Agent), ExitAgentError)
	}

	// Build agent launch config
	promptOpts := &promptOptions{
		NoPR:           opts.NoPR,
		PromptTemplate: opts.PromptTemplate,
		BaseBranch:     opts.BaseBranch,
		PRTargetBranch: opts.PRTargetBranch,
		VaultPath:      st.VaultPath(),
		IssuePath:      issue.Path,
	}
	agentPrompt := buildAgentPrompt(issue, promptOpts)
	promptPath := filepath.Join(worktreeResult.WorktreePath, promptFileName)
	if err := os.WriteFile(promptPath, []byte(agentPrompt), 0644); err != nil {
		return exitWithCode(fmt.Errorf("failed to write prompt file: %w", err), ExitInternalError)
	}
	launchCfg := &agent.LaunchConfig{
		Type:         agentType,
		CustomCmd:    opts.AgentCmd,
		WorkDir:      worktreeResult.WorktreePath,
		IssueID:      issueID,
		RunID:        runID,
		RunPath:      run.Path,
		VaultPath:    st.VaultPath(),
		Branch:       worktreeResult.Branch,
		Prompt:       promptFileInstruction,
		Profile:      opts.AgentProfile,
		Port:         4096, // Default port for HTTP-based agents (e.g., opencode)
		Model:        opts.Model,
		ModelVariant: opts.ModelVariant,
	}

	agentCmd, err := adapter.LaunchCommand(launchCfg)
	if err != nil {
		return exitWithCode(err, ExitAgentError)
	}

	// Update status to booting
	st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusBooting))

	if opts.Tmux {
		// Check if tmux is available
		if !tmux.IsTmuxAvailable() {
			return exitWithCode(fmt.Errorf("tmux is not available"), ExitTmuxError)
		}

		// For HTTP-based agents, check if a server is already running (reuse it)
		// We only need one opencode server - it can handle sessions for any project
		serverAlreadyRunning := false
		if adapter.PromptInjection() == agent.InjectionHTTP {
			// Check if any opencode server is already running
			foundPort := findRunningOpenCodeServer(launchCfg.Port, launchCfg.Port+100)
			if foundPort > 0 {
				serverAlreadyRunning = true
				launchCfg.Port = foundPort
				fmt.Fprintf(os.Stderr, "reusing existing opencode server on port %d\n", foundPort)
			} else {
				// No server running, find a free port
				freePort := findAvailablePort(launchCfg.Port, launchCfg.Port+100)
				if freePort == 0 {
					st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusFailed))
					return exitWithCode(fmt.Errorf("no available port found for opencode server"), ExitAgentError)
				}
				launchCfg.Port = freePort
				// Regenerate the agent command with the port
				agentCmd, err = adapter.LaunchCommand(launchCfg)
				if err != nil {
					return exitWithCode(err, ExitAgentError)
				}
			}
		}

		// Only create tmux session if server is not already running
		if !serverAlreadyRunning {
			// Build environment variables - merge adapter-specific vars with launch config vars
			env := launchCfg.Env()
			if opencodeAdapter, ok := adapter.(*agent.OpenCodeAdapter); ok {
				env = append(env, opencodeAdapter.Env()...)
			}

			// Create tmux session
			err = tmux.NewSession(&tmux.SessionConfig{
				SessionName: tmuxSession,
				WorkDir:     worktreeResult.WorktreePath,
				Command:     agentCmd,
				Env:         env,
			})
			if err != nil {
				st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusFailed))
				return exitWithCode(fmt.Errorf("failed to create tmux session: %w", err), ExitTmuxError)
			}

			// Record session artifact
			st.AppendEvent(run.Ref(), model.NewArtifactEvent("session", map[string]string{
				"name": tmuxSession,
			}))
		}

		// Handle prompt injection based on agent type
		switch adapter.PromptInjection() {
		case agent.InjectionTmux:
			// Send prompt via tmux send-keys
			if launchCfg.Prompt != "" {
				// Wait for the agent to be ready before sending the prompt
				if pattern := adapter.ReadyPattern(); pattern != "" {
					if err := tmux.WaitForReady(tmuxSession, pattern, 30*time.Second); err != nil {
						st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusFailed))
						return exitWithCode(fmt.Errorf("agent did not become ready: %w", err), ExitAgentError)
					}
				}
				if err := tmux.SendKeys(tmuxSession, launchCfg.Prompt); err != nil {
					st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusFailed))
					return exitWithCode(fmt.Errorf("failed to send prompt to session: %w", err), ExitTmuxError)
				}
			}

		case agent.InjectionHTTP:
			// For HTTP-based agents (e.g., opencode), send prompt via HTTP API
			if err := injectPromptViaHTTP(st, run, launchCfg); err != nil {
				st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusFailed))
				return exitWithCode(fmt.Errorf("failed to send prompt via HTTP: %w", err), ExitAgentError)
			}
		}

		// Record window ID only if we created a new tmux session
		if !serverAlreadyRunning {
			windowID := ""
			if windows, err := tmux.ListWindows(tmuxSession); err == nil {
				for _, window := range windows {
					if window.Index == 0 {
						windowID = window.ID
						break
					}
				}
			}
			if windowID != "" {
				st.AppendEvent(run.Ref(), model.NewArtifactEvent("window", map[string]string{
					"id": windowID,
				}))
			}
		}
	}

	// Update status to running
	st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusRunning))
	result.Status = string(model.StatusRunning)

	// Output result
	if globalOpts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	if !globalOpts.Quiet {
		fmt.Printf("Run started: %s#%s\n", issueID, runID)
		fmt.Printf("  Branch:   %s\n", result.Branch)
		fmt.Printf("  Worktree: %s\n", result.WorktreePath)
		if opts.Tmux {
			fmt.Printf("  Session:  %s\n", result.TmuxSession)
			fmt.Printf("\nAttach with: orch attach %s#%s\n", issueID, runID)
		}
	}

	return nil
}

type promptOptions struct {
	NoPR           bool
	PromptTemplate string
	BaseBranch     string
	PRTargetBranch string
	VaultPath      string
	IssuePath      string
}

const (
	promptFileName        = "ORCH_PROMPT.md"
	promptFileInstruction = "ultrathink Please read '" + promptFileName + "' in the current directory and follow the instructions found there."
	defaultPRTargetBranch = "main"
)

const defaultPromptTemplate = `## Context

This file (ORCH_PROMPT.md) is auto-generated by orch. The original issue is at:
- Vault: {{.VaultPath}}
- Issue file: {{.IssuePath}}

## Issue

<issue>
{{.Body}}
</issue>

## Instructions

- Implement the changes described in the issue above
- Run tests to verify your changes work correctly
{{- if not .NoPR}}
- When complete, create a pull request targeting ` + "`" + `{{.PRTargetBranch}}` + "`" + `:
  - Title should summarize the change
  - Body should reference issue: {{.IssueID}}
  - Include a summary of changes made
{{- end}}
`

func applyPromptDefaults(opts *promptOptions) *promptOptions {
	if opts == nil {
		opts = &promptOptions{}
	}
	opts.BaseBranch = strings.TrimSpace(opts.BaseBranch)
	opts.PRTargetBranch = strings.TrimSpace(opts.PRTargetBranch)
	if opts.PRTargetBranch == "" {
		opts.PRTargetBranch = opts.BaseBranch
	}
	if opts.PRTargetBranch == "" {
		opts.PRTargetBranch = defaultPRTargetBranch
	}
	return opts
}

func buildAgentPrompt(issue *model.Issue, opts *promptOptions) string {
	opts = applyPromptDefaults(opts)

	// If custom template provided, try to load it
	if opts.PromptTemplate != "" {
		content, err := os.ReadFile(opts.PromptTemplate)
		if err == nil {
			return executeTemplate(string(content), issue, opts)
		}
		// Fall back to default if template file not found
	}

	return executeTemplate(defaultPromptTemplate, issue, opts)
}

// executeTemplate executes a prompt template with issue data
func executeTemplate(tmplStr string, issue *model.Issue, opts *promptOptions) string {
	opts = applyPromptDefaults(opts)

	data := map[string]interface{}{
		"IssueID":        issue.ID,
		"Title":          issue.Title,
		"Body":           issue.Body,
		"NoPR":           opts.NoPR,
		"BaseBranch":     opts.BaseBranch,
		"PRTargetBranch": opts.PRTargetBranch,
		"VaultPath":      opts.VaultPath,
		"IssuePath":      opts.IssuePath,
	}

	// Use text/template for proper template execution
	tmpl, err := template.New("prompt").Parse(tmplStr)
	if err != nil {
		// Fallback to simple format if template parsing fails
		return buildSimplePrompt(issue, opts)
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return buildSimplePrompt(issue, opts)
	}

	return buf.String()
}

// buildSimplePrompt creates a basic prompt without template processing
func buildSimplePrompt(issue *model.Issue, opts *promptOptions) string {
	opts = applyPromptDefaults(opts)

	prompt := fmt.Sprintf("You are working on issue: %s\n\n", issue.ID)
	if issue.Title != "" {
		prompt += fmt.Sprintf("Title: %s\n\n", issue.Title)
	}
	if issue.Body != "" {
		prompt += fmt.Sprintf("Description:\n%s\n", issue.Body)
	}
	if !opts.NoPR {
		prompt += "\nInstructions:\n"
		prompt += "- Implement the changes described in the issue above\n"
		prompt += "- Run tests to verify your changes work correctly\n"
		prompt += fmt.Sprintf("- When complete, create a pull request targeting `%s`:\n", opts.PRTargetBranch)
		prompt += "  - Title should summarize the change\n"
		prompt += fmt.Sprintf("  - Body should reference issue: %s\n", issue.ID)
		prompt += "  - Include a summary of changes made\n"
	}
	return prompt
}

// applyPromptConfigDefaults applies config file defaults for prompt options
// Command-line flags take precedence over config values
func applyPromptConfigDefaults(opts *runOptions) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Apply config defaults for core run options
	// Only apply if command-line flag wasn't explicitly set (empty string = not set)

	// BaseBranch: use config value if flag not provided, fallback to "main"
	if opts.BaseBranch == "" {
		if cfg.BaseBranch != "" {
			opts.BaseBranch = cfg.BaseBranch
		} else {
			opts.BaseBranch = "main"
		}
	}

	// Agent: use config value if flag not provided, fallback to "claude"
	if opts.Agent == "" {
		if cfg.Agent != "" {
			opts.Agent = cfg.Agent
		} else {
			opts.Agent = "claude"
		}
	}

	// WorktreeDir: use config value if flag not provided, fallback to "~/.orch/worktrees"
	if opts.WorktreeDir == "" {
		if cfg.WorktreeDir != "" {
			opts.WorktreeDir = cfg.WorktreeDir
		} else {
			// Default to ~/.orch/worktrees (outside repo, keeps repo clean)
			home, _ := os.UserHomeDir()
			opts.WorktreeDir = filepath.Join(home, ".orch", "worktrees")
		}
	}

	// PromptTemplate: use config value if flag not provided
	if opts.PromptTemplate == "" && cfg.PromptTemplate != "" {
		opts.PromptTemplate = cfg.PromptTemplate
	}

	if opts.PRTargetBranch == "" && cfg.PRTargetBranch != "" {
		opts.PRTargetBranch = cfg.PRTargetBranch
	}

	// For NoPR: config sets the default, but --no-pr flag overrides
	// Since bool flags default to false, we apply config value if it's true
	if cfg.NoPR && !opts.NoPR {
		opts.NoPR = cfg.NoPR
	}

	if opts.Model == "" && cfg.Model != "" {
		opts.Model = cfg.Model
	}
	if opts.ModelVariant == "" && cfg.ModelVariant != "" {
		opts.ModelVariant = cfg.ModelVariant
	}

	return nil
}

func exitWithCode(err error, code int) error {
	if globalOpts.JSON {
		result := &runResult{
			OK:    false,
			Error: err.Error(),
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(result)
	} else {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(code)
	return err
}

// injectPromptViaHTTP sends the prompt to an HTTP-based agent (e.g., opencode)
func injectPromptViaHTTP(st interface {
	AppendEvent(*model.RunRef, *model.Event) error
}, run *model.Run, cfg *agent.LaunchConfig) error {
	port := cfg.Port
	if port == 0 {
		port = 4096 // Default opencode port
	}

	client := agent.NewOpenCodeClient(port)

	// Wait for server to be healthy
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := client.WaitForHealthy(ctx, 60*time.Second); err != nil {
		return fmt.Errorf("server did not become healthy: %w", err)
	}

	// Record server port as artifact
	st.AppendEvent(run.Ref(), model.NewArtifactEvent("server", map[string]string{
		"port": fmt.Sprintf("%d", port),
	}))

	// Create a new session in the worktree directory
	session, err := client.CreateSession(ctx, fmt.Sprintf("%s#%s", run.IssueID, run.RunID), cfg.WorkDir)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	// Record session ID as artifact for resume capability
	st.AppendEvent(run.Ref(), model.NewArtifactEvent("opencode_session", map[string]string{
		"id": session.ID,
	}))

	// Build model reference from config
	var modelRef *agent.ModelRef
	if cfg.Model != "" {
		modelRef = agent.ParseModel(cfg.Model)
	}

	// Send the prompt asynchronously (don't wait for completion)
	// Pass directory for proper project context, model for model selection, and variant for thinking mode
	if err := client.SendMessageAsync(ctx, session.ID, cfg.Prompt, cfg.WorkDir, modelRef, cfg.ModelVariant); err != nil {
		return fmt.Errorf("failed to send prompt: %w", err)
	}

	return nil
}

// findAvailablePort finds an available port in the given range
// Returns 0 if no port is available
func findAvailablePort(start, end int) int {
	for port := start; port <= end; port++ {
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		listener, err := net.Listen("tcp", addr)
		if err == nil {
			listener.Close()
			return port
		}
	}
	return 0
}

// findRunningOpenCodeServer finds a running opencode server in the given port range.
// Returns the port number if found, 0 if no server is running.
func findRunningOpenCodeServer(start, end int) int {
	for port := start; port <= end; port++ {
		client := agent.NewOpenCodeClient(port)
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		running := client.IsServerRunning(ctx)
		cancel()
		if running {
			return port
		}
	}
	return 0
}
