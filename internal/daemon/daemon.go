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

	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
)

const (
	DefaultInterval = 5 * time.Second
	StallThreshold  = 60 * time.Second
	FetchInterval   = 90 * time.Second
)

// Daemon manages background monitoring of runs
type Daemon struct {
	vaultPath string
	store     store.Store
	interval  time.Duration
	logger    *log.Logger
	stopCh    chan struct{}
	wg        sync.WaitGroup

	runStates     map[string]*RunState
	lastFetchAt   map[string]time.Time
	fetchInFlight map[string]bool
	mu            sync.Mutex

	executablePath string
	startupMtime   time.Time
	staleLogged    bool
}

// RunState tracks the monitoring state of a single run
type RunState struct {
	LastOutput     string
	LastOutputAt   time.Time
	LastCheckAt    time.Time
	OutputHash     string
	PRRecorded     bool
	WasAlive       bool
	DeadCheckCount int
}

// New creates a new Daemon instance
func New(vaultPath string, st store.Store) *Daemon {
	return &Daemon{
		vaultPath:     vaultPath,
		store:         st,
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

// Run starts the daemon main loop (blocking)
func (d *Daemon) Run() error {
	// Ensure .orch directory exists
	if err := EnsureOrchDir(d.vaultPath); err != nil {
		return fmt.Errorf("failed to create .orch directory: %w", err)
	}

	// Set up logging
	logPath := LogFilePath(d.vaultPath)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer logFile.Close()
	d.logger = log.New(logFile, "", log.LstdFlags)

	if err := d.initBinaryTracking(); err != nil {
		d.logger.Printf("warning: failed to init binary tracking: %v", err)
	}

	// Write PID file
	if err := WritePID(d.vaultPath); err != nil {
		return err
	}
	defer RemovePID(d.vaultPath)

	d.logger.Printf("daemon started (pid=%d, vault=%s, binary=%s)", os.Getpid(), d.vaultPath, d.executablePath)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	d.monitorAll()

	for {
		select {
		case <-ticker.C:
			d.monitorAll()
			d.checkBinaryStaleness()
		case sig := <-sigCh:
			if sig == syscall.SIGHUP {
				d.logger.Printf("received SIGHUP, restarting with new binary")
				if err := d.restartWithNewBinary(); err != nil {
					d.logger.Printf("restart failed: %v", err)
					continue
				}
				return nil
			}
			d.logger.Printf("received signal %v, shutting down", sig)
			d.Stop()
			return nil
		case <-d.stopCh:
			d.logger.Printf("daemon stopped")
			return nil
		}
	}
}

// Stop signals the daemon to stop
func (d *Daemon) Stop() {
	close(d.stopCh)
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
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(MetadataFilePath(d.vaultPath), data, 0644)
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

	args := []string{d.executablePath, "daemon", "--vault", d.vaultPath}
	return syscall.Exec(d.executablePath, args, os.Environ())
}

func (d *Daemon) monitorAll() {
	runs, err := d.store.ListRuns(&store.ListRunsFilter{
		Status: []model.Status{model.StatusRunning, model.StatusBooting, model.StatusBlocked, model.StatusBlockedAPI, model.StatusPROpen, model.StatusUnknown},
	})
	if err != nil {
		d.logger.Printf("error listing runs: %v", err)
		return
	}

	if len(runs) > 0 {
		d.periodicFetch(runs)
	}

	for _, run := range runs {
		if err := d.monitorRun(run); err != nil {
			d.logger.Printf("error monitoring %s#%s: %v", run.IssueID, run.RunID, err)
		}
	}

	d.cleanupStates(runs)
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

// StartInBackground launches the daemon as a background process
// Returns the PID of the spawned process, or error
func StartInBackground(vaultPath string) (int, error) {
	// Check if already running
	if IsRunning(vaultPath) {
		return GetRunningPID(vaultPath), nil
	}

	// Find the current executable
	executable, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("failed to find executable: %w", err)
	}

	// Start daemon process
	// Use "daemon" subcommand which will be handled by CLI
	cmd := &exec.Cmd{
		Path: executable,
		Args: []string{executable, "daemon", "--vault", vaultPath},
		// Detach from parent process group
		SysProcAttr: &syscall.SysProcAttr{
			Setsid: true,
		},
		// Redirect stdout/stderr to null (daemon logs to file)
		Stdout: nil,
		Stderr: nil,
		Stdin:  nil,
	}

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("failed to start daemon: %w", err)
	}

	// Don't wait for the process - let it run in background
	// The daemon will write its own PID file

	// Give it a moment to start and write PID
	time.Sleep(100 * time.Millisecond)

	return cmd.Process.Pid, nil
}

// Kill stops the daemon for the given vault
func Kill(vaultPath string) error {
	pid := GetRunningPID(vaultPath)
	if pid == 0 {
		return nil // Not running
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	// Send SIGTERM for graceful shutdown
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return err
	}

	// Wait a bit for graceful shutdown
	time.Sleep(500 * time.Millisecond)

	// Clean up PID file if process didn't
	RemovePID(vaultPath)

	return nil
}
