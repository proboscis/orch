package worker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/proboscis/orch/internal/daemon"
)

// Worker identity is the worker id, not the managed state file. A process
// started with a different connection string, an older binary generation, or
// no supervisor at all still claims its worker id against the master, so
// `worker stop` and `worker start` reconcile the declarative invariant
// "at most one local process claims a given worker id" against the live
// process table instead of trusting per-profile state records.

// workerProcess is one live process claiming a worker id. WorkerID is the raw
// claim from the command line; empty means the process claims the host
// default worker id (daemon.HostWorkerID of the local hostname).
type workerProcess struct {
	PID      int
	WorkerID string
}

// listWorkerProcesses is a test seam over the process-table scan.
var listWorkerProcesses = defaultListWorkerProcesses

func defaultListWorkerProcesses() ([]workerProcess, error) {
	out, err := exec.Command("ps", "-axww", "-o", "pid=,args=").Output()
	if err != nil {
		return nil, fmt.Errorf("list host processes via ps: %w", err)
	}
	return parseWorkerProcessTable(string(out)), nil
}

func parseWorkerProcessTable(table string) []workerProcess {
	var procs []workerProcess
	for _, line := range strings.Split(table, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 0 {
			continue
		}
		claim, ok := parseWorkerRunClaim(fields[1:])
		if !ok {
			continue
		}
		procs = append(procs, workerProcess{PID: pid, WorkerID: claim})
	}
	return procs
}

// parseWorkerRunClaim reports whether argv is an `orch ... worker run ...`
// invocation and, if so, which worker id it claims. An empty claim with
// ok=true means the process claims the host default worker id.
//
// The worker/run pair must be the first two non-flag tokens after the binary:
// requiring position rather than mere presence keeps free-text arguments of
// other commands (e.g. `orch send 75731b "restart the worker run"`) from
// matching. Space-separated flag values before the subcommand (`--remote
// addr worker run`) defeat the position check and make the scan miss that
// process — a miss, never an overkill; the managed launcher and documented
// invocations use the `--remote=` form.
func parseWorkerRunClaim(argv []string) (string, bool) {
	if len(argv) == 0 {
		return "", false
	}
	if !strings.HasPrefix(filepath.Base(argv[0]), "orch") {
		return "", false
	}
	rest := argv[1:]
	runIdx := -1
	seen := 0
	for i, tok := range rest {
		if strings.HasPrefix(tok, "-") {
			continue
		}
		seen++
		if seen == 1 {
			if tok != "worker" {
				return "", false
			}
			continue
		}
		if tok != "run" {
			return "", false
		}
		runIdx = i
		break
	}
	if runIdx < 0 {
		return "", false
	}
	for i := runIdx + 1; i < len(rest); i++ {
		if v, ok := strings.CutPrefix(rest[i], "--worker-id="); ok {
			return strings.TrimSpace(v), true
		}
		if rest[i] == "--worker-id" && i+1 < len(rest) {
			return strings.TrimSpace(rest[i+1]), true
		}
	}
	return "", true
}

func defaultLocalWorkerID() string {
	host, _ := currentWorkerHostname()
	return daemon.HostWorkerID(host)
}

// reconcileWorkerID stops every local process claiming workerID except
// keepPID (and this process), returning the PIDs it stopped. keepPID 0 keeps
// nothing: the invariant target is zero claimants (worker stop). A non-zero
// keepPID preserves exactly the managed process (worker start).
func reconcileWorkerID(workerID string, keepPID int) ([]int, error) {
	target := strings.TrimSpace(workerID)
	if target == "" {
		target = defaultLocalWorkerID()
	}
	procs, err := listWorkerProcesses()
	if err != nil {
		return nil, fmt.Errorf("reconcile worker id %q: %w", target, err)
	}
	self := os.Getpid()
	var stopped []int
	for _, proc := range procs {
		claim := strings.TrimSpace(proc.WorkerID)
		if claim == "" {
			claim = defaultLocalWorkerID()
		}
		if claim != target || proc.PID <= 0 || proc.PID == keepPID || proc.PID == self {
			continue
		}
		// Signal-0 recheck: the scan snapshot may be stale (a process this
		// call already stopped, or one that exited on its own), and a PID we
		// cannot signal is not ours to stop.
		if !daemon.IsProcessRunning(proc.PID) {
			continue
		}
		if err := stopManagedProcess(proc.PID); err != nil {
			return stopped, fmt.Errorf("stop orphan worker process %d claiming worker id %q: %w", proc.PID, target, err)
		}
		stopped = append(stopped, proc.PID)
	}
	sort.Ints(stopped)
	return stopped, nil
}

// reconcileAllWorkerProcesses stops every local `orch worker run` process
// regardless of claimed worker id (worker stop --all: the host runs zero
// workers afterwards, including ones no state file records).
func reconcileAllWorkerProcesses() ([]int, error) {
	procs, err := listWorkerProcesses()
	if err != nil {
		return nil, fmt.Errorf("reconcile all worker processes: %w", err)
	}
	self := os.Getpid()
	var stopped []int
	for _, proc := range procs {
		if proc.PID <= 0 || proc.PID == self {
			continue
		}
		if !daemon.IsProcessRunning(proc.PID) {
			continue
		}
		if err := stopManagedProcess(proc.PID); err != nil {
			return stopped, fmt.Errorf("stop worker process %d: %w", proc.PID, err)
		}
		stopped = append(stopped, proc.PID)
	}
	sort.Ints(stopped)
	return stopped, nil
}
