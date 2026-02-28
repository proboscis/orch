package cli

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/orchapi"
	"github.com/spf13/cobra"
)

var shortIDRegex = regexp.MustCompile(`^[0-9a-f]{2,6}$`)

// Exit codes as per spec
const (
	ExitOK               = 0
	ExitIssueNotFound    = 2
	ExitWorktreeError    = 3
	ExitTmuxError        = 4
	ExitAgentError       = 5
	ExitRunNotFound      = 6
	ExitQuestionNotFound = 7
	ExitRunEnded         = 8
	ExitInternalError    = 10
)

// GlobalOptions holds options shared across all commands
type GlobalOptions struct {
	IssuesRoot  string
	ProjectRoot string
	Remote      string
	Backend     string
	JSON        bool
	TSV         bool
	Quiet       bool
	LogLevel    string
}

var globalOpts = &GlobalOptions{}
var remoteFlagWasSet bool

var noDaemonCommands = map[string]bool{
	"show":                 true,
	"daemon":               true,
	"list":                 true,
	"kill":                 true,
	"status":               true,
	"repair":               true,
	"delete":               true,
	"help":                 true,
	"completion":           true,
	"models":               true,
	"notify":               true,
	"log":                  true,
	"debug":                true,
	"query":                true,
	"q":                    true,
	"schema":               true,
	"tutorial":             true,
	"agent":                true,
	"validate-issue-files": true,
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
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		remoteFlagWasSet = cmd.Flags().Changed("remote") || cmd.PersistentFlags().Changed("remote")

		if err := validateConfigForCommand(); err != nil {
			return err
		}

		// Auto-start daemon for most commands
		if !noDaemonCommands[cmd.Name()] && getRemoteAddr() == "" {
			ensureDaemon()
		}
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&globalOpts.ProjectRoot, "project-root", "", "Path to project root where .orch/ lives (or set ORCH_PROJECT_ROOT)")
	rootCmd.PersistentFlags().StringVar(&globalOpts.IssuesRoot, "issues-root", "", "Path to issues root for file-based issues (or set ORCH_ISSUES_ROOT)")
	rootCmd.PersistentFlags().StringVar(&globalOpts.Remote, "remote", "", "Connect to remote daemon address (or set ORCH_REMOTE)")

	rootCmd.PersistentFlags().StringVar(&globalOpts.Backend, "backend", "file", "Backend type (file|github|linear)")
	rootCmd.PersistentFlags().BoolVar(&globalOpts.JSON, "json", false, "Output in JSON format")
	rootCmd.PersistentFlags().BoolVar(&globalOpts.TSV, "tsv", false, "Output in TSV format (for fzf)")
	rootCmd.PersistentFlags().BoolVar(&globalOpts.Quiet, "quiet", false, "Suppress human-readable output")
	rootCmd.PersistentFlags().StringVar(&globalOpts.LogLevel, "log-level", "warn", "Log level (error|warn|info|debug)")

	// Add subcommands
	rootCmd.AddCommand(newIssueCmd())
	rootCmd.AddCommand(newPsCmd())
	rootCmd.AddCommand(newRunCmd())
	rootCmd.AddCommand(newRestartFromCmd())
	rootCmd.AddCommand(newShowCmd())
	rootCmd.AddCommand(newDiffCmd())
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
	rootCmd.AddCommand(newAgentCmd())
	rootCmd.AddCommand(newValidateIssueFilesCmd())
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

func getRemoteAddr() string {
	clientCfg, err := config.LoadClient()
	if err != nil {
		clientCfg = nil
	}

	return resolveRemoteAddr(globalOpts.Remote, remoteFlagWasSet, os.Getenv("ORCH_REMOTE"), clientCfg)
}

func resolveRemoteAddr(flagValue string, flagChanged bool, envValue string, clientCfg *config.ClientConfig) string {
	resolve := func(v string) string {
		if clientCfg != nil {
			return clientCfg.ResolveRemote(v)
		}
		return strings.TrimSpace(v)
	}

	if flagChanged {
		// Explicit --remote "" forces local mode by design.
		return resolve(flagValue)
	}

	if env := strings.TrimSpace(envValue); env != "" {
		return resolve(env)
	}

	if clientCfg != nil {
		return clientCfg.ResolveRemote(clientCfg.Remote.Default)
	}

	return ""
}

func getAPI() (orchapi.OrchAPI, error) {
	return defaultGetAPI()
}

func defaultGetAPI() (orchapi.OrchAPI, error) {
	projectRoot, err := getProjectRoot()
	if err != nil {
		return nil, err
	}

	issuesRoot, err := getIssuesRoot()
	if err != nil {
		return nil, err
	}

	remoteAddr := getRemoteAddr()
	client := orchapi.NewDaemonClientWithAddress(projectRoot, issuesRoot, remoteAddr)
	if remoteAddr == "" && !client.IsAvailable() {
		ensureDaemon()
	}

	return client, nil
}

func resolveRunAPI(ctx context.Context, api orchapi.OrchAPI, refStr string) (*orchapi.Run, error) {
	ref, err := orchapi.ParseRunRef(refStr)
	if err != nil {
		return nil, err
	}
	return api.ResolveRun(ctx, ref)
}

func apiRunToModelRun(r *orchapi.Run) *model.Run {
	if r == nil {
		return nil
	}
	return &model.Run{
		IssueID:           r.IssueID,
		RunID:             r.RunID,
		Status:            model.NormalizeStatus(string(r.Status)),
		Agent:             r.Agent,
		Model:             r.Model,
		ModelVariant:      r.ModelVariant,
		Branch:            r.Branch,
		WorktreePath:      r.WorktreePath,
		SessionName:       r.SessionName,
		Multiplexer:       string(r.Multiplexer),
		PRUrl:             r.PRUrl,
		PRNumber:          r.PRNumber,
		PRState:           r.PRState,
		ServerPort:        r.ServerPort,
		OpenCodeSessionID: r.OpenCodeSessionID,
		ContinuedFrom:     r.ContinuedFrom,
		StartedAt:         r.StartedAt,
		UpdatedAt:         r.UpdatedAt,
		Alive:             r.Alive,
		AliveKnown:        r.AliveKnown,
		WorktreeExists:    r.WorktreeExists,
	}
}

func validateConfigForCommand() error {
	if _, err := config.LoadClient(); err != nil {
		return fmt.Errorf("invalid client config: %w", err)
	}

	if globalOpts.ProjectRoot != "" {
		projectRoot := config.ExpandPath(globalOpts.ProjectRoot, "")
		if _, err := config.LoadFromProjectRoot(projectRoot); err != nil {
			return fmt.Errorf("invalid config: %w", err)
		}
		return nil
	}

	if _, err := config.Load(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	return nil
}
