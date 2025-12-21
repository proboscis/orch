package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
	"github.com/s22625/orch/internal/store/file"
	"github.com/spf13/cobra"
)

// Exit codes as per spec
const (
	ExitOK            = 0
	ExitIssueNotFound = 2
	ExitWorktreeError = 3
	ExitTmuxError     = 4
	ExitAgentError    = 5
	ExitRunNotFound   = 6
	ExitQuestionNotFound = 7
	ExitInternalError = 10
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
	"daemon":     true,
	"repair":     true,
	"delete":     true,
	"help":       true,
	"completion": true,
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
	rootCmd.AddCommand(newShowCmd())
	rootCmd.AddCommand(newAttachCmd())
	rootCmd.AddCommand(newAnswerCmd())
	rootCmd.AddCommand(newTickCmd())
	rootCmd.AddCommand(newOpenCmd())
	rootCmd.AddCommand(newStopCmd())
	rootCmd.AddCommand(newDaemonCmd())
	rootCmd.AddCommand(newRepairCmd())
	rootCmd.AddCommand(newDeleteCmd())
}

// Execute runs the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(ExitInternalError)
	}
}

// getVaultPath returns the vault path from flags, environment, or config files
// Precedence: --vault flag > ORCH_VAULT env > .orch/config.yaml > ~/.config/orch/config.yaml
func getVaultPath() (string, error) {
	// 1. Command-line flag (highest precedence)
	if globalOpts.VaultPath != "" {
		return globalOpts.VaultPath, nil
	}

	// 2. Load from config (handles env vars and config files)
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}

	if cfg.Vault != "" {
		// Expand path relative to repo config dir if it's a relative path
		vaultPath := cfg.Vault
		if len(vaultPath) > 0 && !filepath.IsAbs(vaultPath) && vaultPath[0] != '~' {
			// Relative path - expand relative to .orch directory location
			repoDir := config.RepoConfigDir()
			if repoDir != "" {
				vaultPath = filepath.Join(filepath.Dir(repoDir), vaultPath)
			}
		}
		return config.ExpandPath(vaultPath, ""), nil
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

// shortIDRegex matches a 6-char hex string (git-style short ID)
var shortIDRegex = regexp.MustCompile(`^[0-9a-f]{6}$`)

// resolveRun resolves a run by short ID or run reference (issue#run or issue)
// Accepts:
//   - 6-char hex short ID (e.g., "a3b4c5")
//   - Full run ref (e.g., "my-task#20231220-100000")
//   - Issue ID for latest run (e.g., "my-task")
func resolveRun(st store.Store, refStr string) (*model.Run, error) {
	// First, try as a short ID (6-char hex)
	if shortIDRegex.MatchString(refStr) {
		run, err := st.GetRunByShortID(refStr)
		if err == nil {
			return run, nil
		}
		// Fall through to try as regular ref
	}

	// Try as a regular run reference
	ref, err := model.ParseRunRef(refStr)
	if err != nil {
		return nil, err
	}

	return st.GetRun(ref)
}
