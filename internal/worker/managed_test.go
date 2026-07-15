package worker

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/proboscis/orch/internal/daemon"
	"github.com/proboscis/orch/internal/xdg"
)

func TestManagedWorkerHelperProcess(t *testing.T) {
	if os.Getenv("ORCH_TEST_MANAGED_HELPER") != "1" {
		return
	}

	runtimeState := managedRuntimeStateFromEnv()
	workerID := os.Getenv("ORCH_TEST_MANAGED_HELPER_WORKER_ID")
	mode := os.Getenv("ORCH_TEST_MANAGED_HELPER_MODE")
	runtimeState.markStarting(workerID)

	switch mode {
	case "registered":
		runtimeState.markRegistered()
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(sigCh)
		for {
			select {
			case <-ticker.C:
				runtimeState.markHeartbeat()
			case <-sigCh:
				runtimeState.markExited(nil)
				os.Exit(0)
			}
		}
	case "unregistered":
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(sigCh)
		<-sigCh
		runtimeState.markExited(nil)
		os.Exit(0)
	case "exit-before-register":
		runtimeState.markExited(fmt.Errorf("exit status 7"))
		os.Exit(7)
	default:
		os.Exit(2)
	}
}

func TestStartManagedFailsFastWhenProcessExitsBeforeRegister(t *testing.T) {
	setManagedWorkerTestEnv(t)
	withManagedWorkerTestHooks(t)

	managedWorkerStartupTimeout = 500 * time.Millisecond
	managedWorkerStartupPoll = 25 * time.Millisecond
	managedWorkerLaunchConfig = helperManagedWorkerLaunchConfig("exit-before-register")
	lookupManagedWorkerRegistration = func(remoteAddr, workerID string) (*daemon.WorkerRegistration, error) {
		return nil, nil
	}

	_, err := StartManaged(ManagedOptions{WorkerID: "worker-fail", RemoteAddr: "remotebox:7777"})
	if err == nil {
		t.Fatal("expected StartManaged to fail")
	}
	if !strings.Contains(err.Error(), "exited before registering") {
		t.Fatalf("unexpected error: %v", err)
	}

	status, err := StatusManaged(ManagedOptions{WorkerID: "worker-fail", RemoteAddr: "remotebox:7777"})
	if err != nil {
		t.Fatalf("StatusManaged() error = %v", err)
	}
	if status.Local.State != localProcessStateExited {
		t.Fatalf("local state = %q, want %q", status.Local.State, localProcessStateExited)
	}
	if status.Local.ProcessExists {
		t.Fatal("expected no running process after fail-fast exit")
	}
	if status.Local.LastError == "" {
		t.Fatal("expected last error to be recorded")
	}
}

func TestStartStatusStopManagedUsesPersistentLocalState(t *testing.T) {
	setManagedWorkerTestEnv(t)
	withManagedWorkerTestHooks(t)

	managedWorkerStartupTimeout = 2 * time.Second
	managedWorkerStartupPoll = 25 * time.Millisecond
	managedWorkerLaunchConfig = helperManagedWorkerLaunchConfig("registered")
	lookupManagedWorkerRegistration = lookupRegistrationFromManagedState

	opts := ManagedOptions{WorkerID: "worker-live", RemoteAddr: "remotebox:7777"}
	start, err := StartManaged(opts)
	if err != nil {
		t.Fatalf("StartManaged() error = %v", err)
	}
	if start.PID == 0 {
		t.Fatal("expected started pid")
	}
	if start.LogPath == "" {
		t.Fatal("expected log path")
	}
	t.Cleanup(func() {
		_, _ = StopManaged(opts, false)
	})

	status, err := StatusManaged(opts)
	if err != nil {
		t.Fatalf("StatusManaged() error = %v", err)
	}
	if !status.Local.Managed || !status.Local.ProcessExists {
		t.Fatalf("unexpected local status: %+v", status.Local)
	}
	if status.Local.State != localProcessStateRunning {
		t.Fatalf("local state = %q, want %q", status.Local.State, localProcessStateRunning)
	}
	if status.Master.State != masterStateActive {
		t.Fatalf("master state = %q, want %q", status.Master.State, masterStateActive)
	}

	stopped, err := StopManaged(opts, false)
	if err != nil {
		t.Fatalf("StopManaged() error = %v", err)
	}
	if stopped.StoppedCount != 1 {
		t.Fatalf("stopped = %d, want 1", stopped.StoppedCount)
	}
	if len(stopped.OrphanPIDs) != 0 {
		t.Fatalf("orphan pids = %v, want none", stopped.OrphanPIDs)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, err = StatusManaged(opts)
		if err != nil {
			t.Fatalf("StatusManaged() error = %v", err)
		}
		if !status.Local.ProcessExists {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if status.Local.ProcessExists {
		t.Fatal("expected managed worker process to stop")
	}
	if status.Local.State != localProcessStateStopped && status.Local.State != localProcessStateExited {
		t.Fatalf("local state after stop = %q", status.Local.State)
	}
}

func TestStatusManagedReportsUnmanagedRegistration(t *testing.T) {
	setManagedWorkerTestEnv(t)
	withManagedWorkerTestHooks(t)

	lookupManagedWorkerRegistration = func(remoteAddr, workerID string) (*daemon.WorkerRegistration, error) {
		return &daemon.WorkerRegistration{
			ID:            workerID,
			Host:          "mac-host",
			Mode:          "external",
			WorkerType:    "executor",
			RegisteredAt:  time.Now().Add(-time.Minute),
			LastHeartbeat: time.Now(),
			Active:        true,
		}, nil
	}

	status, err := StatusManaged(ManagedOptions{WorkerID: "worker-unmanaged", RemoteAddr: "remotebox:7777"})
	if err != nil {
		t.Fatalf("StatusManaged() error = %v", err)
	}
	if status.Local.State != localProcessStateUnmanaged {
		t.Fatalf("local state = %q, want %q", status.Local.State, localProcessStateUnmanaged)
	}
	if status.Diagnostic == "" {
		t.Fatal("expected unmanaged diagnostic")
	}
}

func TestStopManagedStopsUnmanagedClaimantsWithoutStateFile(t *testing.T) {
	setManagedWorkerTestEnv(t)
	withManagedWorkerTestHooks(t)

	lookupManagedWorkerRegistration = func(remoteAddr, workerID string) (*daemon.WorkerRegistration, error) {
		return nil, nil
	}

	orphan := spawnSleeperHelper(t, nil)
	listWorkerProcesses = func() ([]workerProcess, error) {
		return []workerProcess{{PID: orphan.Process.Pid, WorkerID: "worker-orphan"}}, nil
	}

	res, err := StopManaged(ManagedOptions{WorkerID: "worker-orphan", RemoteAddr: "remotebox:7777"}, false)
	if err != nil {
		t.Fatalf("StopManaged() error = %v", err)
	}
	if res.StoppedCount != 1 {
		t.Fatalf("stopped = %d, want 1", res.StoppedCount)
	}
	if len(res.OrphanPIDs) != 1 || res.OrphanPIDs[0] != orphan.Process.Pid {
		t.Fatalf("orphan pids = %v, want [%d]", res.OrphanPIDs, orphan.Process.Pid)
	}
	waitForProcessExit(t, orphan.Process.Pid, 3*time.Second)
}

func TestStopManagedStopsManagedProcessAndOrphanClaimant(t *testing.T) {
	setManagedWorkerTestEnv(t)
	withManagedWorkerTestHooks(t)

	managedWorkerStartupTimeout = 2 * time.Second
	managedWorkerStartupPoll = 25 * time.Millisecond
	managedWorkerLaunchConfig = helperManagedWorkerLaunchConfig("registered")
	lookupManagedWorkerRegistration = lookupRegistrationFromManagedState

	opts := ManagedOptions{WorkerID: "worker-live", RemoteAddr: "remotebox:7777"}
	start, err := StartManaged(opts)
	if err != nil {
		t.Fatalf("StartManaged() error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = StopManaged(opts, false)
	})

	orphan := spawnSleeperHelper(t, nil)
	listWorkerProcesses = func() ([]workerProcess, error) {
		return []workerProcess{
			{PID: start.PID, WorkerID: "worker-live"},
			{PID: orphan.Process.Pid, WorkerID: "worker-live"},
		}, nil
	}

	res, err := StopManaged(opts, false)
	if err != nil {
		t.Fatalf("StopManaged() error = %v", err)
	}
	if res.StoppedCount != 2 {
		t.Fatalf("stopped = %d, want 2 (managed + orphan)", res.StoppedCount)
	}
	if len(res.OrphanPIDs) != 1 || res.OrphanPIDs[0] != orphan.Process.Pid {
		t.Fatalf("orphan pids = %v, want [%d]", res.OrphanPIDs, orphan.Process.Pid)
	}
	waitForProcessExit(t, start.PID, 3*time.Second)
	waitForProcessExit(t, orphan.Process.Pid, 3*time.Second)
}

func TestStopManagedAllSweepsUnrecordedWorkerProcesses(t *testing.T) {
	setManagedWorkerTestEnv(t)
	withManagedWorkerTestHooks(t)

	orphan := spawnSleeperHelper(t, nil)
	listWorkerProcesses = func() ([]workerProcess, error) {
		return []workerProcess{{PID: orphan.Process.Pid, WorkerID: "worker-unrecorded"}}, nil
	}

	res, err := StopManaged(ManagedOptions{}, true)
	if err != nil {
		t.Fatalf("StopManaged(all) error = %v", err)
	}
	if res.StoppedCount != 1 {
		t.Fatalf("stopped = %d, want 1", res.StoppedCount)
	}
	if len(res.OrphanPIDs) != 1 || res.OrphanPIDs[0] != orphan.Process.Pid {
		t.Fatalf("orphan pids = %v, want [%d]", res.OrphanPIDs, orphan.Process.Pid)
	}
	waitForProcessExit(t, orphan.Process.Pid, 3*time.Second)
}

func TestStartManagedReconcilesOrphanClaimantBeforeLaunch(t *testing.T) {
	setManagedWorkerTestEnv(t)
	withManagedWorkerTestHooks(t)

	managedWorkerStartupTimeout = 2 * time.Second
	managedWorkerStartupPoll = 25 * time.Millisecond
	managedWorkerLaunchConfig = helperManagedWorkerLaunchConfig("registered")

	// The orphan holds an active master registration (the incident state:
	// registered worker, no local supervisor metadata). Without a local
	// claimant this blocks start; with one, reconcile removes it and start
	// proceeds.
	lookupManagedWorkerRegistration = func(remoteAddr, workerID string) (*daemon.WorkerRegistration, error) {
		return &daemon.WorkerRegistration{
			ID:            workerID,
			Host:          "test-host",
			Mode:          "external",
			WorkerType:    "executor",
			RegisteredAt:  time.Now().Add(-time.Hour),
			LastHeartbeat: time.Now(),
			Active:        true,
		}, nil
	}

	orphan := spawnSleeperHelper(t, nil)
	listWorkerProcesses = func() ([]workerProcess, error) {
		return []workerProcess{{PID: orphan.Process.Pid, WorkerID: "worker-takeover"}}, nil
	}

	opts := ManagedOptions{WorkerID: "worker-takeover", RemoteAddr: "remotebox:7777"}
	start, err := StartManaged(opts)
	if err != nil {
		t.Fatalf("StartManaged() error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = StopManaged(opts, false)
	})
	if start.Reused {
		t.Fatalf("expected fresh launch, got reuse: %+v", start)
	}
	if len(start.OrphanPIDs) != 1 || start.OrphanPIDs[0] != orphan.Process.Pid {
		t.Fatalf("orphan pids = %v, want [%d]", start.OrphanPIDs, orphan.Process.Pid)
	}
	waitForProcessExit(t, orphan.Process.Pid, 3*time.Second)
	if !daemon.IsProcessRunning(start.PID) {
		t.Fatal("expected managed worker process to be running")
	}
}

func TestStartManagedStillRefusesActiveRegistrationWithoutLocalClaimant(t *testing.T) {
	setManagedWorkerTestEnv(t)
	withManagedWorkerTestHooks(t)

	lookupManagedWorkerRegistration = func(remoteAddr, workerID string) (*daemon.WorkerRegistration, error) {
		return &daemon.WorkerRegistration{
			ID:            workerID,
			Host:          "other-host",
			Mode:          "external",
			WorkerType:    "executor",
			RegisteredAt:  time.Now().Add(-time.Hour),
			LastHeartbeat: time.Now(),
			Active:        true,
		}, nil
	}

	_, err := StartManaged(ManagedOptions{WorkerID: "worker-elsewhere", RemoteAddr: "remotebox:7777"})
	if err == nil {
		t.Fatal("expected StartManaged to fail")
	}
	if !strings.Contains(err.Error(), "already active") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStartManagedReusesManagedProcessAndStopsOrphan(t *testing.T) {
	setManagedWorkerTestEnv(t)
	withManagedWorkerTestHooks(t)

	managedWorkerStartupTimeout = 2 * time.Second
	managedWorkerStartupPoll = 25 * time.Millisecond
	managedWorkerLaunchConfig = helperManagedWorkerLaunchConfig("registered")
	lookupManagedWorkerRegistration = lookupRegistrationFromManagedState

	opts := ManagedOptions{WorkerID: "worker-live", RemoteAddr: "remotebox:7777"}
	start, err := StartManaged(opts)
	if err != nil {
		t.Fatalf("StartManaged() error = %v", err)
	}
	t.Cleanup(func() {
		_, _ = StopManaged(opts, false)
	})

	orphan := spawnSleeperHelper(t, nil)
	listWorkerProcesses = func() ([]workerProcess, error) {
		return []workerProcess{
			{PID: start.PID, WorkerID: "worker-live"},
			{PID: orphan.Process.Pid, WorkerID: "worker-live"},
		}, nil
	}

	again, err := StartManaged(opts)
	if err != nil {
		t.Fatalf("second StartManaged() error = %v", err)
	}
	if !again.Reused || again.PID != start.PID {
		t.Fatalf("expected reuse of pid %d, got %+v", start.PID, again)
	}
	if len(again.OrphanPIDs) != 1 || again.OrphanPIDs[0] != orphan.Process.Pid {
		t.Fatalf("orphan pids = %v, want [%d]", again.OrphanPIDs, orphan.Process.Pid)
	}
	waitForProcessExit(t, orphan.Process.Pid, 3*time.Second)
	if !daemon.IsProcessRunning(start.PID) {
		t.Fatal("managed worker process must survive the reuse reconcile")
	}
}

func TestManagedWorkerIdentityLockBlocksSameWorkerID(t *testing.T) {
	setManagedWorkerTestEnv(t)
	withManagedWorkerTestHooks(t)

	lookupManagedWorkerRegistration = func(remoteAddr, workerID string) (*daemon.WorkerRegistration, error) {
		return nil, nil
	}

	lock, err := acquireManagedWorkerIdentityLock("worker-serial")
	if err != nil {
		t.Fatalf("acquireManagedWorkerIdentityLock() error = %v", err)
	}

	// The lock is keyed by worker id alone: a stop for the same id under a
	// DIFFERENT --remote profile must still contend on it.
	done := make(chan struct{})
	go func() {
		_, _ = StopManaged(ManagedOptions{WorkerID: "worker-serial", RemoteAddr: "remotebox:7777"}, false)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("StopManaged completed while the identity lock was held")
	case <-time.After(300 * time.Millisecond):
	}

	releaseManagedWorkerIdentityLock(lock)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("StopManaged did not proceed after the identity lock was released")
	}
}

func TestManagedLifecycleSerializesPerWorkerID(t *testing.T) {
	setManagedWorkerTestEnv(t)
	withManagedWorkerTestHooks(t)

	managedWorkerStartupTimeout = 2 * time.Second
	managedWorkerStartupPoll = 25 * time.Millisecond
	managedWorkerLaunchConfig = helperManagedWorkerLaunchConfig("registered")
	lookupManagedWorkerRegistration = lookupRegistrationFromManagedState

	// The process-table scan runs exactly once per start/stop, inside the
	// identity lock. Two probes in flight at once would mean two lifecycle
	// transitions interleaved their reconcile critical sections.
	var inCritical, overlaps atomic.Int32
	listWorkerProcesses = func() ([]workerProcess, error) {
		if inCritical.Add(1) > 1 {
			overlaps.Add(1)
		}
		time.Sleep(100 * time.Millisecond)
		inCritical.Add(-1)
		return nil, nil
	}

	opts := ManagedOptions{WorkerID: "worker-serial", RemoteAddr: "remotebox:7777"}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = StartManaged(opts)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = StopManaged(opts, false)
		}()
	}
	wg.Wait()

	if n := overlaps.Load(); n != 0 {
		t.Fatalf("reconcile critical sections overlapped %d times; the identity lock must serialize start/stop for one worker id", n)
	}
	if _, err := StopManaged(opts, false); err != nil {
		t.Fatalf("final StopManaged() error = %v", err)
	}
}

func TestMergeManagedEnvScrubsMultiplexerVars(t *testing.T) {
	profile := managedProfile{
		StatePath:  "/tmp/state.json",
		PIDPath:    "/tmp/worker.pid",
		RemoteAddr: "remotebox:7777",
		LogPath:    "/tmp/worker.log",
	}
	env := mergeManagedEnv(
		[]string{
			"PATH=/bin",
			"TMUX=/private/tmp/tmux-501/agent-deck,123,0",
			"ZELLIJ=1",
			managedWorkerStateEnv + "=/old/state.json",
		},
		[]string{
			"FOO=bar",
			"TMUX_PANE=%1",
			"ZELLIJ_SESSION_NAME=foreign",
		},
		profile,
	)

	joined := strings.Join(env, "\n")
	for _, key := range workerMultiplexerEnvKeys {
		if strings.Contains(joined, key+"=") {
			t.Fatalf("managed worker env leaked %s: %v", key, env)
		}
	}
	if !strings.Contains(joined, "PATH=/bin") || !strings.Contains(joined, "FOO=bar") {
		t.Fatalf("managed worker env dropped non-multiplexer entries: %v", env)
	}
	if strings.Contains(joined, managedWorkerStateEnv+"=/old/state.json") {
		t.Fatalf("managed worker env kept stale runtime state entry: %v", env)
	}
	if !strings.Contains(joined, managedWorkerStateEnv+"="+profile.StatePath) {
		t.Fatalf("managed worker env missing runtime state path: %v", env)
	}
}

func setManagedWorkerTestEnv(t *testing.T) {
	t.Helper()

	base := t.TempDir()
	t.Setenv("HOME", filepath.Join(base, "home"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(base, "runtime"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "state"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(base, "data"))
	if err := xdg.EnsureWorkersStateDir(); err != nil {
		t.Fatalf("EnsureWorkersStateDir() error = %v", err)
	}
	if err := xdg.EnsureWorkersRuntimeDir(); err != nil {
		t.Fatalf("EnsureWorkersRuntimeDir() error = %v", err)
	}
}

func withManagedWorkerTestHooks(t *testing.T) {
	t.Helper()

	origTimeout := managedWorkerStartupTimeout
	origPoll := managedWorkerStartupPoll
	origQueryTimeout := managedWorkerQueryTimeout
	origLaunch := managedWorkerLaunchConfig
	origLookup := lookupManagedWorkerRegistration
	origNow := managedWorkerNow
	origList := listWorkerProcesses
	t.Cleanup(func() {
		managedWorkerStartupTimeout = origTimeout
		managedWorkerStartupPoll = origPoll
		managedWorkerQueryTimeout = origQueryTimeout
		managedWorkerLaunchConfig = origLaunch
		lookupManagedWorkerRegistration = origLookup
		managedWorkerNow = origNow
		listWorkerProcesses = origList
	})
	// Hermetic default: never scan (or signal) the real process table from
	// unit tests. Reconcile-specific tests override this seam explicitly.
	listWorkerProcesses = func() ([]workerProcess, error) { return nil, nil }
}

func helperManagedWorkerLaunchConfig(mode string) func(managedProfile) (string, []string, []string, error) {
	return func(profile managedProfile) (string, []string, []string, error) {
		return os.Args[0],
			[]string{os.Args[0], "-test.run=TestManagedWorkerHelperProcess"},
			[]string{
				"ORCH_TEST_MANAGED_HELPER=1",
				"ORCH_TEST_MANAGED_HELPER_MODE=" + mode,
				"ORCH_TEST_MANAGED_HELPER_WORKER_ID=" + profile.WorkerID,
			},
			nil
	}
}

func lookupRegistrationFromManagedState(remoteAddr, workerID string) (*daemon.WorkerRegistration, error) {
	state, err := loadManagedState(filepath.Join(xdg.WorkersStateDir(), managedProfileKey(workerID, remoteAddr)+".json"))
	if err != nil {
		return nil, nil
	}
	if state.ProcessState != localProcessStateRunning {
		return nil, nil
	}
	return &daemon.WorkerRegistration{
		ID:            workerID,
		Host:          "test-host",
		Mode:          "external",
		WorkerType:    "executor",
		RegisteredAt:  state.RegisteredAt,
		LastHeartbeat: state.LastHeartbeatAt,
		Active:        true,
	}, nil
}
