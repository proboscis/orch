package integration

// Single-point guard for orch worker lifecycle CLI invocations in this suite
// (issue: integration-lifecycle-test-sweeps-real-worker).
//
// `worker start`/`worker stop` reconcile the invariant "at most one local
// process claims a given worker id" against the LIVE HOST PROCESS TABLE
// (ADR-0002 §4, worker-lease.md LL8). HOME/XDG isolation and a private
// --remote daemon do not contain a ps scan, so a test that lets the worker id
// default to the hostname-derived value SIGTERMs the production worker of
// whatever machine runs the tests, and `worker stop --all` sweeps every orch
// worker process on the host regardless of id. Every orch subprocess in this
// package must therefore be built through newOrchCommand, which rejects those
// invocations before any process is spawned.
//
// The reconcile semantics themselves are intentionally untouched: identity is
// worker-id per host, and tests must stay inside a worker-id namespace that
// production workers can never claim.

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/proboscis/orch/internal/daemon"
)

var workerLifecycleSubcommands = map[string]bool{
	"start":  true,
	"stop":   true,
	"status": true,
	"run":    true,
}

// guardWorkerLifecycleArgs returns an error when args invoke an orch worker
// lifecycle subcommand in a way that can reach beyond the test sandbox:
// without an explicit --worker-id, with an id in the hostname-default
// namespace (host-*), or as a host-wide `worker stop --all` sweep.
// Non-worker invocations pass through untouched.
func guardWorkerLifecycleArgs(args []string) error {
	sub := ""
	subIdx := -1
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "worker" && workerLifecycleSubcommands[args[i+1]] {
			sub = args[i+1]
			subIdx = i + 1
			break
		}
	}
	if subIdx == -1 {
		return nil
	}

	workerID := ""
	explicit := false
	all := false
	rest := args[subIdx+1:]
	for i := 0; i < len(rest); i++ {
		if v, ok := strings.CutPrefix(rest[i], "--worker-id="); ok {
			workerID = strings.TrimSpace(v)
			explicit = true
			continue
		}
		if rest[i] == "--worker-id" {
			explicit = true
			if i+1 < len(rest) {
				workerID = strings.TrimSpace(rest[i+1])
				i++
			}
			continue
		}
		if rest[i] == "--all" {
			all = true
		}
	}

	if all {
		return fmt.Errorf("`worker %s --all` reconciles the whole host to zero workers (reconcileAllWorkerProcesses stops every `orch worker run` process regardless of id, production worker included); stop exactly the test worker via --worker-id instead", sub)
	}
	if !explicit || workerID == "" {
		return fmt.Errorf("`worker %s` without an explicit --worker-id resolves to the hostname-default worker id, and the pre-start/stop reconcile would stop the production worker claiming that id on this host; pass a unique test id such as test-lifecycle-<pid>", sub)
	}
	host, _ := os.Hostname()
	if workerID == daemon.HostWorkerID(host) || strings.HasPrefix(workerID, "host-") {
		return fmt.Errorf("worker id %q lies in the hostname-default namespace (host-*) that real workers claim; pass a unique test id such as test-lifecycle-<pid>", workerID)
	}
	return nil
}

// newOrchCommand is the single construction point for orch CLI subprocesses
// in this package. Every helper must build orch commands through it so the
// worker lifecycle guard cannot be bypassed by a new call site. It panics
// (loud failure in both tests and TestMain) instead of spawning an unsafe
// invocation. A fired guard crashes the test process before TestMain's
// shutdown runs, which can leak the suite's private daemon — acceptable for
// an invariant that only fires on test-code bugs, and nothing unsafe has
// been spawned at that point.
func newOrchCommand(args ...string) *exec.Cmd {
	if err := guardWorkerLifecycleArgs(args); err != nil {
		panic(fmt.Sprintf("integration harness blocked unsafe orch invocation %v: %v", args, err))
	}
	return exec.Command(orchBinary, args...)
}

func TestWorkerCLIGuardBlocksDefaultIDLifecycleCalls(t *testing.T) {
	// The first case is the exact invocation that swept the production
	// worker before the fix (PR #559 incident disclosure).
	cases := [][]string{
		{"--remote", "127.0.0.1:1", "worker", "start", "--json"},
		{"--remote", "127.0.0.1:1", "worker", "stop", "--json"},
		{"--remote", "127.0.0.1:1", "worker", "status", "--json"},
		{"worker", "run"},
		{"--remote", "127.0.0.1:1", "worker", "start", "--worker-id", "", "--json"},
		{"--remote", "127.0.0.1:1", "worker", "start", "--worker-id=", "--json"},
	}
	for _, args := range cases {
		if err := guardWorkerLifecycleArgs(args); err == nil {
			t.Errorf("guard allowed default-worker-id invocation %v", args)
		}
	}
}

func TestWorkerCLIGuardBlocksHostNamespaceIDs(t *testing.T) {
	host, _ := os.Hostname()
	cases := [][]string{
		{"worker", "start", "--worker-id", daemon.HostWorkerID(host), "--json"},
		{"worker", "start", "--worker-id", "host-CA-20035844", "--json"},
		{"worker", "stop", "--worker-id=host-someone-else", "--json"},
	}
	for _, args := range cases {
		if err := guardWorkerLifecycleArgs(args); err == nil {
			t.Errorf("guard allowed host-namespace worker id in %v", args)
		}
	}
}

func TestWorkerCLIGuardBlocksStopAll(t *testing.T) {
	cases := [][]string{
		{"--remote", "127.0.0.1:1", "worker", "stop", "--all", "--json"},
		{"worker", "stop", "--worker-id", "test-lifecycle-1", "--all"},
	}
	for _, args := range cases {
		if err := guardWorkerLifecycleArgs(args); err == nil {
			t.Errorf("guard allowed host-wide stop sweep %v", args)
		}
	}
}

func TestWorkerCLIGuardAllowsUniqueTestIDs(t *testing.T) {
	id := fmt.Sprintf("test-lifecycle-%d", os.Getpid())
	cases := [][]string{
		{"--remote", "127.0.0.1:1", "worker", "start", "--worker-id", id, "--json"},
		{"--remote", "127.0.0.1:1", "worker", "status", "--worker-id=" + id, "--json"},
		{"--remote", "127.0.0.1:1", "worker", "stop", "--worker-id", id, "--json"},
	}
	for _, args := range cases {
		if err := guardWorkerLifecycleArgs(args); err != nil {
			t.Errorf("guard rejected safe invocation %v: %v", args, err)
		}
	}
}

func TestWorkerCLIGuardIgnoresNonLifecycleCommands(t *testing.T) {
	cases := [][]string{
		{"ps", "--json"},
		{"daemon", "run", "--listen", "tcp://127.0.0.1:0"},
		{"daemon", "status"},
		{"send", "75731b", "restart the worker run"},
		{"worker", "list", "--json"},
		{"diff", "--help"},
	}
	for _, args := range cases {
		if err := guardWorkerLifecycleArgs(args); err != nil {
			t.Errorf("guard rejected non-lifecycle invocation %v: %v", args, err)
		}
	}
}
