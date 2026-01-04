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
	VaultPath string
	Backend   string
	JSON      bool
	TSV       bool
	Quiet     bool
	LogLevel  string
}

var globalOpts = &GlobalOptions{}

// Commands that should NOT auto-start the daemon
var noDaemonCommands = map[string]bool{
	"show":       true,
	"daemon":     true,
	"repair":     true,
	"delete":     true,
	"help":       true,
	"completion": true,
	"models":     true,
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
	// Global flags
	rootCmd.PersistentFlags().StringVar(&globalOpts.VaultPath, "vault", "", "Path to vault (or set ORCH_VAULT)")
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
}

// Execute runs the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(ExitInternalError)
	}
}

// getVaultPath returns the vault path from flags, environment, or config files
// Precedence: --vault flag > local .orch/config.yaml > parent .orch/config.yaml > ORCH_VAULT env > ~/.config/orch/config.yaml
func getVaultPath() (string, error) {
	// 1. Command-line flag (highest precedence)
	if globalOpts.VaultPath != "" {
		return config.ExpandPath(globalOpts.VaultPath, ""), nil
	}

	// 2. Load from config (handles env vars and config files)
	// Note: config.Load() resolves relative paths from config files at load time
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}

	if cfg.Vault != "" {
		// Path is already resolved if it came from a config file
		// For env vars, ExpandPath will handle ~ but relative paths stay relative to cwd
		return config.ExpandPath(cfg.Vault, ""), nil
	}

	return "", fmt.Errorf("vault path not specified (use --vault, set ORCH_VAULT, or create .orch/config.yaml)")
}

// getStore returns a store instance based on configuration
func getStore() (store.Store, error) {
	vaultPath, err := getVaultPath()
	if err != nil {
		return nil, err
	}

	switch globalOpts.Backend {
	case "file":
		return file.New(vaultPath)
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
