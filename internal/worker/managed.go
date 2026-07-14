package worker

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/proboscis/orch/internal/daemon"
	"github.com/proboscis/orch/internal/xdg"
)

const (
	managedWorkerStateVersion     = 1
	managedWorkerStateEnv         = "ORCH_MANAGED_WORKER_STATE_PATH"
	managedWorkerPIDEnv           = "ORCH_MANAGED_WORKER_PID_PATH"
	managedWorkerRemoteAddrEnv    = "ORCH_MANAGED_WORKER_REMOTE_ADDR"
	managedWorkerLogPathEnv       = "ORCH_MANAGED_WORKER_LOG_PATH"
	localProcessStateMissing      = "missing"
	localProcessStateStarting     = "starting"
	localProcessStateRunning      = "running"
	localProcessStateReconnecting = "reconnecting"
	localProcessStateStopped      = "stopped"
	localProcessStateExited       = "exited"
	localProcessStateUnmanaged    = "unmanaged"
	masterStateActive             = "active"
	masterStateStale              = "stale"
	masterStateNotRegistered      = "not_registered"
	masterStateUnreachable        = "unreachable"
	defaultManagedWorkerQueryTime = time.Second
)

var (
	managedWorkerStartupTimeout     = 5 * time.Second
	managedWorkerStartupPoll        = 50 * time.Millisecond
	managedWorkerQueryTimeout       = defaultManagedWorkerQueryTime
	managedWorkerNow                = time.Now
	managedWorkerExecutable         = os.Executable
	managedWorkerLaunchConfig       = defaultManagedWorkerLaunchConfig
	lookupManagedWorkerRegistration = defaultManagedWorkerRegistrationLookup
	listManagedWorkerProcesses      = defaultListManagedWorkerProcesses
)

type ManagedOptions struct {
	WorkerID   string
	RemoteAddr string
}

type ManagedStartResult struct {
	OK       bool   `json:"ok"`
	WorkerID string `json:"worker_id"`
	PID      int    `json:"pid"`
	LogPath  string `json:"log_path,omitempty"`
	Reused   bool   `json:"reused"`
}

type ManagedState struct {
	Version           int       `json:"version"`
	Key               string    `json:"key"`
	WorkerID          string    `json:"worker_id"`
	RemoteAddr        string    `json:"remote_addr,omitempty"`
	LogPath           string    `json:"log_path,omitempty"`
	PID               int       `json:"pid,omitempty"`
	ProcessState      string    `json:"process_state,omitempty"`
	StartedAt         time.Time `json:"started_at,omitempty"`
	RegisteredAt      time.Time `json:"registered_at,omitempty"`
	LastHeartbeatAt   time.Time `json:"last_heartbeat_at,omitempty"`
	ReconnectingSince time.Time `json:"reconnecting_since,omitempty"`
	ExitedAt          time.Time `json:"exited_at,omitempty"`
	LastError         string    `json:"last_error,omitempty"`
	UpdatedAt         time.Time `json:"updated_at,omitempty"`
}

type ManagedLocalStatus struct {
	Managed           bool      `json:"managed"`
	ProcessExists     bool      `json:"process_exists"`
	State             string    `json:"state"`
	PID               int       `json:"pid,omitempty"`
	LogPath           string    `json:"log_path,omitempty"`
	StartedAt         time.Time `json:"started_at,omitempty"`
	RegisteredAt      time.Time `json:"registered_at,omitempty"`
	LastHeartbeatAt   time.Time `json:"last_heartbeat_at,omitempty"`
	ReconnectingSince time.Time `json:"reconnecting_since,omitempty"`
	ExitedAt          time.Time `json:"exited_at,omitempty"`
	LastError         string    `json:"last_error,omitempty"`
}

type ManagedMasterStatus struct {
	Reachable    bool                       `json:"reachable"`
	State        string                     `json:"state"`
	Error        string                     `json:"error,omitempty"`
	Registration *daemon.WorkerRegistration `json:"registration,omitempty"`
}

type ManagedStatus struct {
	OK         bool                `json:"ok"`
	WorkerID   string              `json:"worker_id"`
	RemoteAddr string              `json:"remote_addr,omitempty"`
	Profile    string              `json:"profile"`
	Local      ManagedLocalStatus  `json:"local"`
	Master     ManagedMasterStatus `json:"master"`
	Diagnostic string              `json:"diagnostic,omitempty"`
}

type managedProfile struct {
	Key        string
	WorkerID   string
	RemoteAddr string
	StatePath  string
	PIDPath    string
	LogPath    string
}

type managedRuntimeStateWriter struct {
	statePath  string
	pidPath    string
	logPath    string
	remoteAddr string
	workerID   string
	pid        int
}

type managedWorkerProcess struct {
	PID     int
	Command string
}

func StartManaged(opts ManagedOptions) (*ManagedStartResult, error) {
	profile, err := resolveManagedProfile(opts)
	if err != nil {
		return nil, err
	}
	if err := ensureManagedDirs(); err != nil {
		return nil, err
	}
	identityLock, err := acquireManagedWorkerIdentityLock(profile.WorkerID)
	if err != nil {
		return nil, err
	}
	defer releaseManagedWorkerIdentityLock(identityLock)

	state, _ := loadManagedState(profile.StatePath)
	keepPID := 0
	if state != nil && state.PID > 0 && daemon.IsProcessRunning(state.PID) {
		keepPID = state.PID
	}

	// A worker ID names one host-level supervisor, independent of which
	// spelling/profile was used to reach the master. Reconcile historical
	// profiles before inspecting the requested profile so a changed --remote
	// cannot leave the previous process competing for the same worker ID.
	if _, err := stopManagedProfilesForWorkerID(profile.WorkerID, profile.StatePath); err != nil {
		return nil, err
	}
	if _, err := stopUnmanagedWorkerProcesses(profile.WorkerID, keepPID); err != nil {
		return nil, err
	}

	state, _ = loadManagedState(profile.StatePath)
	if state != nil && state.LogPath != "" {
		profile.LogPath = state.LogPath
	}

	reg, regErr := lookupManagedWorkerRegistration(profile.RemoteAddr, profile.WorkerID)
	if state == nil && regErr == nil && reg != nil && reg.Active {
		return nil, fmt.Errorf("worker %q is already active on orch-master profile %q but no local managed process metadata exists; it may have been started manually on this host or another host", profile.WorkerID, managedProfileDisplay(profile.RemoteAddr))
	}

	if state != nil && state.PID > 0 && daemon.IsProcessRunning(state.PID) {
		if err := waitForManagedWorkerReady(profile, state.PID, nil); err != nil {
			return nil, err
		}
		return &ManagedStartResult{OK: true, WorkerID: profile.WorkerID, PID: state.PID, LogPath: profile.LogPath, Reused: true}, nil
	}

	if state != nil && state.PID > 0 {
		_ = removeManagedPID(profile.PIDPath)
	}

	logFile, err := os.OpenFile(profile.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open worker log %s: %w", profile.LogPath, err)
	}

	path, args, extraEnv, err := managedWorkerLaunchConfig(profile)
	if err != nil {
		_ = logFile.Close()
		return nil, err
	}
	cmd := &exec.Cmd{
		Path: path,
		Args: args,
		Env:  mergeManagedEnv(os.Environ(), extraEnv, profile),
		SysProcAttr: &syscall.SysProcAttr{
			Setsid: true,
		},
		Stdout: logFile,
		Stderr: logFile,
		Stdin:  nil,
	}

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("start managed worker %q: %w", profile.WorkerID, err)
	}
	_ = logFile.Close()

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	if err := writeManagedState(profile.StatePath, &ManagedState{
		Version:      managedWorkerStateVersion,
		Key:          profile.Key,
		WorkerID:     profile.WorkerID,
		RemoteAddr:   profile.RemoteAddr,
		LogPath:      profile.LogPath,
		PID:          cmd.Process.Pid,
		ProcessState: localProcessStateStarting,
		StartedAt:    managedWorkerNow(),
		UpdatedAt:    managedWorkerNow(),
	}); err != nil {
		_ = stopManagedProcess(cmd.Process.Pid)
		return nil, err
	}
	if err := writeManagedPID(profile.PIDPath, cmd.Process.Pid); err != nil {
		_ = stopManagedProcess(cmd.Process.Pid)
		return nil, err
	}

	if err := waitForManagedWorkerReady(profile, cmd.Process.Pid, waitCh); err != nil {
		_ = stopManagedProcess(cmd.Process.Pid)
		updateManagedState(profile.StatePath, func(state *ManagedState) {
			state.ProcessState = localProcessStateExited
			state.ExitedAt = managedWorkerNow()
			state.LastError = err.Error()
		})
		_ = removeManagedPID(profile.PIDPath)
		return nil, err
	}

	return &ManagedStartResult{OK: true, WorkerID: profile.WorkerID, PID: cmd.Process.Pid, LogPath: profile.LogPath}, nil
}

func StopManaged(opts ManagedOptions, all bool) (int, error) {
	if all {
		profiles, err := listManagedProfiles()
		if err != nil {
			return 0, err
		}
		stopped := 0
		for _, profile := range profiles {
			count, err := stopManagedProfile(profile)
			if err != nil {
				return stopped, err
			}
			stopped += count
		}
		orphaned, err := stopAllUnmanagedWorkerProcesses()
		stopped += orphaned
		if err != nil {
			return stopped, err
		}
		return stopped, nil
	}

	profile, err := resolveManagedProfile(opts)
	if err != nil {
		return 0, err
	}
	if err := ensureManagedDirs(); err != nil {
		return 0, err
	}
	identityLock, err := acquireManagedWorkerIdentityLock(profile.WorkerID)
	if err != nil {
		return 0, err
	}
	defer releaseManagedWorkerIdentityLock(identityLock)

	stopped, err := stopManagedProfilesForWorkerID(profile.WorkerID, "")
	if err != nil {
		return stopped, err
	}
	unmanagedStopped, err := stopUnmanagedWorkerProcesses(profile.WorkerID, 0)
	stopped += unmanagedStopped
	if err != nil {
		return stopped, err
	}

	// Preserve the unmanaged-registration diagnostic when no local profile
	// exists for this identity.
	profiles, err := listManagedProfilesForWorkerID(profile.WorkerID)
	if err != nil {
		return stopped, err
	}
	if len(profiles) == 0 && stopped == 0 {
		return stopManagedProfile(profile)
	}
	return stopped, nil
}

func StatusManaged(opts ManagedOptions) (*ManagedStatus, error) {
	profile, err := resolveManagedProfile(opts)
	if err != nil {
		return nil, err
	}

	state, _ := loadManagedState(profile.StatePath)
	processExists := false
	if state != nil && state.PID > 0 {
		processExists = daemon.IsProcessRunning(state.PID)
		if !processExists && state.ProcessState != localProcessStateStopped && state.ProcessState != localProcessStateExited {
			state.ProcessState = localProcessStateExited
		}
	}

	local := ManagedLocalStatus{Managed: state != nil, ProcessExists: processExists, State: localProcessStateMissing}
	if state != nil {
		local.State = strings.TrimSpace(state.ProcessState)
		if local.State == "" {
			local.State = localProcessStateMissing
		}
		local.PID = state.PID
		local.LogPath = state.LogPath
		local.StartedAt = state.StartedAt
		local.RegisteredAt = state.RegisteredAt
		local.LastHeartbeatAt = state.LastHeartbeatAt
		local.ReconnectingSince = state.ReconnectingSince
		local.ExitedAt = state.ExitedAt
		local.LastError = state.LastError
	}

	if !local.Managed {
		local.State = localProcessStateMissing
	} else if !local.ProcessExists && local.State == localProcessStateRunning {
		local.State = localProcessStateExited
	}

	reg, regErr := lookupManagedWorkerRegistration(profile.RemoteAddr, profile.WorkerID)
	master := ManagedMasterStatus{}
	switch {
	case regErr != nil:
		master.Reachable = false
		master.State = masterStateUnreachable
		master.Error = regErr.Error()
	case reg == nil:
		master.Reachable = true
		master.State = masterStateNotRegistered
	default:
		master.Reachable = true
		if reg.Active {
			master.State = masterStateActive
		} else {
			master.State = masterStateStale
		}
		master.Registration = reg
	}

	diagnostic := ""
	if !local.Managed && master.State != masterStateNotRegistered && master.Registration != nil {
		local.State = localProcessStateUnmanaged
		diagnostic = fmt.Sprintf("worker %q is registered on orch-master profile %q but is not managed by the local background supervisor", profile.WorkerID, managedProfileDisplay(profile.RemoteAddr))
	}
	if local.Managed && local.ProcessExists && master.State == masterStateNotRegistered {
		diagnostic = fmt.Sprintf("local worker process %q is running but has not registered with orch-master profile %q", profile.WorkerID, managedProfileDisplay(profile.RemoteAddr))
	}
	if local.Managed && local.ProcessExists && local.State == localProcessStateReconnecting {
		diagnostic = fmt.Sprintf("worker %q is reconnecting to orch-master profile %q since %s", profile.WorkerID, managedProfileDisplay(profile.RemoteAddr), local.ReconnectingSince.Format(time.RFC3339))
		if strings.TrimSpace(local.LastError) != "" {
			diagnostic += "; last error: " + strings.TrimSpace(local.LastError)
		}
	}
	if local.Managed && !local.ProcessExists && strings.TrimSpace(local.LastError) != "" {
		diagnostic = local.LastError
	}
	if master.State == masterStateUnreachable && diagnostic == "" {
		diagnostic = fmt.Sprintf("failed to reach orch-master profile %q", managedProfileDisplay(profile.RemoteAddr))
	}

	return &ManagedStatus{
		OK:         true,
		WorkerID:   profile.WorkerID,
		RemoteAddr: profile.RemoteAddr,
		Profile:    managedProfileDisplay(profile.RemoteAddr),
		Local:      local,
		Master:     master,
		Diagnostic: diagnostic,
	}, nil
}

func resolveManagedProfile(opts ManagedOptions) (managedProfile, error) {
	workerID := strings.TrimSpace(opts.WorkerID)
	if workerID == "" {
		host, _ := currentWorkerHostname()
		workerID = daemon.HostWorkerID(host)
	}
	remoteAddr := strings.TrimSpace(opts.RemoteAddr)
	key := managedProfileKey(workerID, remoteAddr)
	return managedProfile{
		Key:        key,
		WorkerID:   workerID,
		RemoteAddr: remoteAddr,
		StatePath:  filepath.Join(xdg.WorkersStateDir(), key+".json"),
		PIDPath:    filepath.Join(xdg.WorkersRuntimeDir(), key+".pid"),
		LogPath:    filepath.Join(xdg.WorkersStateDir(), key+".log"),
	}, nil
}

func managedProfileDisplay(remoteAddr string) string {
	if strings.TrimSpace(remoteAddr) == "" {
		return "local"
	}
	return strings.TrimSpace(remoteAddr)
}

func managedProfileKey(workerID, remoteAddr string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.TrimSpace(workerID)))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(strings.TrimSpace(remoteAddr)))
	base := sanitizeManagedProfileToken(workerID)
	if base == "" {
		base = "worker"
	}
	return fmt.Sprintf("%s-%016x", base, h.Sum64())
}

func sanitizeManagedProfileToken(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func defaultManagedWorkerLaunchConfig(profile managedProfile) (string, []string, []string, error) {
	exe, err := managedWorkerExecutable()
	if err != nil {
		return "", nil, nil, err
	}
	args := []string{exe, "--remote=", "worker", "run", "--worker-id", profile.WorkerID}
	if profile.RemoteAddr != "" {
		args[1] = "--remote=" + profile.RemoteAddr
	}
	return exe, args, nil, nil
}

func mergeManagedEnv(base, extra []string, profile managedProfile) []string {
	filtered := make([]string, 0, len(base)+len(extra)+4)
	omitPrefixes := []string{
		managedWorkerStateEnv + "=",
		managedWorkerPIDEnv + "=",
		managedWorkerRemoteAddrEnv + "=",
		managedWorkerLogPathEnv + "=",
	}
	for _, kv := range base {
		if hasAnyPrefix(kv, omitPrefixes) || workerEnvEntryHasAnyKey(kv, workerMultiplexerEnvKeys) {
			continue
		}
		filtered = append(filtered, kv)
	}
	filtered = append(filtered, scrubWorkerMultiplexerEnvEntries(extra)...)
	filtered = append(filtered,
		managedWorkerStateEnv+"="+profile.StatePath,
		managedWorkerPIDEnv+"="+profile.PIDPath,
		managedWorkerRemoteAddrEnv+"="+profile.RemoteAddr,
		managedWorkerLogPathEnv+"="+profile.LogPath,
	)
	return filtered
}

func hasAnyPrefix(value string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func ensureManagedDirs() error {
	if err := xdg.EnsureWorkersStateDir(); err != nil {
		return fmt.Errorf("ensure worker state dir: %w", err)
	}
	if err := xdg.EnsureWorkersRuntimeDir(); err != nil {
		return fmt.Errorf("ensure worker runtime dir: %w", err)
	}
	return nil
}

func loadManagedState(path string) (*ManagedState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state ManagedState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func writeManagedState(path string, state *ManagedState) error {
	if state == nil {
		return fmt.Errorf("managed worker state is nil")
	}
	state.Version = managedWorkerStateVersion
	state.UpdatedAt = managedWorkerNow()
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal managed worker state: %w", err)
	}
	return writeManagedAtomicFile(path, data, 0o644)
}

func updateManagedState(path string, update func(*ManagedState)) error {
	state, err := loadManagedState(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		state = &ManagedState{}
	}
	update(state)
	return writeManagedState(path, state)
}

func writeManagedAtomicFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmpFile, err := os.CreateTemp(dir, "*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmpFile.Chmod(perm); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func writeManagedPID(path string, pid int) error {
	return writeManagedAtomicFile(path, []byte(fmt.Sprintf("%d", pid)), 0o644)
}

func removeManagedPID(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func waitForManagedWorkerReady(profile managedProfile, pid int, waitCh <-chan error) error {
	deadline := managedWorkerNow().Add(managedWorkerStartupTimeout)
	var lastLookupErr error
	for managedWorkerNow().Before(deadline) {
		reg, err := lookupManagedWorkerRegistration(profile.RemoteAddr, profile.WorkerID)
		if err == nil && reg != nil && reg.Active {
			return nil
		}
		if err != nil {
			lastLookupErr = err
		}
		if waitCh != nil {
			select {
			case waitErr := <-waitCh:
				return managedWorkerExitedError(profile, waitErr)
			default:
			}
		}
		if pid > 0 && !daemon.IsProcessRunning(pid) {
			return managedWorkerExitedError(profile, nil)
		}
		time.Sleep(managedWorkerStartupPoll)
	}
	return managedWorkerTimeoutError(profile, lastLookupErr)
}

func managedWorkerExitedError(profile managedProfile, waitErr error) error {
	exitSuffix := ""
	if waitErr != nil {
		exitSuffix = fmt.Sprintf(" (%v)", waitErr)
	}
	if state, err := loadManagedState(profile.StatePath); err == nil && strings.TrimSpace(state.LastError) != "" {
		exitSuffix = fmt.Sprintf(" (%s)", strings.TrimSpace(state.LastError))
	}
	return fmt.Errorf("managed worker %q exited before registering with orch-master profile %q%s; check %s or run %s manually for diagnostics", profile.WorkerID, managedProfileDisplay(profile.RemoteAddr), exitSuffix, profile.LogPath, managedWorkerRunDiagnosticCommand(profile))
}

func managedWorkerTimeoutError(profile managedProfile, lastLookupErr error) error {
	if lastLookupErr != nil {
		return fmt.Errorf("managed worker %q did not register with orch-master profile %q within %s; last master error: %v; check %s or run %s manually for diagnostics", profile.WorkerID, managedProfileDisplay(profile.RemoteAddr), managedWorkerStartupTimeout, lastLookupErr, profile.LogPath, managedWorkerRunDiagnosticCommand(profile))
	}
	return fmt.Errorf("managed worker %q did not register with orch-master profile %q within %s; check %s or run %s manually for diagnostics", profile.WorkerID, managedProfileDisplay(profile.RemoteAddr), managedWorkerStartupTimeout, profile.LogPath, managedWorkerRunDiagnosticCommand(profile))
}

func managedWorkerRunDiagnosticCommand(profile managedProfile) string {
	flag := "--remote="
	if profile.RemoteAddr != "" {
		flag = "--remote=" + profile.RemoteAddr
	}
	return fmt.Sprintf("orch %s worker run --worker-id %s", flag, profile.WorkerID)
}

func defaultManagedWorkerRegistrationLookup(remoteAddr, workerID string) (*daemon.WorkerRegistration, error) {
	client := daemon.NewProtoClientWithAddress("", strings.TrimSpace(remoteAddr))
	client.SetTimeout(managedWorkerQueryTimeout)
	defer client.Close()

	resp, err := client.ListWorkers()
	if err != nil {
		return nil, err
	}
	for _, worker := range resp.Workers {
		if worker != nil && worker.ID == workerID {
			return worker, nil
		}
	}
	return nil, nil
}

func listManagedProfiles() ([]managedProfile, error) {
	if err := ensureManagedDirs(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(xdg.WorkersStateDir())
	if err != nil {
		return nil, err
	}
	profiles := make([]managedProfile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(xdg.WorkersStateDir(), entry.Name())
		state, err := loadManagedState(path)
		if err != nil {
			continue
		}
		profiles = append(profiles, managedProfile{
			Key:        strings.TrimSuffix(entry.Name(), ".json"),
			WorkerID:   state.WorkerID,
			RemoteAddr: state.RemoteAddr,
			StatePath:  path,
			PIDPath:    filepath.Join(xdg.WorkersRuntimeDir(), strings.TrimSuffix(entry.Name(), ".json")+".pid"),
			LogPath:    state.LogPath,
		})
	}
	return profiles, nil
}

func listManagedProfilesForWorkerID(workerID string) ([]managedProfile, error) {
	workerID = strings.TrimSpace(workerID)
	profiles, err := listManagedProfiles()
	if err != nil {
		return nil, err
	}

	matches := make([]managedProfile, 0, len(profiles))
	for _, profile := range profiles {
		if strings.TrimSpace(profile.WorkerID) == workerID {
			matches = append(matches, profile)
		}
	}
	return matches, nil
}

func stopManagedProfilesForWorkerID(workerID, exceptStatePath string) (int, error) {
	profiles, err := listManagedProfilesForWorkerID(workerID)
	if err != nil {
		return 0, err
	}

	stopped := 0
	for _, profile := range profiles {
		if exceptStatePath != "" && profile.StatePath == exceptStatePath {
			continue
		}
		count, err := stopManagedProfile(profile)
		if err != nil {
			return stopped, err
		}
		stopped += count
	}
	return stopped, nil
}

func stopUnmanagedWorkerProcesses(workerID string, keepPID int) (int, error) {
	processes, err := listManagedWorkerProcesses()
	if err != nil {
		return 0, fmt.Errorf("list local worker processes for %q: %w", workerID, err)
	}

	stopped := 0
	for _, process := range processes {
		if process.PID <= 0 || process.PID == os.Getpid() || process.PID == keepPID {
			continue
		}
		if !managedWorkerCommandHasIdentity(process.Command, workerID) || !daemon.IsProcessRunning(process.PID) {
			continue
		}
		if err := stopManagedProcess(process.PID); err != nil {
			return stopped, fmt.Errorf("stop orphaned worker %q pid %d: %w", workerID, process.PID, err)
		}
		stopped++
	}
	return stopped, nil
}

func stopAllUnmanagedWorkerProcesses() (int, error) {
	processes, err := listManagedWorkerProcesses()
	if err != nil {
		return 0, fmt.Errorf("list local worker processes: %w", err)
	}

	stopped := 0
	for _, process := range processes {
		if process.PID <= 0 || process.PID == os.Getpid() || !daemon.IsProcessRunning(process.PID) {
			continue
		}
		workerID, ok := managedWorkerCommandIdentity(process.Command)
		if !ok {
			continue
		}
		if err := stopManagedProcess(process.PID); err != nil {
			return stopped, fmt.Errorf("stop orphaned worker %q pid %d: %w", workerID, process.PID, err)
		}
		stopped++
	}
	return stopped, nil
}

func defaultListManagedWorkerProcesses() ([]managedWorkerProcess, error) {
	psPath := "/bin/ps"
	if _, err := os.Stat(psPath); err != nil {
		psPath = "/usr/bin/ps"
	}
	output, err := exec.Command(psPath, "-ww", "-axo", "pid=,args=").Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(output), "\n")
	processes := make([]managedWorkerProcess, 0, len(lines))
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			return nil, fmt.Errorf("parse ps pid %q: %w", fields[0], err)
		}
		processes = append(processes, managedWorkerProcess{
			PID:     pid,
			Command: strings.Join(fields[1:], " "),
		})
	}
	return processes, nil
}

func managedWorkerCommandHasIdentity(command, workerID string) bool {
	actualWorkerID, ok := managedWorkerCommandIdentity(command)
	return ok && strings.TrimSpace(actualWorkerID) == strings.TrimSpace(workerID)
}

func managedWorkerCommandIdentity(command string) (string, bool) {
	args := strings.Fields(strings.TrimSpace(command))
	if len(args) < 3 {
		return "", false
	}
	executable := strings.TrimSuffix(filepath.Base(args[0]), ".exe")
	if executable != "orch" && !strings.HasPrefix(executable, "orch-") {
		return "", false
	}

	workerRunIndex := -1
	for i := 1; i+1 < len(args); i++ {
		if args[i] == "worker" && args[i+1] == "run" {
			workerRunIndex = i
			break
		}
		if (args[i] == "--project" || args[i] == "--remote" || args[i] == "--log-level") && i+1 < len(args) {
			i++
			continue
		}
		if !strings.HasPrefix(args[i], "-") {
			// The first subcommand is not `worker run`; do not match text from
			// another command's positional arguments (for example `orch send`).
			return "", false
		}
	}
	if workerRunIndex < 0 {
		return "", false
	}

	actualWorkerID := ""
	for i := workerRunIndex + 2; i < len(args); i++ {
		switch {
		case args[i] == "--worker-id" && i+1 < len(args):
			actualWorkerID = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--worker-id="):
			actualWorkerID = strings.TrimPrefix(args[i], "--worker-id=")
		}
	}
	if actualWorkerID == "" {
		host, _ := currentWorkerHostname()
		if strings.TrimSpace(host) == "" {
			host = "localhost"
		}
		actualWorkerID = daemon.HostWorkerID(host)
	}
	return strings.TrimSpace(actualWorkerID), true
}

func acquireManagedWorkerIdentityLock(workerID string) (*os.File, error) {
	lockPath := filepath.Join(xdg.WorkersRuntimeDir(), managedProfileKey(workerID, "")+".lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open managed worker identity lock for %q: %w", workerID, err)
	}
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		_ = lockFile.Close()
		return nil, fmt.Errorf("lock managed worker identity %q: %w", workerID, err)
	}
	return lockFile, nil
}

func releaseManagedWorkerIdentityLock(lockFile *os.File) {
	if lockFile == nil {
		return
	}
	_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
	_ = lockFile.Close()
}

func stopManagedProfile(profile managedProfile) (int, error) {
	state, err := loadManagedState(profile.StatePath)
	if err != nil {
		if os.IsNotExist(err) {
			status, statusErr := StatusManaged(ManagedOptions{WorkerID: profile.WorkerID, RemoteAddr: profile.RemoteAddr})
			if statusErr == nil && status != nil && status.Master.Registration != nil {
				return 0, fmt.Errorf("worker %q is registered on orch-master profile %q but is not managed by the local background supervisor", profile.WorkerID, managedProfileDisplay(profile.RemoteAddr))
			}
			return 0, nil
		}
		return 0, err
	}
	if state.PID <= 0 || !daemon.IsProcessRunning(state.PID) {
		updateManagedState(profile.StatePath, func(current *ManagedState) {
			current.ProcessState = localProcessStateStopped
			current.ExitedAt = managedWorkerNow()
		})
		_ = removeManagedPID(profile.PIDPath)
		return 0, nil
	}
	if err := stopManagedProcess(state.PID); err != nil {
		return 0, err
	}
	updateManagedState(profile.StatePath, func(current *ManagedState) {
		current.ProcessState = localProcessStateStopped
		current.ExitedAt = managedWorkerNow()
		current.LastError = ""
	})
	_ = removeManagedPID(profile.PIDPath)
	return 1, nil
}

func stopManagedProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	_ = process.Signal(os.Interrupt)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !daemon.IsProcessRunning(pid) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := process.Kill(); err != nil && !strings.Contains(err.Error(), "process already finished") {
		return err
	}
	return nil
}

func managedRuntimeStateFromEnv() *managedRuntimeStateWriter {
	statePath := strings.TrimSpace(os.Getenv(managedWorkerStateEnv))
	if statePath == "" {
		return nil
	}
	return &managedRuntimeStateWriter{
		statePath:  statePath,
		pidPath:    strings.TrimSpace(os.Getenv(managedWorkerPIDEnv)),
		logPath:    strings.TrimSpace(os.Getenv(managedWorkerLogPathEnv)),
		remoteAddr: strings.TrimSpace(os.Getenv(managedWorkerRemoteAddrEnv)),
		pid:        os.Getpid(),
	}
}

func (w *managedRuntimeStateWriter) markStarting(workerID string) {
	if w == nil {
		return
	}
	w.workerID = workerID
	_ = updateManagedState(w.statePath, func(state *ManagedState) {
		state.Key = strings.TrimSuffix(filepath.Base(w.statePath), filepath.Ext(w.statePath))
		state.WorkerID = workerID
		state.RemoteAddr = w.remoteAddr
		state.LogPath = w.logPath
		state.PID = w.pid
		state.ProcessState = localProcessStateStarting
		if state.StartedAt.IsZero() {
			state.StartedAt = managedWorkerNow()
		}
		state.ReconnectingSince = time.Time{}
		state.ExitedAt = time.Time{}
		state.LastError = ""
	})
	if w.pidPath != "" {
		_ = writeManagedPID(w.pidPath, w.pid)
	}
}

func (w *managedRuntimeStateWriter) markRegistered() {
	if w == nil {
		return
	}
	now := managedWorkerNow()
	_ = updateManagedState(w.statePath, func(state *ManagedState) {
		state.WorkerID = w.workerID
		state.RemoteAddr = w.remoteAddr
		state.LogPath = w.logPath
		state.PID = w.pid
		state.ProcessState = localProcessStateRunning
		if state.StartedAt.IsZero() {
			state.StartedAt = now
		}
		if state.RegisteredAt.IsZero() {
			state.RegisteredAt = now
		}
		state.LastHeartbeatAt = now
		state.ReconnectingSince = time.Time{}
		state.ExitedAt = time.Time{}
		state.LastError = ""
	})
}

// markReconnecting records that the worker lost its master connection and is
// retrying with backoff, so `orch worker status` shows "reconnecting since
// ... last error ..." instead of a dead-looking exited state.
func (w *managedRuntimeStateWriter) markReconnecting(err error) {
	if w == nil {
		return
	}
	now := managedWorkerNow()
	_ = updateManagedState(w.statePath, func(state *ManagedState) {
		state.WorkerID = w.workerID
		state.RemoteAddr = w.remoteAddr
		state.LogPath = w.logPath
		state.PID = w.pid
		if state.ProcessState != localProcessStateReconnecting || state.ReconnectingSince.IsZero() {
			state.ReconnectingSince = now
		}
		state.ProcessState = localProcessStateReconnecting
		state.ExitedAt = time.Time{}
		if err != nil {
			state.LastError = err.Error()
		}
	})
}

func (w *managedRuntimeStateWriter) markHeartbeat() {
	if w == nil {
		return
	}
	now := managedWorkerNow()
	_ = updateManagedState(w.statePath, func(state *ManagedState) {
		state.LastHeartbeatAt = now
		if state.ProcessState == "" || state.ProcessState == localProcessStateStarting {
			state.ProcessState = localProcessStateRunning
		}
	})
}

func (w *managedRuntimeStateWriter) markExited(err error) {
	if w == nil {
		return
	}
	now := managedWorkerNow()
	_ = updateManagedState(w.statePath, func(state *ManagedState) {
		state.ExitedAt = now
		state.ReconnectingSince = time.Time{}
		state.ProcessState = localProcessStateStopped
		if err != nil {
			state.ProcessState = localProcessStateExited
			state.LastError = err.Error()
		} else {
			state.LastError = ""
		}
	})
	if w.pidPath != "" {
		_ = removeManagedPID(w.pidPath)
	}
}
