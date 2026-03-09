package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/model"
)

const (
	workerHeartbeatTTL = 30 * time.Second
	workerLeaseTTL     = 60 * time.Second
)

func (s *SocketServer) requireWorkerAuth(token string) error {
	required := strings.TrimSpace(s.workerAuthToken)
	if required == "" {
		return nil
	}
	if strings.TrimSpace(token) != required {
		return fmt.Errorf("unauthorized worker")
	}
	return nil
}

type WorkerRegistration struct {
	ID            string    `json:"id"`
	WorkerType    string    `json:"worker_type"`
	Host          string    `json:"host"`
	Mode          string    `json:"mode"`
	Capabilities  []string  `json:"capabilities,omitempty"`
	RegisteredAt  time.Time `json:"registered_at"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	Active        bool      `json:"active"`
}

type WorkerLease struct {
	LeaseID       string               `json:"lease_id"`
	WorkerID      string               `json:"worker_id"`
	ProjectID     string               `json:"project_id,omitempty"`
	Effect        string               `json:"effect"`
	IssueID       string               `json:"issue_id,omitempty"`
	RunID         string               `json:"run_id,omitempty"`
	LeasedAt      time.Time            `json:"leased_at"`
	DispatchedAt  time.Time            `json:"dispatched_at,omitempty"`
	DispatchCount int                  `json:"dispatch_count"`
	ExpiresAt     time.Time            `json:"expires_at"`
	CompletedAt   time.Time            `json:"completed_at,omitempty"`
	Completed     bool                 `json:"completed"`
	Success       bool                 `json:"success"`
	Error         string               `json:"error,omitempty"`
	Payload       *WorkerEffectPayload `json:"-"`
	PayloadJSON   string               `json:"payload_json,omitempty"`
	ResultJSON    string               `json:"result_json,omitempty"`
}

type WorkerEffectPayload struct {
	StartRun          *StartRunOptions
	ContinueRun       *ContinueRunOptions
	StopRun           *StopRunPayload
	StartRunResult    *StartRunResult
	ContinueRunResult *ContinueRunResult
}

type StopRunPayload struct {
	ProjectRoot    string `json:"project_root,omitempty"`
	Target         string `json:"target,omitempty"`
	TargetHost     string `json:"target_host,omitempty"`
	TargetWorkerID string `json:"target_worker_id,omitempty"`
}

type WorkerEffectResult struct {
	StartRunResult    *StartRunResult    `json:"start_run_result,omitempty"`
	ContinueRunResult *ContinueRunResult `json:"continue_run_result,omitempty"`
}

func normalizeCapabilities(caps []string) []string {
	if len(caps) == 0 {
		return []string{"start_run", "continue_run", "stop_run"}
	}
	set := make(map[string]struct{}, len(caps))
	norm := make([]string, 0, len(caps))
	for _, c := range caps {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, ok := set[c]; ok {
			continue
		}
		set[c] = struct{}{}
		norm = append(norm, c)
	}
	if len(norm) == 0 {
		return []string{"start_run", "continue_run", "stop_run"}
	}
	sort.Strings(norm)
	return norm
}

func (s *SocketServer) registerWorker(workerID, workerType, host, mode string, capabilities []string) (*WorkerRegistration, int64) {
	now := time.Now()
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		workerID = "wrk-" + generateMonitorID()[:12]
	}
	workerType = strings.TrimSpace(workerType)
	if workerType == "" {
		workerType = "general"
	}
	host = strings.TrimSpace(host)
	if host == "" {
		host = "unknown"
	}
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = "external"
	}

	s.workersMu.Lock()
	defer s.workersMu.Unlock()

	registeredAt := now
	if existing, ok := s.workers[workerID]; ok {
		registeredAt = existing.RegisteredAt
	}

	worker := &WorkerRegistration{
		ID:            workerID,
		WorkerType:    workerType,
		Host:          host,
		Mode:          mode,
		Capabilities:  normalizeCapabilities(capabilities),
		RegisteredAt:  registeredAt,
		LastHeartbeat: now,
	}
	worker.Active = s.workerIsActive(worker, now)
	s.workers[workerID] = worker

	copy := *worker
	return &copy, int64(workerHeartbeatTTL.Seconds())
}

func (s *SocketServer) unregisterWorker(workerID string) bool {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return false
	}

	s.workersMu.Lock()
	defer s.workersMu.Unlock()
	if _, ok := s.workers[workerID]; !ok {
		return false
	}
	delete(s.workers, workerID)
	return true
}

func (s *SocketServer) heartbeatWorker(workerID string) (int64, error) {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return 0, fmt.Errorf("worker_id required")
	}

	now := time.Now()
	s.workersMu.Lock()
	defer s.workersMu.Unlock()
	worker, ok := s.workers[workerID]
	if !ok {
		return 0, fmt.Errorf("worker not found: %s", workerID)
	}
	worker.LastHeartbeat = now
	worker.Active = true
	return int64(workerHeartbeatTTL.Seconds()), nil
}

func (s *SocketServer) listWorkers() []*WorkerRegistration {
	now := time.Now()

	s.workersMu.RLock()
	workers := make([]*WorkerRegistration, 0, len(s.workers))
	for _, worker := range s.workers {
		copy := *worker
		copy.Active = s.workerIsActive(worker, now)
		workers = append(workers, &copy)
	}
	s.workersMu.RUnlock()

	sort.Slice(workers, func(i, j int) bool {
		return workers[i].ID < workers[j].ID
	})

	return workers
}

func (s *SocketServer) listWorkerLeases(includeCompleted bool) []*WorkerLease {
	s.workerLeasesMu.RLock()
	leases := make([]*WorkerLease, 0, len(s.workerLeases))
	for _, lease := range s.workerLeases {
		if !includeCompleted && lease.Completed {
			continue
		}
		copy := *lease
		leases = append(leases, &copy)
	}
	s.workerLeasesMu.RUnlock()

	sort.Slice(leases, func(i, j int) bool {
		if leases[i].LeasedAt.Equal(leases[j].LeasedAt) {
			return leases[i].LeaseID < leases[j].LeaseID
		}
		return leases[i].LeasedAt.After(leases[j].LeasedAt)
	})

	return leases
}

func (s *SocketServer) workerIsActive(worker *WorkerRegistration, now time.Time) bool {
	if worker == nil || worker.LastHeartbeat.IsZero() {
		return false
	}
	return now.Sub(worker.LastHeartbeat) <= workerHeartbeatTTL
}

func (s *SocketServer) selectActiveWorker() (*WorkerRegistration, error) {
	return s.selectActiveWorkerForEffect("", "", false)
}

func (s *SocketServer) selectActiveWorkerForEffect(effect, requiredWorkerID string, strict bool) (*WorkerRegistration, error) {
	now := time.Now()
	effect = strings.TrimSpace(effect)
	requiredWorkerID = strings.TrimSpace(requiredWorkerID)

	s.workersMu.RLock()
	defer s.workersMu.RUnlock()

	var selected *WorkerRegistration
	for _, worker := range s.workers {
		if !s.workerIsActive(worker, now) {
			continue
		}
		if effect != "" {
			supported := false
			for _, cap := range worker.Capabilities {
				if cap == effect {
					supported = true
					break
				}
			}
			if !supported {
				continue
			}
		}
		if requiredWorkerID != "" && worker.ID != requiredWorkerID {
			continue
		}
		if selected == nil || worker.LastHeartbeat.After(selected.LastHeartbeat) {
			selected = worker
		}
	}

	if selected == nil {
		if requiredWorkerID != "" && strict {
			return nil, fmt.Errorf("no active worker available for target %q; start orch-worker with --worker-id %s on the target host", requiredWorkerID, requiredWorkerID)
		}
		if requiredWorkerID != "" && !strict {
			for _, worker := range s.workers {
				if !s.workerIsActive(worker, now) {
					continue
				}
				if effect != "" {
					supported := false
					for _, cap := range worker.Capabilities {
						if cap == effect {
							supported = true
							break
						}
					}
					if !supported {
						continue
					}
				}
				if selected == nil || worker.LastHeartbeat.After(selected.LastHeartbeat) {
					selected = worker
				}
			}
			if selected != nil {
				copy := *selected
				copy.Active = true
				return &copy, nil
			}
		}
		return nil, fmt.Errorf("no active workers available; start an external worker via 'orch worker start'")
	}

	copy := *selected
	copy.Active = true
	return &copy, nil
}

func (s *SocketServer) acquireWorkerLease(projectID, effect, issueID, runID string, payload *WorkerEffectPayload) (*WorkerLease, error) {
	preferredWorkerID, strict := preferredWorkerPreferenceForPayload(payload)
	worker, err := s.selectActiveWorkerForEffect(effect, preferredWorkerID, strict)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	lease := &WorkerLease{
		LeaseID:   "lease-" + generateMonitorID()[:12],
		WorkerID:  worker.ID,
		ProjectID: strings.TrimSpace(projectID),
		Effect:    strings.TrimSpace(effect),
		IssueID:   strings.TrimSpace(issueID),
		RunID:     strings.TrimSpace(runID),
		LeasedAt:  now,
		Payload:   payload,
	}

	s.workerLeasesMu.Lock()
	s.workerLeases[lease.LeaseID] = lease
	s.workerLeasesMu.Unlock()

	copy := *lease
	return &copy, nil
}

func preferredWorkerPreferenceForPayload(payload *WorkerEffectPayload) (string, bool) {
	if payload == nil {
		return defaultWorkerID(), false
	}
	if payload.StartRun != nil {
		if workerID := strings.TrimSpace(payload.StartRun.TargetWorkerID); workerID != "" {
			return workerID, true
		}
		if target := strings.TrimSpace(payload.StartRun.Target); target != "" && target != "local" {
			return target, true
		}
		return defaultWorkerID(), false
	}
	if payload.ContinueRun != nil {
		if workerID := strings.TrimSpace(payload.ContinueRun.TargetWorkerID); workerID != "" {
			return workerID, true
		}
		if target := strings.TrimSpace(payload.ContinueRun.Target); target != "" && target != "local" {
			return target, true
		}
		return defaultWorkerID(), false
	}
	if payload.StopRun != nil {
		if workerID := strings.TrimSpace(payload.StopRun.TargetWorkerID); workerID != "" {
			return workerID, true
		}
		if target := strings.TrimSpace(payload.StopRun.Target); target != "" && target != "local" {
			return target, true
		}
		return defaultWorkerID(), false
	}
	return defaultWorkerID(), false
}

func (s *SocketServer) leaseWorkForWorker(workerID string) *WorkerLease {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return nil
	}

	now := time.Now()

	s.workerLeasesMu.Lock()
	defer s.workerLeasesMu.Unlock()

	var selected *WorkerLease
	for _, lease := range s.workerLeases {
		if lease.Completed || lease.WorkerID != workerID {
			continue
		}
		if lease.DispatchCount > 0 && now.Before(lease.ExpiresAt) {
			continue
		}
		if selected == nil || lease.LeasedAt.Before(selected.LeasedAt) {
			selected = lease
		}
	}

	if selected == nil {
		return nil
	}

	selected.DispatchCount++
	selected.DispatchedAt = now
	selected.ExpiresAt = now.Add(workerLeaseTTL)
	selected.PayloadJSON = marshalWorkerEffectPayload(selected.Payload)

	copy := *selected
	return &copy
}

func marshalWorkerEffectPayload(payload *WorkerEffectPayload) string {
	if payload == nil {
		return ""
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(data)
}

func marshalWorkerEffectResult(result *WorkerEffectResult) string {
	if result == nil {
		return ""
	}

	if result.StartRunResult == nil && result.ContinueRunResult == nil {
		return ""
	}

	data, err := json.Marshal(result)
	if err != nil {
		return ""
	}
	return string(data)
}

func decodeWorkerEffectResult(resultJSON string) (*WorkerEffectResult, error) {
	if strings.TrimSpace(resultJSON) == "" {
		return &WorkerEffectResult{}, nil
	}

	var result WorkerEffectResult
	if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
		return nil, fmt.Errorf("invalid worker result_json: %w", err)
	}

	return &result, nil
}

func projectRootFromWorkerPayload(payload *WorkerEffectPayload) string {
	if payload == nil {
		return ""
	}
	if payload.StartRun != nil {
		return strings.TrimSpace(payload.StartRun.ProjectRoot)
	}
	if payload.ContinueRun != nil {
		return strings.TrimSpace(payload.ContinueRun.ProjectRoot)
	}
	if payload.StopRun != nil {
		return strings.TrimSpace(payload.StopRun.ProjectRoot)
	}
	return ""
}

func (s *SocketServer) ensureRepoContextForProject(projectID, projectRoot string) *RepoContext {
	projectRoot = strings.TrimSpace(projectRoot)
	if projectRoot == "" {
		return nil
	}

	cfg, err := config.LoadFromProjectRoot(projectRoot)
	if err != nil || cfg == nil {
		return nil
	}
	issuesRoot := strings.TrimSpace(cfg.GetIssuesPath())
	if issuesRoot == "" {
		return nil
	}

	st := s.getOrCreateStore(issuesRoot, projectRoot)
	if st == nil {
		return nil
	}

	repoID := strings.TrimSpace(projectID)
	if repoID == "" {
		var err error
		repoID, err = s.repoIDForProjectRoot(projectRoot)
		if err != nil {
			return nil
		}
	}

	s.reposMu.Lock()
	existing := s.repos[repoID]
	if existing == nil {
		existing = &RepoContext{ProjectRoot: projectRoot, RepoID: repoID, Store: st}
		s.repos[repoID] = existing
	} else {
		if strings.TrimSpace(existing.ProjectRoot) == "" {
			existing.ProjectRoot = projectRoot
		}
		if existing.Store == nil {
			existing.Store = st
		}
	}
	s.reposMu.Unlock()

	return existing
}

func (s *SocketServer) executeLeaseEffect(lease *WorkerLease) (*WorkerEffectResult, error) {
	if lease == nil {
		return nil, fmt.Errorf("lease required")
	}

	repoCtx := s.ensureRepoContextByID(strings.TrimSpace(lease.ProjectID))
	if repoCtx == nil || repoCtx.Store == nil {
		repoCtx = s.ensureRepoContextForProject(strings.TrimSpace(lease.ProjectID), projectRootFromWorkerPayload(lease.Payload))
	}
	if repoCtx == nil || repoCtx.Store == nil {
		return nil, fmt.Errorf("no store available for project_id %q (register daemon project mapping)", strings.TrimSpace(lease.ProjectID))
	}

	switch lease.Effect {
	case "start_run":
		if lease.Payload == nil || lease.Payload.StartRun == nil {
			return nil, fmt.Errorf("start_run payload missing")
		}
		optsCopy := *lease.Payload.StartRun
		if s.currentWorkerID != "" && strings.TrimSpace(optsCopy.TargetWorkerID) == s.currentWorkerID {
			if filepath.IsAbs(strings.TrimSpace(optsCopy.WorktreeDir)) {
				optsCopy.WorktreeDir = ""
			}
		}
		if strings.TrimSpace(optsCopy.ProjectRoot) == "" {
			optsCopy.ProjectRoot = repoCtx.ProjectRoot
		}
		result, err := s.processStartRunCore(repoCtx.Store, optsCopy.ProjectRoot, &optsCopy)
		if err != nil {
			return nil, err
		}
		return &WorkerEffectResult{StartRunResult: result}, nil
	case "continue_run":
		if lease.Payload == nil || lease.Payload.ContinueRun == nil {
			return nil, fmt.Errorf("continue_run payload missing")
		}
		optsCopy := *lease.Payload.ContinueRun
		if s.currentWorkerID != "" && strings.TrimSpace(optsCopy.TargetWorkerID) == s.currentWorkerID {
			if filepath.IsAbs(strings.TrimSpace(optsCopy.WorktreeDir)) {
				optsCopy.WorktreeDir = ""
			}
		}
		projectRoot := strings.TrimSpace(optsCopy.ProjectRoot)
		if projectRoot == "" {
			projectRoot = repoCtx.ProjectRoot
			optsCopy.ProjectRoot = projectRoot
		}
		result, err := s.processContinueRunCore(repoCtx.Store, projectRoot, &optsCopy)
		if err != nil {
			return nil, err
		}
		return &WorkerEffectResult{ContinueRunResult: result}, nil
	case "stop_run":
		run, err := repoCtx.Store.GetRun(&model.RunRef{IssueID: lease.IssueID, RunID: lease.RunID})
		if err != nil {
			return nil, fmt.Errorf("not_found")
		}
		if err := s.stopSingleRun(run, repoCtx.Store); err != nil {
			return nil, err
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported worker lease effect: %s", lease.Effect)
	}
}

func (s *SocketServer) waitForWorkerLeaseCompletion(leaseID string, timeout time.Duration) (*WorkerLease, error) {
	leaseID = strings.TrimSpace(leaseID)
	if leaseID == "" {
		return nil, fmt.Errorf("lease_id required")
	}

	deadline := time.Now().Add(timeout)
	for {
		s.workerLeasesMu.RLock()
		lease := s.workerLeases[leaseID]
		s.workerLeasesMu.RUnlock()

		if lease == nil {
			return nil, fmt.Errorf("lease not found: %s", leaseID)
		}
		if lease.Completed {
			copy := *lease
			if lease.Success {
				return &copy, nil
			}
			if strings.TrimSpace(lease.Error) != "" {
				return &copy, errors.New(lease.Error)
			}
			return &copy, fmt.Errorf("worker lease failed: %s", leaseID)
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("worker lease timed out: %s", leaseID)
		}

		time.Sleep(100 * time.Millisecond)
	}
}

func (s *SocketServer) acknowledgeWorkerLease(workerID, leaseID string, success bool, errMsg, resultJSON string) error {
	leaseID = strings.TrimSpace(leaseID)
	if leaseID == "" {
		return fmt.Errorf("lease_id required")
	}

	s.workerLeasesMu.Lock()
	defer s.workerLeasesMu.Unlock()

	lease, ok := s.workerLeases[leaseID]
	if !ok {
		return fmt.Errorf("lease not found: %s", leaseID)
	}

	if workerID = strings.TrimSpace(workerID); workerID != "" && lease.WorkerID != workerID {
		return fmt.Errorf("lease worker mismatch")
	}

	lease.Completed = true
	lease.CompletedAt = time.Now()
	lease.Success = success
	lease.Error = strings.TrimSpace(errMsg)
	lease.ResultJSON = strings.TrimSpace(resultJSON)
	return nil
}

func (s *SocketServer) withWorkerLease(projectID, effect, issueID, runID string, payload *WorkerEffectPayload) (*WorkerLease, error) {
	lease, err := s.acquireWorkerLease(projectID, effect, issueID, runID, payload)
	if err != nil {
		return nil, err
	}

	completedLease, err := s.waitForWorkerLeaseCompletion(lease.LeaseID, 10*time.Minute)
	if err != nil {
		return nil, err
	}

	return completedLease, nil
}

func (s *SocketServer) ExecuteWorkerLease(lease *WorkerLease) (*WorkerEffectResult, error) {
	return s.executeLeaseEffect(lease)
}

func EncodeWorkerEffectResult(result *WorkerEffectResult) string {
	return marshalWorkerEffectResult(result)
}
