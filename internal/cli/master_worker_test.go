package cli

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/proboscis/orch/internal/daemon"
	"github.com/proboscis/orch/internal/worker"
)

func TestRootRegistersMasterAndWorkerCommands(t *testing.T) {
	if rootCmd.Commands() == nil {
		t.Fatal("expected root commands to be initialized")
	}

	var masterCmdLong string
	masterHidden := false
	hasMaster := false
	hasWorker := false
	for _, cmd := range rootCmd.Commands() {
		switch cmd.Name() {
		case "master":
			hasMaster = true
			masterHidden = cmd.Hidden
			masterCmdLong = cmd.Long
		case "worker":
			hasWorker = true
		}
	}

	if !hasMaster {
		t.Fatal("expected root command to include 'master'")
	}
	if !masterHidden {
		t.Fatal("expected 'master' command to be hidden from help")
	}
	if !strings.Contains(masterCmdLong, "canonical command name is 'orch daemon'") {
		t.Fatalf("expected master help to point at canonical daemon command, got: %s", masterCmdLong)
	}
	if !hasWorker {
		t.Fatal("expected root command to include 'worker'")
	}
}

func TestWorkerCommandRegistersRunSubcommand(t *testing.T) {
	worker := newWorkerCmd()
	if worker == nil {
		t.Fatal("newWorkerCmd() = nil")
	}

	hasRun := false
	for _, cmd := range worker.Commands() {
		if cmd.Name() == "run" {
			hasRun = true
			break
		}
	}

	if !hasRun {
		t.Fatal("expected worker command to include 'run' subcommand")
	}
}

func TestWorkerRunCommandInvokesExternalLoop(t *testing.T) {
	t.Setenv("TMUX", "/tmp/foreign,123,0")
	t.Setenv("TMUX_PANE", "%999")
	t.Setenv("ZELLIJ", "1")
	t.Setenv("ZELLIJ_SESSION_NAME", "foreign")

	origRequire := requireDaemonForWorker
	origRun := runExternalWorkerLoop
	origLogAvailability := logWorkerAgentAvailability
	t.Cleanup(func() {
		requireDaemonForWorker = origRequire
		runExternalWorkerLoop = origRun
		logWorkerAgentAvailability = origLogAvailability
	})

	requireDaemonForWorker = func() (worker.Client, error) {
		for _, key := range []string{"TMUX", "TMUX_PANE", "ZELLIJ", "ZELLIJ_SESSION_NAME"} {
			if _, ok := os.LookupEnv(key); ok {
				t.Fatalf("worker run reached daemon setup with %s still set", key)
			}
		}
		return &mockWorkerClient{}, nil
	}

	called := false
	availabilityLogged := false
	availabilityWorkerID := ""
	// The availability probe runs on its own goroutine; record and assert
	// after Execute instead of failing from a non-test goroutine.
	logWorkerAgentAvailability = func(workerID string) {
		availabilityLogged = true
		availabilityWorkerID = workerID
	}
	var got worker.RunConfig
	runExternalWorkerLoop = func(client worker.Client, cfg worker.RunConfig) error {
		called = true
		got = cfg
		return nil
	}

	cmd := newWorkerRunCmd()
	cmd.SetArgs([]string{"--worker-id", "worker-1", "--once", "--poll-interval", "300ms", "--heartbeat-interval", "7s"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("worker run execute failed: %v", err)
	}

	if !called {
		t.Fatal("expected runExternalWorkerLoop to be called")
	}
	if !availabilityLogged {
		t.Fatal("expected worker startup to log agent availability")
	}
	if availabilityWorkerID != "worker-1" {
		t.Fatalf("availability worker id = %q, want worker-1", availabilityWorkerID)
	}
	if got.WorkerID != "worker-1" || !got.Once || got.PollInterval != 300*time.Millisecond || got.HeartbeatInterval != 7*time.Second {
		t.Fatalf("unexpected run config: %+v", got)
	}
}

// TestWorkerRunRegistrationNotBlockedByAvailabilityProbe pins the startup
// accounting law behind `orch worker start`: the availability probe is
// diagnostic-only, so the registration path (the worker loop) must run while
// a probe is still in flight. A hanging probe otherwise consumes the managed
// launcher's entire ready-wait budget (both were 5s) and start always fails.
// The process still must not exit before the availability map is logged.
func TestWorkerRunRegistrationNotBlockedByAvailabilityProbe(t *testing.T) {
	origRequire := requireDaemonForWorker
	origRun := runExternalWorkerLoop
	origLogAvailability := logWorkerAgentAvailability
	t.Cleanup(func() {
		requireDaemonForWorker = origRequire
		runExternalWorkerLoop = origRun
		logWorkerAgentAvailability = origLogAvailability
	})

	requireDaemonForWorker = func() (worker.Client, error) {
		return &mockWorkerClient{}, nil
	}

	probeStarted := make(chan struct{})
	probeRelease := make(chan struct{})
	probeUnblockedByLoop := false
	probeFinished := false
	logWorkerAgentAvailability = func(string) {
		close(probeStarted)
		// Simulate a hanging agent CLI: only the worker loop having already
		// run releases the probe. If registration still waited on the probe,
		// this would time out and fail the ordering assertion below.
		select {
		case <-probeRelease:
			probeUnblockedByLoop = true
		case <-time.After(2 * time.Second):
		}
		probeFinished = true
	}

	loopCalled := false
	runExternalWorkerLoop = func(client worker.Client, cfg worker.RunConfig) error {
		loopCalled = true
		select {
		case <-probeStarted:
		case <-time.After(2 * time.Second):
			t.Error("availability probe was never started")
		}
		close(probeRelease)
		return nil
	}

	cmd := newWorkerRunCmd()
	cmd.SetArgs([]string{"--worker-id", "worker-probe", "--once"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("worker run execute failed: %v", err)
	}

	if !loopCalled {
		t.Fatal("expected runExternalWorkerLoop to be called")
	}
	if !probeUnblockedByLoop {
		t.Fatal("worker loop did not run while the availability probe was still in flight; registration is serialized behind probing again")
	}
	if !probeFinished {
		t.Fatal("worker run returned before the availability map was logged")
	}
}

type mockWorkerClient struct{}

func (m *mockWorkerClient) RegisterWorker(workerID, workerType, host, mode string) (*daemon.RegisterWorkerResponse, error) {
	return nil, nil
}

func (m *mockWorkerClient) UnregisterWorker(workerID string) error {
	return nil
}

func (m *mockWorkerClient) WorkerHeartbeat(workerID string) error {
	return nil
}

func (m *mockWorkerClient) LeaseWork(workerID string) (*daemon.LeaseWorkResponse, error) {
	return nil, nil
}

func (m *mockWorkerClient) AcknowledgeEffect(workerID, leaseID string, success bool, effectErr, resultJSON string) error {
	return nil
}

func (m *mockWorkerClient) Close() error {
	return nil
}

func TestRunWorkerStatusWithoutProjectRoot(t *testing.T) {
	setIsolatedXDG(t)
	resetGlobalOpts(t)
	out := captureStdout(t, func() {
		if err := runWorkerStatus(""); err != nil {
			t.Fatalf("runWorkerStatus() error = %v", err)
		}
	})
	if !strings.Contains(out, "Local Process: missing") {
		t.Fatalf("expected local-process output, got: %s", out)
	}
	if !strings.Contains(out, "Master Registration: unreachable") {
		t.Fatalf("expected unreachable master output, got: %s", out)
	}
}
