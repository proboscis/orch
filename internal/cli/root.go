package cli

import (
	"fmt"
	"os"
	"regexp"

	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
	"github.com/s22625/orch/internal/store/file"
	"github.com/spf13/cobra"
)

// Exit codes as per spec
const (
	ExitOK               = 0
	ExitIssueNotFound    = 2
	ExitWorktreeError    = 3
	ExitTmuxError        = 4
	ExitAgentError       = 5
	ExitRunNotFound      = 6
	ExitQuestionNotFound = 7
	ExitInternalError    = 10
)

// GlobalOptions holds options shared across all commands
type GlobalOptions struct {
	IssuesRoot  string
	ProjectRoot string
	Backend     string
	JSON        bool
	TSV         bool
	Quiet       bool
	LogLevel    string
}

var globalOpts = &GlobalOptions{}

var noDaemonCommands = map[string]bool{
	"show":       true,
	"daemon":     true,
	"run":        true,
	"list":       true,
	"kill":       true,
	"status":     true,
	"repair":     true,
	"delete":     true,
	"help":       true,
	"completion": true,
	"models":     true,
	"notify":     true,
	"log":        true,
	"debug":      true,
	"query":      true,
	"q":          true,
	"schema":     true,
	"tutorial":   true,
}
// rootCmd represents the base command
var rootCmd = &cobra.Command{
	Use:   "orch",
	Short: "Orchestrator for multiple LLM CLIs",
	Long: `orch is an orchestrator for managing multiple LLM CLIs (claude/codex/gemini)
using a unified vocabulary of issue/run/event.

It operates non-interactively by default, using events to track state
and questions to handle human input requirements.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Auto-start daemon for most commands
		if !noDaemonCommands[cmd.Name()] {
			ensureDaemon()
		}
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&globalOpts.ProjectRoot, "project-root", "", "Path to project root where .orch/ lives (or set ORCH_PROJECT_ROOT)")
	rootCmd.PersistentFlags().StringVar(&globalOpts.IssuesRoot, "issues-root", "", "Path to issues root for file-based issues (or set ORCH_ISSUES_ROOT)")

	rootCmd.PersistentFlags().StringVar(&globalOpts.Backend, "backend", "file", "Backend type (file|github|linear)")
	rootCmd.PersistentFlags().BoolVar(&globalOpts.JSON, "json", false, "Output in JSON format")
	rootCmd.PersistentFlags().BoolVar(&globalOpts.TSV, "tsv", false, "Output in TSV format (for fzf)")
	rootCmd.PersistentFlags().BoolVar(&globalOpts.Quiet, "quiet", false, "Suppress human-readable output")
	rootCmd.PersistentFlags().StringVar(&globalOpts.LogLevel, "log-level", "warn", "Log level (error|warn|info|debug)")

	// Add subcommands
	rootCmd.AddCommand(newIssueCmd())
	rootCmd.AddCommand(newPsCmd())
	rootCmd.AddCommand(newRunCmd())
	rootCmd.AddCommand(newContinueCmd())
	rootCmd.AddCommand(newShowCmd())
	rootCmd.AddCommand(newAttachCmd())
	rootCmd.AddCommand(newTickCmd())
	rootCmd.AddCommand(newOpenCmd())
	rootCmd.AddCommand(newStopCmd())
	rootCmd.AddCommand(newMonitorCmd())
	rootCmd.AddCommand(newResolveCmd())
	rootCmd.AddCommand(newDaemonCmd())
	rootCmd.AddCommand(newDaemonRestartCmd())
	rootCmd.AddCommand(newRepairCmd())
	rootCmd.AddCommand(newDeleteCmd())
	rootCmd.AddCommand(newExecCmd())
	rootCmd.AddCommand(newSendCmd())
	rootCmd.AddCommand(newCaptureCmd())
	rootCmd.AddCommand(newCaptureAllCmd())
	rootCmd.AddCommand(newModelsCmd())
	rootCmd.AddCommand(newNotifyCmd())
	rootCmd.AddCommand(newLogCmd())
	rootCmd.AddCommand(newDebugCmd())
	rootCmd.AddCommand(newQueryCmd())
	rootCmd.AddCommand(newSchemaCmd())
	rootCmd.AddCommand(newTutorialCmd())
}

// Execute runs the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(ExitInternalError)
	}
}

// getIssuesRoot returns the issues root path from flags, environment, or config files
// Precedence: --issues-root flag > issues.path config > default XDG path (~/.local/share/orch/<repo>)
func getIssuesRoot() (string, error) {
	if globalOpts.IssuesRoot != "" {
		return config.ExpandPath(globalOpts.IssuesRoot, ""), nil
	}

	cfg, err := config.Load()
	if err != nil {
		return "", err
	}

	if issuesPath := cfg.GetIssuesPath(); issuesPath != "" {
		return issuesPath, nil
	}

	return "", fmt.Errorf("issues root not specified (use --issues-root or set issues.path in .orch/config.yaml)")
}

// getProjectRoot returns the project root directory (where .orch/ lives).
// Precedence: --project-root flag > ORCH_PROJECT_ROOT > .orch/config.yaml location
func getProjectRoot() (string, error) {
	if globalOpts.ProjectRoot != "" {
		return config.ExpandPath(globalOpts.ProjectRoot, ""), nil
	}

	return config.GetProjectRoot()
}

// getStore returns a store instance based on configuration
func getStore() (store.Store, error) {
	issuesRoot, err := getIssuesRoot()
	if err != nil {
		return nil, err
	}

	switch globalOpts.Backend {
	case "file":
		s, err := file.New(issuesRoot)
		if err != nil {
			return nil, err
		}
		// Enable duplicate frontmatter warnings to stderr
		s.SetWarnFunc(func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, format, args...)
		})
		return s, nil
	default:
		return nil, fmt.Errorf("unsupported backend: %s", globalOpts.Backend)
	}
}

// shortIDRegex matches a 2-6 char hex string (git-style short ID prefix)
var shortIDRegex = regexp.MustCompile(`^[0-9a-f]{2,6}$`)

// resolveRun resolves a run by short ID or run reference (issue#run or issue)
// Accepts:
//   - 2-6 char hex short ID prefix (e.g., "a3", "a3b4", "a3b4c5")
//   - Full run ref (e.g., "my-task#20231220-100000")
//   - Issue ID for latest run (e.g., "my-task")
func resolveRun(st store.Store, refStr string) (*model.Run, error) {
	// First, try as a short ID prefix (2-6 hex chars)
	if shortIDRegex.MatchString(refStr) {
		run, err := st.GetRunByShortID(refStr)
		if err == nil {
			return run, nil
		}
		// If it's exactly 6 chars and failed, report the short ID error
		// For shorter prefixes, fall through to try as regular ref
		if len(refStr) == 6 {
			return nil, err
		}
	}

	// Try as a regular run reference
	ref, err := model.ParseRunRef(refStr)
	if err != nil {
		return nil, err
	}

	return st.GetRun(ref)
}
