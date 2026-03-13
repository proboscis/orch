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

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/orchapi"
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
	WorktreeSet    bool
	Tmux           bool
	SessionName    string
	Multiplexer    string
	DryRun         bool
	NoPR           bool
	PromptTemplate string
	PRTargetBranch string
	Model          string
	ModelVariant   string
	On             string
	Preset         string
	Verbose        bool
}

func newRunCmd() *cobra.Command {
	opts := &runOptions{}

	cmd := &cobra.Command{
		Use:   "run ISSUE_ID",
		Short: "Create and start a new run",
		Long: `Create a new run for an issue, set up a git worktree, and launch an agent.

The run will be started in a tmux session by default.

Debug output can be enabled with --verbose, --log-level debug, or ORCH_DEBUG=1.`,
		Args: cobra.ExactArgs(1),
		PreRun: func(cmd *cobra.Command, args []string) {
			if opts.Verbose {
				globalOpts.LogLevel = "debug"
			}
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRun(args[0], opts)
		},
	}

	cmd.Flags().BoolVar(&opts.New, "new", true, "Always create a new run (default)")
	cmd.Flags().BoolVar(&opts.Reuse, "reuse", false, "Reuse the latest run if waiting or rate_limited")
	cmd.Flags().StringVar(&opts.RunID, "run-id", "", "Manually specify run ID")
	cmd.Flags().StringVar(&opts.Agent, "agent", "", "Agent type (claude|codex|gemini|opencode|custom)")
	cmd.Flags().StringVar(&opts.AgentCmd, "agent-cmd", "", "Custom agent command (when --agent=custom)")
	cmd.Flags().StringVar(&opts.AgentProfile, "profile", "", "Agent profile (e.g., claude --profile)")
	cmd.Flags().StringVar(&opts.BaseBranch, "base-branch", "", "Base branch for worktree")
	cmd.Flags().StringVar(&opts.Branch, "branch", "", "Branch name (default: issue/<ID>/run-<RUN_ID>)")
	cmd.Flags().BoolVar(&opts.Tmux, "tmux", true, "Run in terminal multiplexer session")
	cmd.Flags().StringVar(&opts.SessionName, "session-name", "", "Session name (default: run-<ISSUE>-<RUN>)")
	cmd.Flags().StringVar(&opts.Multiplexer, "multiplexer", "", "Terminal multiplexer (tmux|zellij)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be done without doing it")
	cmd.Flags().BoolVar(&opts.NoPR, "no-pr", false, "Skip PR creation instructions in agent prompt")
	cmd.Flags().StringVar(&opts.PromptTemplate, "prompt-template", "", "Custom prompt template file")
	cmd.Flags().StringVar(&opts.Model, "model", "", "Model for opencode (provider/model format, e.g., anthropic/claude-opus-4-5)")
	cmd.Flags().StringVar(&opts.ModelVariant, "model-variant", "", "Model variant (e.g., 'max' for max thinking)")
	cmd.Flags().StringVar(&opts.On, "on", "", "Target name from config.targets for remote execution")
	cmd.Flags().StringVar(&opts.Preset, "preset", "", "Named preset from config (e.g., 'opus:high', 'gpt5.2-codex:xhigh')")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "Enable debug output for troubleshooting")

	return cmd
}

type runResult struct {
	OK           bool   `json:"ok"`
	IssueID      string `json:"issue_id"`
	RunID        string `json:"run_id"`
	RunPath      string `json:"run_path"`
	Branch       string `json:"branch"`
	WorktreePath string `json:"worktree_path"`
	SessionName  string `json:"session_name"`
	Status       string `json:"status"`
	Error        string `json:"error,omitempty"`
}

func runRun(issueID string, opts *runOptions) error {
	if model.IsGitHubIssueID(issueID) {
		issueID = model.NormalizeGitHubIssueID(issueID)
	}

	_, _, rootErr := getProjectRootWithSource()
	remoteMode := strings.TrimSpace(getRemoteAddr()) != ""
	if rootErr != nil {
		if !remoteMode {
			return exitWithCode(fmt.Errorf("project scope required: run from repository root or set --project/ORCH_PROJECT"), ExitWorktreeError)
		}
	}

	ctx := context.Background()
	api, err := getAPI()
	if err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	cfg, err := api.GetConfig(ctx)
	if err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	applyConfigDefaults(opts, cfg, remoteMode)

	resp, err := api.StartRun(ctx, &orchapi.StartRunRequest{
		IssueID:        issueID,
		RunID:          opts.RunID,
		Agent:          opts.Agent,
		AgentCmd:       opts.AgentCmd,
		AgentProfile:   opts.AgentProfile,
		Model:          opts.Model,
		ModelVariant:   opts.ModelVariant,
		Preset:         opts.Preset,
		BaseBranch:     opts.BaseBranch,
		Branch:         opts.Branch,
		WorktreeDir:    opts.WorktreeDir,
		NoPR:           opts.NoPR,
		PromptTemplate: opts.PromptTemplate,
		PRTargetBranch: opts.PRTargetBranch,
		DryRun:         opts.DryRun,
		Reuse:          opts.Reuse,
		Multiplexer:    opts.Multiplexer,
		Target:         opts.On,
	})
	if err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	result := &runResult{
		OK:           true,
		IssueID:      issueID,
		RunID:        resp.RunID,
		Branch:       resp.Branch,
		WorktreePath: resp.WorktreePath,
		SessionName:  resp.SessionName,
		Status:       resp.Status,
	}

	if opts.DryRun {
		if globalOpts.JSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		}
		fmt.Printf("Would create run:\n")
		fmt.Printf("  Issue:     %s\n", issueID)
		fmt.Printf("  Run ID:    %s\n", resp.RunID)
		fmt.Printf("  Branch:    %s\n", resp.Branch)
		fmt.Printf("  Worktree:  %s\n", resp.WorktreePath)
		fmt.Printf("  Session:   %s\n", resp.SessionName)
		return nil
	}

	if globalOpts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	if !globalOpts.Quiet {
		fmt.Printf("Run started: %s#%s\n", issueID, resp.RunID)
		fmt.Printf("  Branch:   %s\n", resp.Branch)
		fmt.Printf("  Worktree: %s\n", resp.WorktreePath)
		if resp.SessionName != "" {
			fmt.Printf("  Session:  %s\n", resp.SessionName)
			fmt.Printf("\nAttach with: orch attach %s#%s\n", issueID, resp.RunID)
		}
	}

	return nil
}

type promptOptions struct {
	NoPR           bool
	PromptTemplate string
	BaseBranch     string
	PRTargetBranch string
	IssuesRoot     string
	IssuePath      string
}

const (
	promptFileName        = "ORCH_PROMPT.md"
	promptFileInstruction = "ultrathink Please read '" + promptFileName + "' in the current directory and follow the instructions found there."
	defaultPRTargetBranch = "main"
)

func renderInitialPromptTemplate(tmplStr string, issue *model.Issue) string {
	if tmplStr == "" {
		return promptFileInstruction
	}

	issueContent := issue.Title
	if issue.Body != "" {
		if issueContent != "" {
			issueContent += "\n\n"
		}
		issueContent += issue.Body
	}

	result := strings.ReplaceAll(tmplStr, "{{issue}}", issueContent)
	result = strings.ReplaceAll(result, "{{issue_id}}", issue.ID)
	result = strings.ReplaceAll(result, "{{issue_title}}", issue.Title)

	return result
}

const defaultPromptTemplate = `## Context

This file (ORCH_PROMPT.md) is auto-generated by orch. The original issue is at:
- Issues path: {{.IssuesRoot}}
- Issue file: {{.IssuePath}}

## Issue

<issue>
{{.Body}}
</issue>

## Instructions

- Read the issue carefully, especially the **Acceptance Criteria** section
- Implement the changes described in the issue
- **CRITICAL: Verify EACH acceptance criterion by actually running the code**
  - If the issue requires outputs (CSV, reports, etc.), run the entrypoint and confirm outputs exist
  - Don't just check that code compiles - verify it produces correct results
- Run tests to verify your changes work correctly
{{- if not .NoPR}}
- When complete, create a pull request targeting ` + "`" + `{{.PRTargetBranch}}` + "`" + ` with:
  - Evidence that each acceptance criterion is met (command outputs, file listings, etc.)
  - Summary of changes made
  - Reference to the issue: {{.IssueID}}
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

	if opts.PromptTemplate != "" {
		if api, err := getAPIForListing(); err == nil {
			ctx := context.Background()
			if content, err := api.ReadFile(ctx, opts.PromptTemplate); err == nil {
				return executeTemplate(string(content), issue, opts)
			}
		}
	}

	return executeTemplate(defaultPromptTemplate, issue, opts)
}

func executeTemplate(tmplStr string, issue *model.Issue, opts *promptOptions) string {
	opts = applyPromptDefaults(opts)

	data := map[string]interface{}{
		"IssueID":        issue.ID,
		"Title":          issue.Title,
		"Body":           issue.Body,
		"NoPR":           opts.NoPR,
		"BaseBranch":     opts.BaseBranch,
		"PRTargetBranch": opts.PRTargetBranch,
		"IssuesRoot":     opts.IssuesRoot,
		"IssuePath":      opts.IssuePath,
	}

	tmpl, err := template.New("prompt").Parse(tmplStr)
	if err != nil {
		return buildSimplePrompt(issue, opts)
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return buildSimplePrompt(issue, opts)
	}

	return buf.String()
}

func buildSimplePrompt(issue *model.Issue, opts *promptOptions) string {
	opts = applyPromptDefaults(opts)

	prompt := fmt.Sprintf("You are working on issue: %s\n\n", issue.ID)
	if issue.Title != "" {
		prompt += fmt.Sprintf("Title: %s\n\n", issue.Title)
	}
	if issue.Body != "" {
		prompt += fmt.Sprintf("Description:\n%s\n", issue.Body)
	}
	prompt += "\nInstructions:\n"
	prompt += "- Read the issue carefully, especially the **Acceptance Criteria** section\n"
	prompt += "- Implement the changes described in the issue\n"
	prompt += "- **CRITICAL: Verify EACH acceptance criterion by actually running the code**\n"
	prompt += "  - If the issue requires outputs (CSV, reports, etc.), run the entrypoint and confirm outputs exist\n"
	prompt += "  - Don't just check that code compiles - verify it produces correct results\n"
	prompt += "- Run tests to verify your changes work correctly\n"
	if !opts.NoPR {
		prompt += fmt.Sprintf("- When complete, create a pull request targeting `%s` with:\n", opts.PRTargetBranch)
		prompt += "  - Evidence that each acceptance criterion is met (command outputs, file listings, etc.)\n"
		prompt += "  - Summary of changes made\n"
		prompt += fmt.Sprintf("  - Reference to the issue: %s\n", issue.ID)
	}
	return prompt
}

func applyConfigDefaults(opts *runOptions, cfg *orchapi.Config, remoteMode bool) {
	agentExplicit := opts.Agent != ""
	profileExplicit := opts.AgentProfile != ""

	presetName := opts.Preset
	if presetName == "" && cfg.DefaultPreset != "" {
		presetName = cfg.DefaultPreset
		opts.Preset = presetName
	}

	// Resolve agent and profile from preset in CLI since they affect
	// CLI-side display; model and variant are resolved by the daemon.
	if presetName != "" {
		preset := findPreset(cfg.Presets, presetName)
		if preset != nil {
			if !agentExplicit {
				opts.Agent = effectiveBackend(preset)
			}
			if !profileExplicit && preset.Profile != "" {
				opts.AgentProfile = preset.Profile
			}
		}
	}

	if opts.BaseBranch == "" {
		if cfg.BaseBranch != "" {
			opts.BaseBranch = cfg.BaseBranch
		} else {
			opts.BaseBranch = "main"
		}
	}

	if opts.Agent == "" {
		if cfg.Agent != "" {
			opts.Agent = cfg.Agent
		} else {
			opts.Agent = "claude"
		}
	}

	if opts.Multiplexer == "" {
		opts.Multiplexer = getAgentMultiplexer(cfg)
	}

	if opts.WorktreeDir == "" && !opts.WorktreeSet {
		if cfg.WorktreeDir != "" {
			opts.WorktreeDir = cfg.WorktreeDir
		} else if !remoteMode {
			home, _ := os.UserHomeDir()
			opts.WorktreeDir = filepath.Join(home, ".orch", "worktrees")
		}
	}

	if opts.PromptTemplate == "" && cfg.PromptTemplate != "" {
		opts.PromptTemplate = cfg.PromptTemplate
	}

	if opts.PRTargetBranch == "" && cfg.PRTargetBranch != "" {
		opts.PRTargetBranch = cfg.PRTargetBranch
	}

	if cfg.NoPR && !opts.NoPR {
		opts.NoPR = cfg.NoPR
	}

	// Model and variant resolution is handled by the daemon via
	// cfg.ResolveModelAndVariant — CLI just forwards flags as-is.
}

func findPreset(presets []orchapi.Preset, name string) *orchapi.Preset {
	for i := range presets {
		if presets[i].Name == name {
			return &presets[i]
		}
	}
	return nil
}

func effectiveBackend(preset *orchapi.Preset) string {
	if preset.Backend != "" {
		return preset.Backend
	}
	return "opencode"
}

func getAgentMultiplexer(cfg *orchapi.Config) string {
	if cfg.AgentMultiplexer != "" {
		return cfg.AgentMultiplexer
	}
	if cfg.Multiplexer != "" {
		return cfg.Multiplexer
	}
	return "tmux"
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
