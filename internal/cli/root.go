package cli

import (
	"fmt"
	"os"

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
}

// Execute runs the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(ExitInternalError)
	}
}

// getVaultPath returns the vault path from flags or environment
func getVaultPath() (string, error) {
	if globalOpts.VaultPath != "" {
		return globalOpts.VaultPath, nil
	}
	if envPath := os.Getenv("ORCH_VAULT"); envPath != "" {
		return envPath, nil
	}
	return "", fmt.Errorf("vault path not specified (use --vault or set ORCH_VAULT)")
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
