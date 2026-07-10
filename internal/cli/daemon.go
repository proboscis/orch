package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/proboscis/orch/internal/daemon"
	buildversion "github.com/proboscis/orch/internal/version"
	"github.com/spf13/cobra"
)

var daemonDebugMode bool
var daemonListenAddr string

type daemonVersionPinger interface {
	PingStatus() (*daemon.PingResponse, error)
}

func pingDaemonWithVersionCheck(client daemonVersionPinger) error {
	resp, err := client.PingStatus()
	if err != nil {
		return err
	}
	warnIfDaemonVersionMismatch(resp.Version)
	return nil
}

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the background monitoring daemon",
		Long: `Manage the background monitoring daemon.

The daemon monitors all running agent sessions and updates their status.
It runs automatically in the background when needed.`,
	}

	runCmd := &cobra.Command{
		Use:    "run",
		Short:  "Run the daemon (internal use)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemon()
		},
	}
	runCmd.Flags().BoolVar(&daemonDebugMode, "debug", false, "Enable verbose debug logging")
	runCmd.Flags().StringVar(&daemonListenAddr, "listen", daemon.DefaultTCPListenAddr, "TCP listen address for remote clients")
	cmd.AddCommand(runCmd)
	cmd.AddCommand(newDaemonStartCmd())

	cmd.AddCommand(newDaemonListCmd())
	cmd.AddCommand(newDaemonKillCmd())
	cmd.AddCommand(newDaemonStatusCmd())
	cmd.AddCommand(newDaemonRepoCmd())

	return cmd
}

func newDaemonRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Manage daemon repo identity mappings",
		Long: `Manage daemon-side repo identity mappings used by remote clients.

Mappings connect repo URL identities to daemon-managed project workspaces.`,
	}

	cmd.AddCommand(newDaemonRepoRegisterCmd())
	cmd.AddCommand(newDaemonRepoListCmd())

	return cmd
}

func newDaemonRepoRegisterCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "register PROJECT_PATH",
		Short: "Register a local checkout path for project identity",
		Long: `Register a local Git checkout path with the daemon.

The daemon reads the checkout's origin remote to derive the project identity.
PROJECT_PATH must exist on the daemon host.`,
		Example: `  orch daemon repo register "$(pwd)"`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonRepoRegister(args[0])
		},
	}
}

func newDaemonRepoListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List repo identity mappings known by daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonRepoList()
		},
	}
}

func requireDaemonAdminClient() (*daemon.ProtoClient, error) {
	remoteAddr := getRemoteAddr()
	if remoteAddr != "" {
		client := daemon.NewProtoClientWithAddress("", remoteAddr)
		if err := pingDaemonWithVersionCheck(client); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("remote daemon %s is not reachable: %w", remoteAddr, err)
		}
		return client, nil
	}

	client := daemon.NewProtoClientWithAddress("", "")
	if client.IsAvailable() {
		if err := pingDaemonWithVersionCheck(client); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("daemon is not reachable: %w", err)
		}
		return client, nil
	}

	if _, err := daemon.StartInBackground(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("daemon not running and failed to start: %w", err)
	}

	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		if client.IsAvailable() {
			if err := pingDaemonWithVersionCheck(client); err != nil {
				_ = client.Close()
				return nil, fmt.Errorf("daemon started but is not reachable: %w", err)
			}
			return client, nil
		}
	}

	_ = client.Close()
	return nil, fmt.Errorf("daemon did not become available after starting")
}

func runDaemonRepoRegister(repoURL string) error {
	client, err := requireDaemonAdminClient()
	if err != nil {
		return err
	}
	defer client.Close()

	repoID, err := client.RegisterRepo(repoURL)
	if err != nil {
		return err
	}

	if globalOpts.JSON {
		fmt.Printf("{\"repo_id\":%q,\"repo_url\":%q}\n", repoID, repoURL)
		return nil
	}

	fmt.Printf("Registered repo mapping: %s -> %s\n", repoID, repoURL)
	return nil
}

func runDaemonRepoList() error {
	client, err := requireDaemonAdminClient()
	if err != nil {
		return err
	}
	defer client.Close()

	repos, err := client.ListRepos()
	if err != nil {
		return err
	}

	if globalOpts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(repos)
	}

	if len(repos) == 0 {
		fmt.Println("No repo mappings registered.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "REPO_ID\tPROJECT_ROOT")
	for _, r := range repos {
		fmt.Fprintf(w, "%s\t%s\n", r["repo_id"], r["project_root"])
	}
	return w.Flush()
}

func newDaemonStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the daemon in the background",
		Long: fmt.Sprintf(`Start the orch daemon in the background.

The proto API listens on %s by default (loopback only — the API is
unauthenticated). Multi-host use is an explicit opt-in: bind a reachable
address with --listen, e.g. tcp://0.0.0.0:7777 or a specific trusted
interface (ADR-0003).`, daemon.DefaultTCPListenAddr),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonStart()
		},
	}

	cmd.Flags().StringVar(&daemonListenAddr, "listen", daemon.DefaultTCPListenAddr, "TCP listen address for remote clients")
	return cmd
}

func newDaemonListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all running daemons",
		Long: `List all running orch daemons across all projects.

Shows PID, project directory, socket status, and uptime for each daemon.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonList()
		},
	}
}

type daemonKillOptions struct {
	All     bool
	Project string
}

func newDaemonKillCmd() *cobra.Command {
	opts := &daemonKillOptions{}

	cmd := &cobra.Command{
		Use:   "kill",
		Short: "Kill running daemon(s)",
		Long: `Kill orch daemon(s).

Orch uses a global daemon. By default, this kills that daemon.
Use --all as an alias for the same behavior.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonKill(opts)
		},
	}

	cmd.Flags().BoolVar(&opts.All, "all", false, "Kill all running daemons")
	cmd.Flags().StringVar(&opts.Project, "project", "", "Deprecated: ignored for global daemon")

	return cmd
}

func newDaemonStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show global daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonStatus()
		},
	}
}

func runDaemonList() error {
	if err := daemon.CleanupStaleRegistrations(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to cleanup stale registrations: %v\n", err)
	}

	infos, err := daemon.ListAllDaemons()
	if err != nil {
		return fmt.Errorf("failed to list daemons: %w", err)
	}

	if len(infos) == 0 {
		fmt.Println("No daemons running.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PID\tPROJECT\tSOCKET\tUPTIME")

	for _, info := range infos {
		socketStatus := "ok"
		if !info.IsHealthy {
			socketStatus = "unavailable"
		}

		projectDisplay := info.ProjectRoot
		if home, err := os.UserHomeDir(); err == nil {
			if rel, err := filepath.Rel(home, info.ProjectRoot); err == nil && len(rel) < len(info.ProjectRoot) {
				projectDisplay = "~/" + rel
			}
		}

		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n",
			info.PID,
			projectDisplay,
			socketStatus,
			formatUptime(info.Uptime),
		)
	}

	w.Flush()
	return nil
}

func runDaemonKill(opts *daemonKillOptions) error {
	if opts.Project != "" {
		fmt.Fprintln(os.Stderr, "warning: --project is deprecated and ignored for global daemon")
	}

	if opts.All {
		count, err := daemon.KillAllDaemons()
		if err != nil {
			return fmt.Errorf("failed to kill daemons: %w", err)
		}
		if count == 0 {
			fmt.Println("No daemons were running.")
		} else {
			fmt.Printf("Killed %d daemon(s).\n", count)
		}
		return nil
	}

	if !daemon.IsRunning("") {
		fmt.Println("No daemon running.")
		return nil
	}

	pid := daemon.GetRunningPID("")
	if err := daemon.KillDaemon(""); err != nil {
		return fmt.Errorf("failed to kill daemon (pid=%d): %w", pid, err)
	}

	fmt.Printf("Killed daemon (pid=%d)\n", pid)
	return nil
}

func runDaemonStatus() error {
	remoteAddr := getRemoteAddr()
	if remoteAddr != "" {
		client := daemon.NewProtoClientWithAddress("", remoteAddr)
		defer client.Close()

		if err := pingDaemonWithVersionCheck(client); err != nil {
			return fmt.Errorf("remote daemon %s is not reachable: %w", remoteAddr, err)
		}

		status, err := client.GetDaemonStatus()
		if err != nil {
			return fmt.Errorf("failed to get remote daemon status from %s: %w", remoteAddr, err)
		}

		warnIfDaemonVersionMismatch(status.Version)
		fmt.Printf("CLI Version: %s\n", buildversion.Version)
		fmt.Printf("Daemon Version: %s\n", status.Version)
		fmt.Printf("Status: running (remote=%s, pid=%d)\n", remoteAddr, status.PID)
		fmt.Printf("Log: %s\n", status.LogPath)
		return nil
	}

	fmt.Printf("CLI Version: %s\n", buildversion.Version)

	if !daemon.IsRunning("") {
		fmt.Println("Daemon Version: not running")
		fmt.Println("Status: not running")
		return nil
	}

	pid := daemon.GetRunningPID("")
	fmt.Printf("Status: running (pid=%d)\n", pid)

	if daemon.IsDaemonSocketAvailable("") {
		fmt.Println("Socket: available")
		client := daemon.NewProtoClientWithAddress("", "")
		defer client.Close()
		status, err := client.GetDaemonStatus()
		if err != nil {
			fmt.Printf("Daemon Version: unavailable (%v)\n", err)
		} else {
			warnIfDaemonVersionMismatch(status.Version)
			fmt.Printf("Daemon Version: %s\n", status.Version)
		}
	} else {
		fmt.Println("Socket: unavailable")
		fmt.Println("Daemon Version: unavailable (socket unavailable)")
	}

	if meta, err := daemon.ReadMetadata(""); err == nil {
		fmt.Printf("Started: %s\n", meta.StartedAt.Format(time.RFC3339))
		fmt.Printf("Uptime: %s\n", formatUptime(time.Since(meta.StartedAt)))
	}

	stale, err := daemon.IsStaleBinary("")
	if err == nil && stale {
		fmt.Println("Warning: daemon is running stale binary (code updated since start)")
	}

	fmt.Printf("Log: %s\n", daemon.LogFilePath(""))

	return nil
}

func formatUptime(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func newDaemonRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "daemon-restart",
		Short:  "Restart daemon with new binary",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !daemon.IsRunning("") {
				return nil
			}

			return daemon.RestartDaemon("")
		},
	}
}

func runDaemon() error {
	if err := daemon.RunWithFileStoreAndListen(daemonDebugMode, daemonListenAddr); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	return nil
}

func runDaemonStart() error {
	if daemon.IsRunning("") {
		fmt.Printf("Daemon already running (pid=%d)\n", daemon.GetRunningPID(""))
		return nil
	}

	pid, err := daemon.StartInBackgroundWithListen(daemonListenAddr)
	if err != nil {
		return err
	}

	fmt.Printf("Started daemon (pid=%d)\n", pid)
	if daemonListenAddr != "" {
		fmt.Printf("Listening on %s\n", daemonListenAddr)
	}
	return nil
}

func ensureDaemon() {
	if daemon.IsRunning("") {
		return
	}

	_, err := daemon.StartInBackground()
	if err != nil {
		if globalOpts.LogLevel == "debug" {
			fmt.Fprintf(os.Stderr, "warning: failed to start daemon: %v\n", err)
		}
	}
}

// testBypassDaemon allows unit tests to bypass daemon requirement
// Set this to true in tests along with a testStore for direct file operations
var testBypassDaemon bool

func requireDaemon() (*daemon.ProtoClient, error) {
	remoteAddr := getRemoteAddr()

	projectRoot, explicitProjectRoot, err := getProjectRootWithSource()
	if err != nil {
		projectRoot = ""
	}
	if !explicitProjectRoot {
		projectRoot = ""
	}

	if remoteAddr != "" {
		client := daemon.NewProtoClientWithAddress(projectRoot, remoteAddr)
		if err := pingDaemonWithVersionCheck(client); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("remote daemon %s is not reachable: %w", remoteAddr, err)
		}
		return client, nil
	}

	client := daemon.NewProtoClientLocal(projectRoot)
	if client.IsAvailable() {
		if err := pingDaemonWithVersionCheck(client); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("daemon is not reachable: %w", err)
		}
		return client, nil
	}

	_, err = daemon.StartInBackground()
	if err != nil {
		return nil, fmt.Errorf("daemon not running and failed to start: %w\nRun 'orch repair' to fix daemon issues", err)
	}

	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		if client.IsAvailable() {
			if err := pingDaemonWithVersionCheck(client); err != nil {
				_ = client.Close()
				return nil, fmt.Errorf("daemon started but is not reachable: %w", err)
			}
			return client, nil
		}
	}

	if !client.IsAvailable() {
		return nil, fmt.Errorf("daemon did not become available after starting\nRun 'orch repair' to fix daemon issues")
	}

	return client, nil
}
