package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/proboscis/orch/internal/config"
	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/orchapi"
	buildversion "github.com/proboscis/orch/internal/version"
	"github.com/proboscis/orch/internal/xdg"
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
	Project     string
	ProjectRoot string // deprecated compatibility field
	Remote      string
	Backend     string
	JSON        bool
	TSV         bool
	Quiet       bool
	LogLevel    string
}

var globalOpts = &GlobalOptions{}
var remoteFlagWasSet bool

const (
	commandGroupCore     = "core"
	commandGroupSetupOps = "setup-ops"
	commandGroupAdvanced = "advanced"
)

var noDaemonCommands = map[string]bool{
	"show":                 true,
	"daemon":               true,
	"master":               true,
	"worker":               true,
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
	"version":              true,
}

var noConfigValidationCommands = map[string]bool{
	"version": true,
}

// rootCmd represents the base command
var rootCmd = &cobra.Command{
	Use:     "orch",
	Short:   "Orchestrator for multiple LLM CLIs",
	Version: buildversion.Version,
	Long: `orch is an orchestrator for managing multiple LLM CLIs (claude/codex/gemini)
using a unified vocabulary of issue/run/event.

It operates non-interactively by default, using events to track state
	and questions to handle human input requirements.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		remoteFlagWasSet = cmd.Flags().Changed("remote") || cmd.PersistentFlags().Changed("remote")

		if shouldValidateConfigForCommand(cmd) {
			if err := validateConfigForCommand(); err != nil {
				return err
			}
		}

		// Auto-start daemon for most commands.
		// Check command ancestry so "orch master start" and "orch worker ..."
		// don't auto-start an implicit local daemon before command execution.
		if shouldAutoStartDaemonForCommand(cmd, getRemoteAddr()) {
			ensureDaemon()
		}
		return nil
	},
}

func shouldAutoStartDaemonForCommand(cmd *cobra.Command, remoteAddr string) bool {
	if strings.TrimSpace(remoteAddr) != "" {
		return false
	}

	for c := cmd; c != nil; c = c.Parent() {
		if noDaemonCommands[c.Name()] {
			return false
		}
	}

	return true
}

func shouldValidateConfigForCommand(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if noConfigValidationCommands[c.Name()] {
			return false
		}
	}
	return true
}

func init() {
	rootCmd.PersistentFlags().StringVar(&globalOpts.Project, "project", "", "Project identity (git repo URL or normalized repo ID; or set ORCH_PROJECT)")
	rootCmd.PersistentFlags().StringVar(&globalOpts.Remote, "remote", "", "Connect to remote daemon address (or set ORCH_REMOTE)")

	rootCmd.PersistentFlags().StringVar(&globalOpts.Backend, "backend", "file", "Issue store backend (local|github)")
	rootCmd.PersistentFlags().BoolVar(&globalOpts.JSON, "json", false, "Output in JSON format")
	rootCmd.PersistentFlags().BoolVar(&globalOpts.TSV, "tsv", false, "Output in TSV format (for fzf)")
	rootCmd.PersistentFlags().BoolVar(&globalOpts.Quiet, "quiet", false, "Suppress human-readable output")
	rootCmd.PersistentFlags().StringVar(&globalOpts.LogLevel, "log-level", "warn", "Log level (error|warn|info|debug)")

	rootCmd.AddGroup(
		&cobra.Group{ID: commandGroupCore, Title: "Core Commands:"},
		&cobra.Group{ID: commandGroupSetupOps, Title: "Setup & Ops Commands:"},
		&cobra.Group{ID: commandGroupAdvanced, Title: "Advanced Commands:"},
	)
	rootCmd.SetHelpCommandGroupID(commandGroupCore)
	rootCmd.SetCompletionCommandGroupID(commandGroupAdvanced)

	// Add subcommands
	rootCmd.AddCommand(
		withCommandGroup(newIssueCmd(), commandGroupCore),
		withCommandGroup(newPsCmd(), commandGroupCore),
		withCommandGroup(newRunCmd(), commandGroupCore),
		withCommandGroup(newShowCmd(), commandGroupCore),
		withCommandGroup(newDiffCmd(), commandGroupCore),
		withCommandGroup(newAttachCmd(), commandGroupCore),
		withCommandGroup(newOpenCmd(), commandGroupCore),
		withCommandGroup(newStopCmd(), commandGroupCore),
		withCommandGroup(newWaitCmd(), commandGroupCore),
		withCommandGroup(newResolveCmd(), commandGroupCore),
		withCommandGroup(newSendCmd(), commandGroupCore),
		withCommandGroup(newCaptureCmd(), commandGroupCore),

		withCommandGroup(newDaemonCmd(), commandGroupSetupOps),
		withCommandGroup(newMasterCmd(), commandGroupSetupOps),
		withCommandGroup(newWorkerCmd(), commandGroupSetupOps),
		withCommandGroup(newDaemonRestartCmd(), commandGroupSetupOps),
		withCommandGroup(newRepairCmd(), commandGroupSetupOps),
		withCommandGroup(newTutorialCmd(), commandGroupSetupOps),
		withCommandGroup(newVersionCmd(), commandGroupSetupOps),
		withCommandGroup(newCleanCmd(), commandGroupSetupOps),
		withCommandGroup(newDeleteCmd(), commandGroupSetupOps),
		withCommandGroup(newNotifyCmd(), commandGroupSetupOps),

		withCommandGroup(newTickCmd(), commandGroupAdvanced),
		withCommandGroup(newRestartFromCmd(), commandGroupAdvanced),
		withCommandGroup(newExecCmd(), commandGroupAdvanced),
		withCommandGroup(newCaptureAllCmd(), commandGroupAdvanced),
		withCommandGroup(newModelsCmd(), commandGroupAdvanced),
		withCommandGroup(newLogCmd(), commandGroupAdvanced),
		withCommandGroup(newEventsCmd(), commandGroupAdvanced),
		withCommandGroup(newDebugCmd(), commandGroupAdvanced),
		withCommandGroup(newQueryCmd(), commandGroupAdvanced),
		withCommandGroup(newSchemaCmd(), commandGroupAdvanced),
		withCommandGroup(newAgentCmd(), commandGroupAdvanced),
		withCommandGroup(newValidateIssueFilesCmd(), commandGroupAdvanced),
	)
}

func withCommandGroup(cmd *cobra.Command, groupID string) *cobra.Command {
	cmd.GroupID = groupID
	return cmd
}

// Execute runs the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(ExitInternalError)
	}
}

func getIssuesRootForProject(projectRoot string) (string, error) {
	cfg, err := config.LoadFromProjectRoot(projectRoot)
	if err != nil {
		return "", err
	}

	if issuesPath := cfg.GetIssuesPath(); issuesPath != "" {
		return issuesPath, nil
	}

	return "", fmt.Errorf("issues root not specified (set issues.path in .orch/config.yaml)")
}

func getIssuesRootForProjectIfConfigured(projectRoot string) (string, error) {
	cfg, err := config.LoadFromProjectRoot(projectRoot)
	if err != nil {
		return "", err
	}

	return cfg.GetIssuesPath(), nil
}

// getIssuesRoot resolves project root and then loads issues.path for that project.
func getIssuesRoot() (string, error) {
	projectRoot, err := getProjectRoot()
	if err != nil {
		return "", err
	}
	return getIssuesRootForProject(projectRoot)
}

// getProjectRoot returns the project root directory (where .orch/ lives).
// It is auto-resolved from the current directory hierarchy.
func getProjectRoot() (string, error) {
	return config.GetProjectRoot()
}

func getProjectRootWithSource() (string, bool, error) {
	projectRoot, err := config.GetProjectRoot()
	if err != nil {
		return "", false, err
	}

	return projectRoot, false, nil
}

func getProjectIDWithSource(projectRoot string) (string, bool, error) {
	if projectID := strings.TrimSpace(globalOpts.Project); projectID != "" {
		normalized, err := normalizeProjectIdentityInput(projectID)
		if err != nil {
			return "", true, err
		}
		return normalized, true, nil
	}

	if projectID := strings.TrimSpace(os.Getenv("ORCH_PROJECT")); projectID != "" {
		normalized, err := normalizeProjectIdentityInput(projectID)
		if err != nil {
			return "", true, err
		}
		return normalized, true, nil
	}

	if strings.TrimSpace(projectRoot) == "" {
		return "", false, nil
	}

	projectID, err := xdg.RepoIDStrict(projectRoot)
	if err != nil {
		return "", false, err
	}

	return string(projectID), false, nil
}

func resolveProjectIdentity(projectRoot string) (string, error) {
	projectID, _, err := getProjectIDWithSource(projectRoot)
	if err != nil {
		return "", fmt.Errorf("project identity required: %w (set --project/ORCH_PROJECT to git repo URL or configure git remote origin)", err)
	}
	if strings.TrimSpace(projectID) == "" {
		return "", fmt.Errorf("project identity required: set --project/ORCH_PROJECT to git repo URL or run from a git repo with remote origin")
	}
	return strings.TrimSpace(projectID), nil
}

func normalizeProjectIdentityInput(project string) (string, error) {
	project = strings.TrimSpace(project)
	if project == "" {
		return "", nil
	}

	if looksLikeProjectRepoURL(project) {
		normalized, err := xdg.ParseRepoID(project)
		if err != nil {
			return "", fmt.Errorf("invalid project identity %q: %w", project, err)
		}
		return string(normalized), nil
	}
	if looksLikeProjectPath(project) {
		return "", fmt.Errorf("project identity %q looks like a filesystem path; use git repo URL or normalized repo ID", project)
	}

	return project, nil
}

func looksLikeProjectRepoURL(project string) bool {
	value := strings.TrimSpace(project)
	if value == "" {
		return false
	}
	if strings.HasPrefix(value, "git@") || strings.Contains(value, "://") {
		return true
	}
	if strings.HasPrefix(value, "github.com/") || strings.HasPrefix(value, "gitlab.com/") {
		return true
	}
	parts := strings.Split(value, "/")
	return len(parts) >= 3 && strings.Contains(parts[0], ".")
}

func looksLikeProjectPath(project string) bool {
	value := strings.TrimSpace(project)
	if value == "" {
		return false
	}
	if filepath.IsAbs(value) {
		return true
	}
	if value == "." || value == ".." {
		return true
	}
	return strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") || strings.Contains(value, "/") || strings.Contains(value, "\\")
}

func resolveExplicitProjectScope(scopeValue, scopeFlagName string) (string, error) {
	if strings.TrimSpace(scopeValue) != "" {
		return config.ExpandPath(scopeValue, ""), nil
	}

	projectRoot, _, err := getProjectRootWithSource()
	if err != nil {
		if scopeFlagName != "" {
			return "", fmt.Errorf("project scope required: %w (set %s or run from repo root)", err, scopeFlagName)
		}
		return "", fmt.Errorf("project scope required: %w (run from repo root)", err)
	}

	return projectRoot, nil
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

func getAPIForListing() (orchapi.OrchAPI, error) {
	return defaultGetAPIWithOptions(false)
}

func defaultGetAPI() (orchapi.OrchAPI, error) {
	return defaultGetAPIWithOptions(true)
}

func defaultGetAPIWithOptions(requireProjectRoot bool) (orchapi.OrchAPI, error) {
	remoteAddr := getRemoteAddr()

	projectRoot, explicitProjectRoot, err := getProjectRootWithSource()
	if err != nil {
		projectRoot = ""
		explicitProjectRoot = false
	}

	if requireProjectRoot {
		if _, err := resolveProjectIdentity(projectRoot); err != nil {
			return nil, err
		}
	}

	clientProjectScope := ""
	if projectID, err := resolveProjectIdentity(projectRoot); err == nil && strings.TrimSpace(projectID) != "" {
		clientProjectScope = projectID
	} else if requireProjectRoot {
		return nil, err
	} else if !explicitProjectRoot && strings.TrimSpace(globalOpts.Project) != "" {
		projectID, err := resolveProjectIdentity("")
		if err != nil {
			return nil, err
		}
		clientProjectScope = projectID
	} else if strings.TrimSpace(projectRoot) != "" && strings.TrimSpace(remoteAddr) == "" {
		clientProjectScope = strings.TrimSpace(projectRoot)
	}

	client := orchapi.NewDaemonClientWithAddress(clientProjectScope, remoteAddr)
	if remoteAddr == "" && !client.IsAvailable() {
		ensureDaemon()
	}
	if err := pingAPIWithVersionCheck(context.Background(), client); err != nil && remoteAddr != "" {
		return nil, fmt.Errorf("remote daemon %s is not reachable: %w", remoteAddr, err)
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

func apiRunToModelRun(r *orchapi.Run) (*model.Run, error) {
	if r == nil {
		return nil, nil
	}
	status, err := model.NormalizeStatus(string(r.Status))
	if err != nil {
		return nil, fmt.Errorf("invalid run status for %s#%s: %w", r.IssueID, r.RunID, err)
	}
	return &model.Run{
		IssueID:           r.IssueID,
		RunID:             r.RunID,
		Status:            status,
		Agent:             r.Agent,
		Profile:           r.Profile,
		Model:             r.Model,
		ModelVariant:      r.ModelVariant,
		Branch:            r.Branch,
		WorktreePath:      r.WorktreePath,
		Target:            r.Target,
		TargetHost:        r.TargetHost,
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
	}, nil
}

func validateConfigForCommand() error {
	if _, err := config.LoadClient(); err != nil {
		return fmt.Errorf("invalid client config: %w", err)
	}

	if _, err := config.Load(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	return nil
}
