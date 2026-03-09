package daemon

import (
	"log"
	"os"
	"strings"
	"testing"
)

func TestDefaultWorkerIDUsesStableHostIdentity(t *testing.T) {
	orig := currentHostname
	currentHostname = func() (string, error) { return "zeus.example", nil }
	t.Cleanup(func() { currentHostname = orig })

	if got := defaultWorkerID(); got != "host-zeus.example" {
		t.Fatalf("defaultWorkerID() = %q, want %q", got, "host-zeus.example")
	}
}

func TestStartManagedExternalWorkerWithoutIDIsIdempotentPerHost(t *testing.T) {
	orig := currentHostname
	currentHostname = func() (string, error) { return "zeus", nil }
	t.Cleanup(func() { currentHostname = orig })

	logger := log.New(os.Stdout, "", 0)
	server := NewSocketServer(nil, logger)
	server.workerLaunchConfig = func(workerID string) (string, []string, []string, error) {
		return "/bin/sleep", []string{"/bin/sleep", "30"}, nil, nil
	}

	workerID1, pid1, err := server.startManagedExternalWorker("")
	if err != nil {
		t.Fatalf("first startManagedExternalWorker() error = %v", err)
	}
	workerID2, pid2, err := server.startManagedExternalWorker("")
	if err != nil {
		t.Fatalf("second startManagedExternalWorker() error = %v", err)
	}

	if workerID1 != "host-zeus" {
		t.Fatalf("workerID1 = %q, want %q", workerID1, "host-zeus")
	}
	if workerID2 != workerID1 {
		t.Fatalf("workerID2 = %q, want %q", workerID2, workerID1)
	}
	if pid2 != pid1 {
		t.Fatalf("pid2 = %d, want reuse pid %d", pid2, pid1)
	}

	_, _ = server.stopManagedExternalWorker(workerID1, false)
}

func TestStopManagedExternalWorkerWithoutIDStopsDefaultHostWorker(t *testing.T) {
	orig := currentHostname
	currentHostname = func() (string, error) { return "zeus", nil }
	t.Cleanup(func() { currentHostname = orig })

	logger := log.New(os.Stdout, "", 0)
	server := NewSocketServer(nil, logger)
	server.workerLaunchConfig = func(workerID string) (string, []string, []string, error) {
		return "/bin/sleep", []string{"/bin/sleep", "30"}, nil, nil
	}

	workerID, _, err := server.startManagedExternalWorker("")
	if err != nil {
		t.Fatalf("startManagedExternalWorker() error = %v", err)
	}
	if workerID != "host-zeus" {
		t.Fatalf("workerID = %q, want %q", workerID, "host-zeus")
	}

	stopped, err := server.stopManagedExternalWorker("", false)
	if err != nil {
		t.Fatalf("stopManagedExternalWorker() error = %v", err)
	}
	if stopped != 1 {
		t.Fatalf("stopped = %d, want 1", stopped)
	}
}

func TestDefaultManagedWorkerLaunchConfigForcesLocalMode(t *testing.T) {
	path, args, env, err := defaultManagedWorkerLaunchConfig("host-zeus")
	if err != nil {
		t.Fatalf("defaultManagedWorkerLaunchConfig() error = %v", err)
	}
	if path == "" {
		t.Fatal("expected executable path")
	}
	if len(env) != 0 {
		t.Fatalf("env = %v, want nil/empty override env", env)
	}
	if len(args) < 5 {
		t.Fatalf("args = %v, want remote override + worker args", args)
	}
	if args[1] != "--remote=" {
		t.Fatalf("args[1] = %q, want %q", args[1], "--remote=")
	}
}

func TestPrepareManagedWorkerEnvStripsORCHREMOTE(t *testing.T) {
	t.Setenv("ORCH_REMOTE", "skip")
	env := prepareManagedWorkerEnv([]string{"ORCH_REMOTE=zeus:7777", "FOO=bar"})
	for _, kv := range env {
		if strings.HasPrefix(kv, "ORCH_REMOTE=") {
			t.Fatalf("unexpected ORCH_REMOTE in env: %v", env)
		}
	}
	if !containsEnv(env, "FOO=bar") {
		t.Fatalf("expected extra env to survive filtering: %v", env)
	}
}

func containsEnv(env []string, want string) bool {
	for _, kv := range env {
		if kv == want {
			return true
		}
	}
	return false
}
