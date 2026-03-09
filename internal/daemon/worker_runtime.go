package daemon

import (
	"os"
	"sort"
	"strings"
	"syscall"
	"time"
)

var currentHostname = os.Hostname

func defaultWorkerID() string {
	host, _ := currentHostname()
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
	if len(env) == 0 {
		env = os.Environ()
	} else {
		env = append(os.Environ(), env...)
	}
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
		_, _ = proc.Wait()
		s.managedWorkersMu.Lock()
		delete(s.managedWorkers, id)
		s.managedWorkersMu.Unlock()
		s.unregisterWorker(id)
	}(workerID, proc)

	return workerID, proc.Pid, nil
}

func defaultManagedWorkerLaunchConfig(workerID string) (string, []string, []string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", nil, nil, err
	}
	return exe, []string{exe, "worker", "run", "--worker-id", workerID}, nil, nil
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
	if entry == nil || entry.Process == nil {
		return 0, nil
	}

	_ = entry.Process.Signal(os.Interrupt)
	time.Sleep(200 * time.Millisecond)
	if err := entry.Process.Signal(syscall.Signal(0)); err == nil {
		_ = entry.Process.Kill()
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
		copy := *w
		items = append(items, &copy)
	}
	s.managedWorkersMu.RUnlock()
	sort.Slice(items, func(i, j int) bool { return items[i].WorkerID < items[j].WorkerID })
	return items
}
