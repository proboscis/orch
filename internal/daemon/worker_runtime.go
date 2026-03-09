package daemon

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/s22625/orch/internal/xdg"
)

var currentHostname = os.Hostname

const managedWorkerRegistrationTimeout = 5 * time.Second
const managedWorkerRegistrationPoll = 50 * time.Millisecond

func HostWorkerID(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		host = "localhost"
	}

	var b strings.Builder
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	idHost := strings.Trim(b.String(), "-")
	if idHost == "" {
		idHost = "localhost"
	}
	return "host-" + idHost
}

func defaultWorkerID() string {
	host, _ := currentHostname()
	return HostWorkerID(host)
}

func (s *SocketServer) startManagedExternalWorker(workerID string) (string, int, error) {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		workerID = defaultWorkerID()
	}

	s.managedWorkersMu.RLock()
	if existing := s.managedWorkers[workerID]; existing != nil && existing.Process != nil {
		pid := existing.Process.Pid
		s.managedWorkersMu.RUnlock()
		return workerID, pid, nil
	}
	s.managedWorkersMu.RUnlock()

	var path string
	var args []string
	var env []string
	if s.workerLaunchConfig != nil {
		var err error
		path, args, env, err = s.workerLaunchConfig(workerID)
		if err != nil {
			return "", 0, err
		}
	} else {
		var err error
		path, args, env, err = defaultManagedWorkerLaunchConfig(workerID)
		if err != nil {
			return "", 0, err
		}
	}
	env = prepareManagedWorkerEnv(env)
	if len(args) == 0 {
		args = []string{path}
	}

	proc, err := os.StartProcess(path, args, &os.ProcAttr{Env: env, Files: []*os.File{os.Stdin, os.Stdout, os.Stderr}})
	if err != nil {
		return "", 0, err
	}

	s.managedWorkersMu.Lock()
	s.managedWorkers[workerID] = &managedWorkerProcess{WorkerID: workerID, Process: proc, PID: proc.Pid, StartedAt: time.Now()}
	s.managedWorkersMu.Unlock()

	go func(id string, proc *os.Process) {
		state, err := proc.Wait()
		s.managedWorkersMu.Lock()
		if entry := s.managedWorkers[id]; entry != nil && entry.Process == proc {
			entry.Process = nil
			entry.ExitedAt = time.Now()
			if err != nil {
				entry.ExitErr = err.Error()
			} else if state != nil && !state.Success() {
				entry.ExitErr = state.String()
			} else {
				entry.ExitErr = ""
			}
		}
		s.managedWorkersMu.Unlock()
		s.unregisterWorker(id)
	}(workerID, proc)

	if err := s.waitForManagedWorkerReady(workerID, proc.Pid, managedWorkerRegistrationTimeout); err != nil {
		_, _ = s.stopManagedExternalWorker(workerID, false)
		return "", 0, err
	}

	return workerID, proc.Pid, nil
}

func defaultManagedWorkerLaunchConfig(workerID string) (string, []string, []string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", nil, nil, err
	}
	return exe, []string{exe, "--remote=", "worker", "run", "--worker-id", workerID}, nil, nil
}

func prepareManagedWorkerEnv(extraEnv []string) []string {
	base := filterManagedWorkerEnv(os.Environ())
	if len(extraEnv) == 0 {
		return base
	}
	return append(base, filterManagedWorkerEnv(extraEnv)...)
}

func filterManagedWorkerEnv(env []string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "ORCH_REMOTE=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func (s *SocketServer) waitForManagedWorkerReady(workerID string, pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if worker := s.lookupActiveWorker(workerID); worker != nil {
			return nil
		}
		if exited, exitErr := s.managedWorkerExitState(workerID, pid); exited {
			return fmt.Errorf("managed worker %q exited before registering with orch-master%s; check 'orch log' or %s, or run 'orch --remote= worker run --worker-id %s' manually for diagnostics", workerID, formatManagedWorkerExitSuffix(exitErr), xdg.LogPath(), workerID)
		}
		time.Sleep(managedWorkerRegistrationPoll)
	}
	return fmt.Errorf("managed worker %q did not register with orch-master within %s; check 'orch log' or %s, or run 'orch --remote= worker run --worker-id %s' manually for diagnostics", workerID, timeout, xdg.LogPath(), workerID)
}

func (s *SocketServer) lookupActiveWorker(workerID string) *WorkerRegistration {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return nil
	}
	now := time.Now()
	s.workersMu.RLock()
	defer s.workersMu.RUnlock()
	worker := s.workers[workerID]
	if worker == nil || !s.workerIsActive(worker, now) {
		return nil
	}
	copy := *worker
	copy.Active = true
	return &copy
}

func (s *SocketServer) managedWorkerExitState(workerID string, pid int) (bool, string) {
	s.managedWorkersMu.RLock()
	defer s.managedWorkersMu.RUnlock()
	entry := s.managedWorkers[workerID]
	if entry == nil {
		return false, ""
	}
	if pid > 0 && entry.PID != 0 && entry.PID != pid {
		return false, ""
	}
	if entry.Process != nil {
		return false, ""
	}
	return !entry.ExitedAt.IsZero(), strings.TrimSpace(entry.ExitErr)
}

func formatManagedWorkerExitSuffix(exitErr string) string {
	exitErr = strings.TrimSpace(exitErr)
	if exitErr == "" {
		return ""
	}
	return fmt.Sprintf(" (%s)", exitErr)
}

func (s *SocketServer) stopManagedExternalWorker(workerID string, all bool) (int, error) {
	if all {
		s.managedWorkersMu.RLock()
		ids := make([]string, 0, len(s.managedWorkers))
		for id := range s.managedWorkers {
			ids = append(ids, id)
		}
		s.managedWorkersMu.RUnlock()
		count := 0
		for _, id := range ids {
			if stopped, _ := s.stopManagedExternalWorker(id, false); stopped > 0 {
				count += stopped
			}
		}
		return count, nil
	}

	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		workerID = defaultWorkerID()
	}

	s.managedWorkersMu.RLock()
	entry := s.managedWorkers[workerID]
	s.managedWorkersMu.RUnlock()
	if entry == nil {
		return 0, nil
	}
	proc := entry.Process
	if proc == nil {
		s.managedWorkersMu.Lock()
		delete(s.managedWorkers, workerID)
		s.managedWorkersMu.Unlock()
		return 0, nil
	}

	_ = proc.Signal(os.Interrupt)
	time.Sleep(200 * time.Millisecond)
	if err := proc.Signal(syscall.Signal(0)); err == nil {
		_ = proc.Kill()
	}

	s.managedWorkersMu.Lock()
	delete(s.managedWorkers, workerID)
	s.managedWorkersMu.Unlock()
	s.unregisterWorker(workerID)
	return 1, nil
}

func (s *SocketServer) listManagedExternalWorkers() []*managedWorkerProcess {
	s.managedWorkersMu.RLock()
	items := make([]*managedWorkerProcess, 0, len(s.managedWorkers))
	for _, w := range s.managedWorkers {
		if w == nil || w.Process == nil {
			continue
		}
		copy := *w
		items = append(items, &copy)
	}
	s.managedWorkersMu.RUnlock()
	sort.Slice(items, func(i, j int) bool { return items[i].WorkerID < items[j].WorkerID })
	return items
}
