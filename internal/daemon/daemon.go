package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/github"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/notify"
	"github.com/s22625/orch/internal/store"
	"github.com/s22625/orch/internal/store/file"
	"github.com/s22625/orch/internal/xdg"
)

const (
	DefaultInterval = 5 * time.Second
	StallThreshold  = 60 * time.Second
	FetchInterval   = 90 * time.Second
)

type Daemon struct {
	storeFactory StoreFactory
	interval     time.Duration
	logger       *log.Logger
	stopCh       chan struct{}
	wg           sync.WaitGroup

	runStates     map[string]*RunState
	lastFetchAt   map[string]time.Time
	fetchInFlight map[string]bool
	mu            sync.Mutex

	executablePath string
	startupMtime   time.Time
	staleLogged    bool

	socketServer  *SocketServer
	config        *config.Config
	slackNotifier *notify.SlackNotifier
	debugMode     bool
	lockFile      *os.File

	githubBackend  *github.Backend
	lastGitHubSync time.Time
	gitHubSyncMu   sync.Mutex
}

// RunState tracks the monitoring state of a single run
type RunState struct {
	LastOutput     string
	LastOutputAt   time.Time
	LastCheckAt    time.Time
	OutputHash     string
	PromptStreak   int
	PRRecorded     bool
	WasAlive       bool
	DeadCheckCount int

	CaptureEndpoint       string
	CaptureFailureCount   int
	CaptureRetryAt        time.Time
	CaptureErrorKey       string
	CaptureErrorLogAt     time.Time
	SuppressedCaptureLogs int
}

func New(factory StoreFactory) *Daemon {
	return &Daemon{
		storeFactory:  factory,
		interval:      DefaultInterval,
		stopCh:        make(chan struct{}),
		runStates:     make(map[string]*RunState),
		lastFetchAt:   make(map[string]time.Time),
		fetchInFlight: make(map[string]bool),
	}
}

// SetInterval sets the monitoring interval
func (d *Daemon) SetInterval(interval time.Duration) {
	d.interval = interval
}

func (d *Daemon) SetDebugMode(enabled bool) {
	d.debugMode = enabled
}

func (d *Daemon) debug(format string, v ...interface{}) {
	if d.debugMode && d.logger != nil {
		d.logger.Printf("[DEBUG] "+format, v...)
	}
}

// Run starts the daemon main loop (blocking)
func (d *Daemon) Run() error {
	// Acquire global lock (XDG-compliant)
	lockFile, err := AcquireLock("")
	if err != nil {
		return err
	}
	d.lockFile = lockFile

	// Ensure state directory exists and open log file
	if err := xdg.EnsureStateDir(); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	logPath := xdg.LogPath()
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer logFile.Close()
	d.logger = log.New(logFile, "", log.LstdFlags)

	if err := d.initBinaryTracking(); err != nil {
		d.logger.Printf("warning: failed to init binary tracking: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		d.logger.Printf("warning: failed to load config: %v", err)
	} else {
		d.config = cfg
		if cfg.Slack.IsConfigured() {
			d.slackNotifier = notify.NewSlackNotifier(&cfg.Slack)
			d.logger.Printf("slack notifications enabled")
		}
		if cfg.IsGitHubBackend() {
			cachePath := filepath.Join(xdg.DataDir(), "github-cache.db")
			ghBackend, err := github.NewBackend(&cfg.GitHub, cachePath)
			if err != nil {
				d.logger.Printf("warning: failed to initialize GitHub backend: %v", err)
			} else {
				d.githubBackend = ghBackend
				d.logger.Printf("GitHub Issues backend enabled (owner=%s, repo=%s)", cfg.GitHub.Owner, cfg.GitHub.Repo)
			}
		}
	}

	d.logPreviousLifecycleState()
	if err := d.recoverStartupRuntimeArtifacts(); err != nil {
		return err
	}

	if err := WritePID(""); err != nil {
		return err
	}
	if err := d.markLifecycleRunning(); err != nil {
		d.logger.Printf("warning: failed to mark daemon lifecycle running: %v", err)
	}
	defer func() {
		UnregisterDaemon("")
		RemovePID("")
		if d.lockFile != nil {
			d.lockFile.Close()
		}
	}()

	if err := RegisterDaemon(""); err != nil {
		d.logger.Printf("warning: failed to register daemon: %v", err)
	}

	d.logger.Printf("global daemon started (pid=%d, binary=%s)", os.Getpid(), d.executablePath)

	d.socketServer = NewSocketServer(d.storeFactory, d.logger)
	d.socketServer.SetGitHubBackend(d.githubBackend)
	if err := d.socketServer.Start(); err != nil {
		d.logger.Printf("warning: failed to start socket server: %v", err)
	}

	if d.githubBackend != nil {
		d.wg.Add(1)
		go d.gitHubPollingLoop()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	d.safeMonitorAll()

	for {
		select {
		case <-ticker.C:
			d.safeMonitorAll()
			d.checkBinaryStaleness()
		case sig := <-sigCh:
			if sig == syscall.SIGHUP {
				d.logger.Printf("received SIGHUP, restarting with new binary")
				if err := d.markLifecycleStopped("signal:SIGHUP-restart"); err != nil {
					d.logger.Printf("warning: failed to mark daemon lifecycle stopped: %v", err)
				}
				if err := d.restartWithNewBinary(); err != nil {
					d.logger.Printf("restart failed: %v", err)
					if err := d.markLifecycleRunning(); err != nil {
						d.logger.Printf("warning: failed to restore daemon lifecycle running state after restart failure: %v", err)
					}
					continue
				}
				return nil
			}
			d.logger.Printf("received signal %v, shutting down", sig)
			if err := d.markLifecycleStopped(fmt.Sprintf("signal:%s", sig)); err != nil {
				d.logger.Printf("warning: failed to mark daemon lifecycle stopped: %v", err)
			}
			d.Stop()
			return nil
		case <-d.stopCh:
			d.logger.Printf("daemon stopped")
			if err := d.markLifecycleStopped("internal:stop"); err != nil {
				d.logger.Printf("warning: failed to mark daemon lifecycle stopped: %v", err)
			}
			return nil
		}
	}
}

func (d *Daemon) Stop() {
	close(d.stopCh)
	if d.socketServer != nil {
		d.socketServer.Stop()
	}
	d.wg.Wait()
}

func (d *Daemon) initBinaryTracking() error {
	execPath, err := os.Executable()
	if err != nil {
		return err
	}

	resolved, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		resolved = execPath
	}
	d.executablePath = resolved

	info, err := os.Stat(resolved)
	if err != nil {
		return err
	}
	d.startupMtime = info.ModTime()

	return d.writeMetadata()
}

func (d *Daemon) writeMetadata() error {
	meta := DaemonMetadata{
		PID:       os.Getpid(),
		StartedAt: time.Now(),
		ExecPath:  d.executablePath,
		ExecMtime: d.startupMtime,
		Version:   2, // XDG global daemon version
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(xdg.MetadataPath(), data, 0644)
}

func (d *Daemon) isBinaryStale() bool {
	if d.executablePath == "" {
		return false
	}
	info, err := os.Stat(d.executablePath)
	if err != nil {
		return false
	}
	return info.ModTime().After(d.startupMtime)
}

func (d *Daemon) checkBinaryStaleness() {
	if d.staleLogged {
		return
	}
	if d.isBinaryStale() {
		d.logger.Printf("WARNING: binary has been updated since daemon started - send SIGHUP to restart")
		d.staleLogged = true
	}
}

func (d *Daemon) restartWithNewBinary() error {
	d.logger.Printf("restarting daemon with new binary via exec...")
	args := []string{d.executablePath, "daemon", "run"}
	return syscall.Exec(d.executablePath, args, os.Environ())
}

func (d *Daemon) safeMonitorAll() {
	defer func() {
		if r := recover(); r != nil {
			d.logger.Printf("PANIC in monitorAll: %v", r)
		}
	}()
	d.monitorAll()
}

func (d *Daemon) monitorAll() {
	if d.socketServer == nil {
		return
	}

	type runWithStore struct {
		run *model.Run
		st  store.Store
	}

	var allRuns []*model.Run
	var runsWithStores []runWithStore

	for _, ctx := range d.socketServer.GetAllRepoContexts() {
		if ctx.Store == nil {
			continue
		}
		runs, err := ctx.Store.ListRuns(&store.ListRunsFilter{
			Status: []model.Status{model.StatusRunning, model.StatusBooting, model.StatusWaiting, model.StatusRateLimited, model.StatusPROpen, model.StatusUnknown},
		})
		if err != nil {
			d.logger.Printf("error listing runs for %s: %v", ctx.RepoID, err)
			continue
		}
		for _, run := range runs {
			allRuns = append(allRuns, run)
			runsWithStores = append(runsWithStores, runWithStore{run: run, st: ctx.Store})
		}
	}

	if len(allRuns) > 0 {
		d.periodicFetch(allRuns)
	}

	for _, rws := range runsWithStores {
		if err := d.monitorRun(rws.run, rws.st); err != nil {
			d.logger.Printf("error monitoring %s#%s: %v", rws.run.IssueID, rws.run.RunID, err)
		}
	}

	d.cleanupStates(allRuns)
}

func (d *Daemon) periodicFetch(runs []*model.Run) {
	repos := make(map[string]bool)
	for _, run := range runs {
		if run.WorktreePath == "" {
			continue
		}
		repoRoot, err := git.FindRepoRoot(run.WorktreePath)
		if err != nil {
			continue
		}
		repos[repoRoot] = true
	}

	var toFetch []string
	now := time.Now()

	d.mu.Lock()
	for repoRoot := range repos {
		if d.fetchInFlight[repoRoot] {
			continue
		}
		if lastFetch, ok := d.lastFetchAt[repoRoot]; ok && now.Sub(lastFetch) < FetchInterval {
			continue
		}
		d.fetchInFlight[repoRoot] = true
		toFetch = append(toFetch, repoRoot)
	}
	d.mu.Unlock()

	for _, repoRoot := range toFetch {
		err := git.Fetch(repoRoot, "")

		d.mu.Lock()
		delete(d.fetchInFlight, repoRoot)
		if err != nil {
			d.logger.Printf("git fetch failed for %s: %v", repoRoot, err)
		} else {
			d.logger.Printf("git fetch completed for %s", repoRoot)
			d.lastFetchAt[repoRoot] = time.Now()
		}
		d.mu.Unlock()
	}
}

// cleanupStates removes state tracking for runs that are no longer active
func (d *Daemon) cleanupStates(activeRuns []*model.Run) {
	d.mu.Lock()
	defer d.mu.Unlock()

	activeKeys := make(map[string]bool)
	for _, run := range activeRuns {
		activeKeys[run.IssueID+"#"+run.RunID] = true
	}

	for key := range d.runStates {
		if !activeKeys[key] {
			delete(d.runStates, key)
		}
	}
}

// getOrCreateState gets or creates run state tracking
func (d *Daemon) getOrCreateState(run *model.Run) *RunState {
	d.mu.Lock()
	defer d.mu.Unlock()

	key := run.IssueID + "#" + run.RunID
	state, ok := d.runStates[key]
	if !ok {
		state = &RunState{
			LastCheckAt:  time.Now(),
			LastOutputAt: time.Now(), // Assume output is fresh when we start tracking
		}
		d.runStates[key] = state
	}
	return state
}

func StartInBackground() (int, error) {
	if IsRunning("") {
		return GetRunningPID(""), nil
	}

	executable, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("failed to find executable: %w", err)
	}

	if err := xdg.EnsureStateDir(); err != nil {
		return 0, fmt.Errorf("failed to create state directory: %w", err)
	}

	stderrFile, err := os.OpenFile(xdg.StderrLogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return 0, fmt.Errorf("failed to open stderr log: %w", err)
	}

	args := []string{executable, "daemon", "run"}

	cmd := &exec.Cmd{
		Path: executable,
		Args: args,
		SysProcAttr: &syscall.SysProcAttr{
			Setsid: true,
		},
		Stdout: nil,
		Stderr: stderrFile,
		Stdin:  nil,
	}

	if err := cmd.Start(); err != nil {
		stderrFile.Close()
		return 0, fmt.Errorf("failed to start daemon: %w", err)
	}

	go func() {
		cmd.Wait()
		stderrFile.Close()
	}()

	time.Sleep(100 * time.Millisecond)

	return cmd.Process.Pid, nil
}

// Kill stops the global daemon.
func Kill(_ string) error {
	pid := GetRunningPID("")
	if pid == 0 {
		return nil // Not running
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return err
	}

	time.Sleep(500 * time.Millisecond)

	RemovePID("")

	return nil
}

func (d *Daemon) gitHubPollingLoop() {
	defer d.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			d.logger.Printf("PANIC in gitHubPollingLoop: %v", r)
		}
	}()

	pollInterval := 300 * time.Second
	if d.config != nil && d.config.GitHub.PollInterval > 0 {
		pollInterval = time.Duration(d.config.GitHub.PollInterval) * time.Second
	}

	d.logger.Printf("GitHub polling loop started (interval=%s)", pollInterval)

	if err := d.syncGitHubIssues(); err != nil {
		d.logger.Printf("initial GitHub sync failed: %v", err)
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := d.syncGitHubIssues(); err != nil {
				d.logger.Printf("GitHub sync failed: %v", err)
			}
		case <-d.stopCh:
			d.logger.Printf("GitHub polling loop stopped")
			return
		}
	}
}

func (d *Daemon) syncGitHubIssues() error {
	d.gitHubSyncMu.Lock()
	defer d.gitHubSyncMu.Unlock()

	if d.githubBackend == nil {
		return nil
	}

	d.debug("syncing GitHub issues...")

	issues, err := d.githubBackend.SyncUpdatedSince(d.lastGitHubSync)
	if err != nil {
		return err
	}

	if len(issues) > 0 {
		d.logger.Printf("GitHub sync: %d issues updated", len(issues))
	}

	d.lastGitHubSync = time.Now()
	return nil
}

func (d *Daemon) GetGitHubBackend() *github.Backend {
	return d.githubBackend
}

func RunWithFileStore(debug bool) error {
	if IsRunning("") {
		pid := GetRunningPID("")
		return fmt.Errorf("daemon already running (pid=%d)", pid)
	}

	factory := func(issuesRoot string) (store.Store, error) {
		return file.New(issuesRoot)
	}

	d := New(factory)
	if debug {
		d.SetDebugMode(true)
	}
	return d.Run()
}
