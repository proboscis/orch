package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/s22625/orch/internal/daemon"
	"github.com/s22625/orch/internal/worker"
)

func TestRootRegistersMasterAndWorkerCommands(t *testing.T) {
	if rootCmd.Commands() == nil {
		t.Fatal("expected root commands to be initialized")
	}

	hasMaster := false
	hasWorker := false
	for _, cmd := range rootCmd.Commands() {
		switch cmd.Name() {
		case "master":
			hasMaster = true
		case "worker":
			hasWorker = true
		}
	}

	if !hasMaster {
		t.Fatal("expected root command to include 'master'")
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
	origRequire := requireDaemonForWorker
	origRun := runExternalWorkerLoop
	t.Cleanup(func() {
		requireDaemonForWorker = origRequire
		runExternalWorkerLoop = origRun
	})

	requireDaemonForWorker = func() (*daemon.ProtoClient, error) {
		return &daemon.ProtoClient{}, nil
	}

	called := false
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
	if got.WorkerID != "worker-1" || !got.Once || got.PollInterval != 300*time.Millisecond || got.HeartbeatInterval != 7*time.Second {
		t.Fatalf("unexpected run config: %+v", got)
	}
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
