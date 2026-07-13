package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/orchapi"
	"github.com/proboscis/orch/internal/xdg"
	"github.com/spf13/cobra"
)

type DebugLogger struct {
	enabled bool
}

func NewDebugLogger() *DebugLogger {
	enabled := globalOpts.LogLevel == "debug" ||
		os.Getenv("ORCH_DEBUG") == "1" ||
		os.Getenv("ORCH_DEBUG") == "true" ||
		os.Getenv("ORCH_DEBUG") == "yes"
	return &DebugLogger{enabled: enabled}
}

func (d *DebugLogger) IsEnabled() bool {
	return d.enabled
}

func (d *DebugLogger) Printf(format string, args ...interface{}) {
	if d == nil || !d.enabled {
		return
	}
	fmt.Fprintf(os.Stderr, "[DEBUG] %s\n", fmt.Sprintf(format, args...))
}

func newDebugCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debug <run>",
		Short: "Debug a run by showing daemon perspective",
		Long: `Show what the daemon sees for a run.

This command is useful for debugging why a run has a particular status.
It shows:
- Agent liveness check results
- Session status (for opencode)
- Git state (commits ahead, uncommitted changes)
- PR status
- Daemon log location`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDebug(args[0])
		},
	}
	cmd.AddCommand(newDebugClientBootstrapCmd())
	return cmd
}

type debugClientBootstrap struct {
	ProjectRoot        string `json:"project_root"`
	ProjectID          string `json:"project_id"`
	RemoteAddr         string `json:"remote_addr"`
	SocketPath         string `json:"socket_path"`
	MonitorSessionName string `json:"monitor_session_name"`
}

func newDebugClientBootstrapCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:    "client-bootstrap",
		Short:  "Print client-side bootstrap values resolved by orch",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDebugClientBootstrap(jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output JSON")
	return cmd
}

func runDebugClientBootstrap(jsonOut bool) error {
	projectRoot, _, err := getProjectRootWithSource()
	if err != nil {
		return err
	}

	projectID, _, err := getProjectIDWithSource(projectRoot)
	if err != nil {
		return err
	}

	sessionRoot := strings.TrimSpace(projectRoot)
	if sessionRoot == "" {
		return fmt.Errorf("project root is empty")
	}

	result := debugClientBootstrap{
		ProjectRoot:        strings.TrimSpace(projectRoot),
		ProjectID:          strings.TrimSpace(projectID),
		RemoteAddr:         strings.TrimSpace(getRemoteAddr()),
		SocketPath:         xdg.SocketPath(),
		MonitorSessionName: monitorSessionNameForProject(sessionRoot),
	}

	if jsonOut || globalOpts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Printf("project_root: %s\n", result.ProjectRoot)
	fmt.Printf("project_id: %s\n", result.ProjectID)
	fmt.Printf("remote_addr: %s\n", result.RemoteAddr)
	fmt.Printf("socket_path: %s\n", result.SocketPath)
	fmt.Printf("monitor_session_name: %s\n", result.MonitorSessionName)
	return nil
}

const (
	defaultMonitorSessionName = "orch-monitor"
	zellijSockMaxLength       = 104
	zellijContractDir         = "contract_version_1"
	sessionNameSafety         = 3
)

// monitorSessionNameForProject preserves the session-name contract consumed by
// the standalone Python orch-monitor client bootstrap.
func monitorSessionNameForProject(projectRoot string) string {
	if projectRoot == "" {
		return defaultMonitorSessionName
	}

	absPath, err := filepath.Abs(projectRoot)
	if err != nil {
		absPath = projectRoot
	}
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = resolved
	}

	hash := sha256.Sum256([]byte(absPath))
	shortHash := hex.EncodeToString(hash[:])[:6]
	baseName := strings.NewReplacer(".", "-", " ", "-").Replace(filepath.Base(absPath))
	name := fmt.Sprintf("orch-%s-%s", baseName, shortHash)

	budget := maxMonitorSessionNameLen()
	if len(name) <= budget {
		return name
	}

	fixed := len("orch-") + 1 + len(shortHash)
	room := budget - fixed
	if room < 1 {
		return "orch-" + shortHash
	}
	if room < len(baseName) {
		baseName = baseName[:room]
	}
	return fmt.Sprintf("orch-%s-%s", baseName, shortHash)
}

func zellijSocketDir() string {
	base := os.Getenv("ZELLIJ_SOCKET_DIR")
	if base == "" {
		base = filepath.Join(os.TempDir(), fmt.Sprintf("zellij-%d", os.Getuid()))
	}
	return filepath.Join(base, zellijContractDir)
}

func maxMonitorSessionNameLen() int {
	budget := zellijSockMaxLength - len(zellijSocketDir()) - 1 - sessionNameSafety
	if budget < 1 {
		return 1
	}
	return budget
}

func runDebug(ref string) error {
	ctx := context.Background()
	api, err := getAPIForListing()
	if err != nil {
		return err
	}

	run, err := resolveRunAPI(ctx, api, ref)
	if err != nil {
		return fmt.Errorf("run not found: %w", err)
	}
	outputDebugRun(run)

	fmt.Printf("--- Daemon Info ---\n")
	daemonStatus, err := api.GetDaemonStatus(ctx)
	if err != nil {
		fmt.Printf("Daemon Running: unknown (error: %v)\n", err)
	} else {
		fmt.Printf("Daemon Running: %v\n", daemonStatus.Running)
		if daemonStatus.Running {
			fmt.Printf("Daemon PID: %d\n", daemonStatus.PID)
		}
		fmt.Printf("Daemon Log: %s\n", daemonStatus.LogPath)
	}

	return nil
}

func outputDebugRun(run *orchapi.Run) {

	fmt.Printf("=== Debug Info for %s#%s ===\n\n", run.IssueID, run.RunID)

	fmt.Printf("Current Status: %s\n", run.Status)
	fmt.Printf("Agent: %s\n", run.Agent)
	fmt.Printf("Branch: %s\n", run.Branch)
	fmt.Printf("Worktree: %s\n", run.WorktreePath)
	if run.PRUrl != "" {
		fmt.Printf("PR URL: %s\n", run.PRUrl)
	}
	fmt.Println()

	fmt.Printf("--- Agent Status ---\n")
	fmt.Printf("Session State: %s\n", run.SessionState)
	if run.SessionState == model.SessionStateReapedUnrevivable && run.SessionStateDetail != "" {
		fmt.Printf("Session State Detail: %s\n", run.SessionStateDetail)
	}
	if run.AliveKnown {
		fmt.Printf("Agent Alive: %v\n", run.Alive)
	} else {
		fmt.Printf("Agent Alive: unknown\n")
	}

	if run.Agent == "opencode" {
		fmt.Printf("Server Port: %d\n", run.ServerPort)
		fmt.Printf("Session ID: %s\n", run.OpenCodeSessionID)
	}
	fmt.Println()

	fmt.Printf("--- Git State ---\n")
	fmt.Printf("Worktree Exists: %v\n", run.WorktreeExists)
	if run.WorktreeExists {
		fmt.Printf("Branch State: %s\n", run.BranchState)
		if run.DiffStats != nil {
			fmt.Printf("Files Changed: %d\n", run.DiffStats.FilesChanged)
			fmt.Printf("Additions: +%d\n", run.DiffStats.Additions)
			fmt.Printf("Deletions: -%d\n", run.DiffStats.Deletions)
		}
	}
	fmt.Println()

	fmt.Printf("--- PR Status ---\n")
	if run.PRUrl != "" {
		fmt.Printf("PR URL: %s\n", run.PRUrl)
		fmt.Printf("PR Number: %d\n", run.PRNumber)
		fmt.Printf("PR State: %s\n", run.PRState)
	} else {
		fmt.Printf("PR: none\n")
	}
	fmt.Println()

}
