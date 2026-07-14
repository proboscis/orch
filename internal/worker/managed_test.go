package worker

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
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
	if stopped != 1 {
		t.Fatalf("stopped = %d, want 1", stopped)
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

func TestStopStartManagedConvergesSameWorkerIDAcrossProfiles(t *testing.T) {
	setManagedWorkerTestEnv(t)
	withManagedWorkerTestHooks(t)

	managedWorkerStartupTimeout = 2 * time.Second
	managedWorkerStartupPoll = 25 * time.Millisecond
	managedWorkerLaunchConfig = helperManagedWorkerLaunchConfig("registered")
	lookupManagedWorkerRegistration = lookupRegistrationFromManagedState

	const workerID = "worker-reconcile"
	oldProfile := ManagedOptions{WorkerID: workerID, RemoteAddr: "127.0.0.1:7777"}
	currentProfile := ManagedOptions{WorkerID: workerID}

	oldPID := startUnreconciledManagedWorkerHelper(t, oldProfile)
	currentPID := startUnreconciledManagedWorkerHelper(t, currentProfile)
	for _, opts := range []ManagedOptions{oldProfile, currentProfile} {
		profile, err := resolveManagedProfile(opts)
		if err != nil {
			t.Fatalf("resolveManagedProfile(%+v) error = %v", opts, err)
		}
		if err := os.Remove(profile.StatePath); err != nil {
			t.Fatalf("remove %s to create orphan: %v", profile.StatePath, err)
		}
		if err := removeManagedPID(profile.PIDPath); err != nil {
			t.Fatalf("remove %s to create orphan: %v", profile.PIDPath, err)
		}
	}
	listManagedWorkerProcesses = func() ([]managedWorkerProcess, error) {
		return []managedWorkerProcess{
			{PID: oldPID, Command: "orch --remote=127.0.0.1:7777 worker run --worker-id " + workerID},
			{PID: currentPID, Command: "orch --remote= worker run --worker-id " + workerID},
		}, nil
	}
	t.Cleanup(func() {
		_, _ = StopManaged(ManagedOptions{}, true)
	})

	stopped, err := StopManaged(currentProfile, false)
	if err != nil {
		t.Fatalf("StopManaged(current profile) error = %v", err)
	}
	if stopped == 0 {
		t.Fatal("StopManaged(current profile) stopped no workers")
	}
	if stopped != 2 {
		t.Fatalf("StopManaged(current profile) stopped = %d, want 2 orphaned processes", stopped)
	}

	restarted, err := StartManaged(currentProfile)
	if err != nil {
		t.Fatalf("StartManaged(current profile after stop) error = %v", err)
	}

	live := 0
	for _, pid := range []int{oldPID, currentPID, restarted.PID} {
		if daemon.IsProcessRunning(pid) {
			live++
		}
	}
	if live != 1 {
		t.Fatalf("live worker processes after stop/start = %d, want 1 (pids: old=%d current=%d restarted=%d)", live, oldPID, currentPID, restarted.PID)
	}
}

func TestManagedWorkerCommandHasIdentity(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		workerID  string
		wantMatch bool
	}{
		{
			name:      "separate worker id flag",
			command:   "/usr/local/bin/orch --remote 127.0.0.1:7777 worker run --worker-id host-zeus",
			workerID:  "host-zeus",
			wantMatch: true,
		},
		{
			name:      "equals worker id flag",
			command:   "/tmp/orch-next worker run --worker-id=host-zeus",
			workerID:  "host-zeus",
			wantMatch: true,
		},
		{
			name:      "different worker id",
			command:   "orch worker run --worker-id host-athena",
			workerID:  "host-zeus",
			wantMatch: false,
		},
		{
			name:      "worker text in another command",
			command:   "orch send run-1 worker run --worker-id host-zeus",
			workerID:  "host-zeus",
			wantMatch: false,
		},
		{
			name:      "non orch executable",
			command:   "sh worker run --worker-id host-zeus",
			workerID:  "host-zeus",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := managedWorkerCommandHasIdentity(tt.command, tt.workerID); got != tt.wantMatch {
				t.Fatalf("managedWorkerCommandHasIdentity(%q, %q) = %t, want %t", tt.command, tt.workerID, got, tt.wantMatch)
			}
		})
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
	origListProcesses := listManagedWorkerProcesses
	origNow := managedWorkerNow
	t.Cleanup(func() {
		managedWorkerStartupTimeout = origTimeout
		managedWorkerStartupPoll = origPoll
		managedWorkerQueryTimeout = origQueryTimeout
		managedWorkerLaunchConfig = origLaunch
		lookupManagedWorkerRegistration = origLookup
		listManagedWorkerProcesses = origListProcesses
		managedWorkerNow = origNow
	})
	listManagedWorkerProcesses = func() ([]managedWorkerProcess, error) { return nil, nil }
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

func startUnreconciledManagedWorkerHelper(t *testing.T, opts ManagedOptions) int {
	t.Helper()

	profile, err := resolveManagedProfile(opts)
	if err != nil {
		t.Fatalf("resolveManagedProfile(%+v) error = %v", opts, err)
	}
	path, args, extraEnv, err := helperManagedWorkerLaunchConfig("unregistered")(profile)
	if err != nil {
		t.Fatalf("helperManagedWorkerLaunchConfig() error = %v", err)
	}
	cmd := &exec.Cmd{
		Path: path,
		Args: args,
		Env:  mergeManagedEnv(os.Environ(), extraEnv, profile),
		SysProcAttr: &syscall.SysProcAttr{
			Setsid: true,
		},
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start unreconciled helper for %+v: %v", opts, err)
	}
	go func() { _ = cmd.Wait() }()
	t.Cleanup(func() { _ = stopManagedProcess(cmd.Process.Pid) })

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state, err := loadManagedState(profile.StatePath)
		if err == nil && state.PID == cmd.Process.Pid && state.ProcessState == localProcessStateStarting {
			return cmd.Process.Pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("unreconciled helper pid %d did not write starting state", cmd.Process.Pid)
	return 0
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
