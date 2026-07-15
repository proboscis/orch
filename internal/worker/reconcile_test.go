package worker

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/proboscis/orch/internal/daemon"
)

func TestParseWorkerRunClaim(t *testing.T) {
	tests := []struct {
		name  string
		argv  []string
		claim string
		ok    bool
	}{
		{
			name:  "incident form: worker run with id and trailing remote",
			argv:  []string{"orch", "worker", "run", "--worker-id", "host-zeus", "--remote=127.0.0.1:7777"},
			claim: "host-zeus",
			ok:    true,
		},
		{
			name:  "managed launcher form: leading remote flag",
			argv:  []string{"/usr/local/bin/orch", "--remote=", "worker", "run", "--worker-id", "host-zeus"},
			claim: "host-zeus",
			ok:    true,
		},
		{
			name:  "equals form worker id",
			argv:  []string{"orch", "worker", "run", "--worker-id=host-a"},
			claim: "host-a",
			ok:    true,
		},
		{
			name:  "no worker id claims host default",
			argv:  []string{"orch", "worker", "run", "--once"},
			claim: "",
			ok:    true,
		},
		{
			name: "free text argument of another command does not match",
			argv: []string{"orch", "send", "75731b", "worker", "run"},
			ok:   false,
		},
		{
			name: "worker stop does not match",
			argv: []string{"orch", "worker", "stop", "--all"},
			ok:   false,
		},
		{
			name: "non-orch binary does not match",
			argv: []string{"grep", "worker", "run"},
			ok:   false,
		},
		{
			name: "shell wrapper does not match",
			argv: []string{"sh", "-c", "orch worker run"},
			ok:   false,
		},
		{
			name: "bare worker without run does not match",
			argv: []string{"orch", "worker"},
			ok:   false,
		},
		{
			name: "empty argv does not match",
			argv: nil,
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claim, ok := parseWorkerRunClaim(tt.argv)
			if ok != tt.ok {
				t.Fatalf("parseWorkerRunClaim(%v) ok = %v, want %v", tt.argv, ok, tt.ok)
			}
			if ok && claim != tt.claim {
				t.Fatalf("parseWorkerRunClaim(%v) claim = %q, want %q", tt.argv, claim, tt.claim)
			}
		})
	}
}

func TestParseWorkerProcessTable(t *testing.T) {
	table := `  101 /usr/local/bin/orch --remote=127.0.0.1:7777 worker run --worker-id host-zeus
  102 orch worker run
  103 grep worker run
 not-a-pid orch worker run
  104 orch worker stop --all
`
	procs := parseWorkerProcessTable(table)
	if len(procs) != 2 {
		t.Fatalf("parseWorkerProcessTable() = %+v, want 2 entries", procs)
	}
	if procs[0].PID != 101 || procs[0].WorkerID != "host-zeus" {
		t.Fatalf("first entry = %+v, want pid 101 claiming host-zeus", procs[0])
	}
	if procs[1].PID != 102 || procs[1].WorkerID != "" {
		t.Fatalf("second entry = %+v, want pid 102 claiming host default", procs[1])
	}
}

// spawnSleeperHelper starts a helper process that idles until it receives
// SIGINT/SIGTERM, optionally disguising its argv so the real process-table
// scan sees an `orch worker run` claimant.
func spawnSleeperHelper(t *testing.T, argv []string) *exec.Cmd {
	t.Helper()
	if len(argv) == 0 {
		argv = []string{os.Args[0], "-test.run=TestManagedWorkerHelperProcess"}
	}
	cmd := &exec.Cmd{
		Path: os.Args[0],
		Args: argv,
		Env: append(os.Environ(),
			"ORCH_TEST_MANAGED_HELPER=1",
			"ORCH_TEST_MANAGED_HELPER_MODE=unregistered",
		),
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleeper helper: %v", err)
	}
	waitCh := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(waitCh)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGKILL)
		select {
		case <-waitCh:
		case <-time.After(5 * time.Second):
		}
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !daemon.IsProcessRunning(cmd.Process.Pid) {
		time.Sleep(10 * time.Millisecond)
	}
	return cmd
}

func waitForProcessExit(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !daemon.IsProcessRunning(pid) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("process %d still running after %s", pid, timeout)
}

func TestReconcileWorkerIDStopsOnlyMatchingClaimants(t *testing.T) {
	withManagedWorkerTestHooks(t)

	orphan := spawnSleeperHelper(t, nil)
	otherID := spawnSleeperHelper(t, nil)
	kept := spawnSleeperHelper(t, nil)

	listWorkerProcesses = func() ([]workerProcess, error) {
		return []workerProcess{
			{PID: orphan.Process.Pid, WorkerID: "worker-reconcile"},
			{PID: otherID.Process.Pid, WorkerID: "worker-unrelated"},
			{PID: kept.Process.Pid, WorkerID: "worker-reconcile"},
		}, nil
	}

	stopped, err := reconcileWorkerID("worker-reconcile", kept.Process.Pid)
	if err != nil {
		t.Fatalf("reconcileWorkerID() error = %v", err)
	}
	if len(stopped) != 1 || stopped[0] != orphan.Process.Pid {
		t.Fatalf("stopped = %v, want [%d]", stopped, orphan.Process.Pid)
	}
	waitForProcessExit(t, orphan.Process.Pid, 3*time.Second)
	if !daemon.IsProcessRunning(otherID.Process.Pid) {
		t.Fatal("process claiming a different worker id was stopped")
	}
	if !daemon.IsProcessRunning(kept.Process.Pid) {
		t.Fatal("keepPID process was stopped")
	}
}

func TestReconcileWorkerIDResolvesDefaultClaims(t *testing.T) {
	withManagedWorkerTestHooks(t)

	origHostname := currentWorkerHostname
	currentWorkerHostname = func() (string, error) { return "testbox", nil }
	t.Cleanup(func() { currentWorkerHostname = origHostname })

	orphan := spawnSleeperHelper(t, nil)
	listWorkerProcesses = func() ([]workerProcess, error) {
		return []workerProcess{{PID: orphan.Process.Pid, WorkerID: ""}}, nil
	}

	stopped, err := reconcileWorkerID("", 0)
	if err != nil {
		t.Fatalf("reconcileWorkerID() error = %v", err)
	}
	if len(stopped) != 1 || stopped[0] != orphan.Process.Pid {
		t.Fatalf("stopped = %v, want [%d]", stopped, orphan.Process.Pid)
	}
	waitForProcessExit(t, orphan.Process.Pid, 3*time.Second)
}

func TestReconcileAllWorkerProcessesSweepsEveryClaimant(t *testing.T) {
	withManagedWorkerTestHooks(t)

	first := spawnSleeperHelper(t, nil)
	second := spawnSleeperHelper(t, nil)
	listWorkerProcesses = func() ([]workerProcess, error) {
		return []workerProcess{
			{PID: first.Process.Pid, WorkerID: "worker-a"},
			{PID: second.Process.Pid, WorkerID: "worker-b"},
		}, nil
	}

	stopped, err := reconcileAllWorkerProcesses()
	if err != nil {
		t.Fatalf("reconcileAllWorkerProcesses() error = %v", err)
	}
	if len(stopped) != 2 {
		t.Fatalf("stopped = %v, want both sleeper pids", stopped)
	}
	waitForProcessExit(t, first.Process.Pid, 3*time.Second)
	waitForProcessExit(t, second.Process.Pid, 3*time.Second)
}

// TestDefaultListWorkerProcessesFindsRealClaimant exercises the real ps scan
// end to end: a live process whose argv looks like `orch ... worker run
// --worker-id <uniq>` must be discovered and reconciled away. The worker id
// is unique per test process so the scan can never select an unrelated
// process on the machine running the tests.
func TestDefaultListWorkerProcessesFindsRealClaimant(t *testing.T) {
	uniq := fmt.Sprintf("orch-test-reconcile-%d", os.Getpid())
	orphan := spawnSleeperHelper(t, []string{
		"orch", "-test.run=TestManagedWorkerHelperProcess", "worker", "run", "--worker-id", uniq,
	})

	procs, err := defaultListWorkerProcesses()
	if err != nil {
		t.Fatalf("defaultListWorkerProcesses() error = %v", err)
	}
	found := false
	for _, proc := range procs {
		if proc.PID == orphan.Process.Pid {
			if proc.WorkerID != uniq {
				t.Fatalf("scan claim = %q, want %q", proc.WorkerID, uniq)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("real ps scan did not find sleeper pid %d claiming %q", orphan.Process.Pid, uniq)
	}

	stopped, err := reconcileWorkerID(uniq, 0)
	if err != nil {
		t.Fatalf("reconcileWorkerID() error = %v", err)
	}
	if len(stopped) != 1 || stopped[0] != orphan.Process.Pid {
		t.Fatalf("stopped = %v, want [%d]", stopped, orphan.Process.Pid)
	}
	waitForProcessExit(t, orphan.Process.Pid, 3*time.Second)
}
