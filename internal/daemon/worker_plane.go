package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/s22625/orch/internal/git"
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
	// Compatibility name for a host work assignment record.
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
	CaptureSession    *CaptureSessionPayload
	SendMessage       *SendMessagePayload
	GetDiffStats      *GetDiffStatsPayload
	GetBranchState    *GetBranchStatePayload
	GetDiff           *GetDiffPayload
	StartRunResult    *StartRunResult
	ContinueRunResult *ContinueRunResult
	CaptureResult     *CaptureSessionResult
	DiffStatsResult   *GetDiffStatsResult
	BranchStateResult *GetBranchStateResult
	DiffResult        *GetDiffResult
}

type StopRunPayload struct {
	ProjectRoot    string `json:"project_root,omitempty"`
	Target         string `json:"target,omitempty"`
	TargetHost     string `json:"target_host,omitempty"`
	TargetWorkerID string `json:"target_worker_id,omitempty"`
	RunSnapshot    *RunSnapshot
}

type CaptureSessionPayload struct {
	Lines          int    `json:"lines,omitempty"`
	Target         string `json:"target,omitempty"`
	TargetHost     string `json:"target_host,omitempty"`
	TargetWorkerID string `json:"target_worker_id,omitempty"`
	RunSnapshot    *RunSnapshot
}

type SendMessagePayload struct {
	Message        string `json:"message,omitempty"`
	NoEnter        bool   `json:"no_enter,omitempty"`
	Target         string `json:"target,omitempty"`
	TargetHost     string `json:"target_host,omitempty"`
	TargetWorkerID string `json:"target_worker_id,omitempty"`
	RunSnapshot    *RunSnapshot
}

type GetDiffStatsPayload struct {
	Target         string `json:"target,omitempty"`
	TargetHost     string `json:"target_host,omitempty"`
	TargetWorkerID string `json:"target_worker_id,omitempty"`
	RunSnapshot    *RunSnapshot
}

type GetBranchStatePayload struct {
	Target         string `json:"target,omitempty"`
	TargetHost     string `json:"target_host,omitempty"`
	TargetWorkerID string `json:"target_worker_id,omitempty"`
	RunSnapshot    *RunSnapshot
}

type GetDiffPayload struct {
	Target         string `json:"target,omitempty"`
	TargetHost     string `json:"target_host,omitempty"`
	TargetWorkerID string `json:"target_worker_id,omitempty"`
	RunSnapshot    *RunSnapshot
}

type CaptureSessionResult struct {
	Content       string `json:"content,omitempty"`
	TimestampUnix int64  `json:"timestamp_unix,omitempty"`
	Source        string `json:"source,omitempty"`
}

type GetDiffStatsResult struct {
	Additions    int      `json:"additions,omitempty"`
	Deletions    int      `json:"deletions,omitempty"`
	FilesChanged int      `json:"files_changed,omitempty"`
	Files        []string `json:"files,omitempty"`
}

type GetBranchStateResult struct {
	State int32 `json:"state,omitempty"`
}

type GetDiffResult struct {
	Diff string `json:"diff,omitempty"`
}

type WorkerEffectResult struct {
	StartRunResult    *StartRunResult       `json:"start_run_result,omitempty"`
	ContinueRunResult *ContinueRunResult    `json:"continue_run_result,omitempty"`
	CaptureResult     *CaptureSessionResult `json:"capture_result,omitempty"`
	DiffStatsResult   *GetDiffStatsResult   `json:"diff_stats_result,omitempty"`
	BranchStateResult *GetBranchStateResult `json:"branch_state_result,omitempty"`
	DiffResult        *GetDiffResult        `json:"diff_result,omitempty"`
}

func normalizeCapabilities(caps []string) []string {
	if len(caps) == 0 {
		return []string{"capture_session", "continue_run", "get_branch_state", "get_diff", "get_diff_stats", "send_message", "start_run", "stop_run"}
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
		return []string{"capture_session", "continue_run", "get_branch_state", "get_diff", "get_diff_stats", "send_message", "start_run", "stop_run"}
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
		return nil, fmt.Errorf("no active workers available; start a local worker on the target host via 'orch worker start'")
	}

	copy := *selected
	copy.Active = true
	return &copy, nil
}

type noActiveWorkerForRunHostError struct {
	Effect   string
	IssueID  string
	RunID    string
	Host     string
	WorkerID string
}

func (e *noActiveWorkerForRunHostError) Error() string {
	host := strings.TrimSpace(e.Host)
	workerID := strings.TrimSpace(e.WorkerID)
	runRef := strings.TrimSpace(e.IssueID)
	if strings.TrimSpace(e.RunID) != "" {
		runRef = fmt.Sprintf("%s#%s", strings.TrimSpace(e.IssueID), strings.TrimSpace(e.RunID))
	}
	if host != "" && runRef != "" && strings.TrimSpace(e.Effect) != "start_run" {
		return fmt.Sprintf("no active worker on host %q where run %s lives (worker_id %q); start orch worker on that host", host, runRef, workerID)
	}
	if host != "" {
		return fmt.Sprintf("no active worker on host %q for target worker %q; start orch worker on that host", host, workerID)
	}
	return fmt.Sprintf("no active worker available for target worker %q; start orch worker on the target host", workerID)
}

func isNoActiveWorkerForRunHostError(err error) bool {
	var targetErr *noActiveWorkerForRunHostError
	return errors.As(err, &targetErr)
}

func (s *SocketServer) acquireWorkerLease(projectID, effect, issueID, runID string, payload *WorkerEffectPayload) (*WorkerLease, error) {
	preferredWorkerID, preferredHost, strict := preferredWorkerPreferenceForPayload(payload)
	worker, err := s.selectActiveWorkerForEffect(effect, preferredWorkerID, strict)
	if err != nil {
		if strict && (strings.TrimSpace(preferredHost) != "" || strings.TrimSpace(preferredWorkerID) != "") {
			return nil, &noActiveWorkerForRunHostError{
				Effect:   strings.TrimSpace(effect),
				IssueID:  strings.TrimSpace(issueID),
				RunID:    strings.TrimSpace(runID),
				Host:     strings.TrimSpace(preferredHost),
				WorkerID: strings.TrimSpace(preferredWorkerID),
			}
		}
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
	// Copy under the lock: leaseWorkForWorker mutates the stored *lease
	// (DispatchCount/ExpiresAt/PayloadJSON) under the same mutex, so reading
	// *lease outside the lock is a data race (flaky under -race).
	copy := *lease
	s.workerLeasesMu.Unlock()

	return &copy, nil
}

func preferredWorkerPreferenceForPayload(payload *WorkerEffectPayload) (string, string, bool) {
	if payload == nil {
		return defaultWorkerID(), "", false
	}
	if payload.StartRun != nil {
		if workerID := strings.TrimSpace(payload.StartRun.TargetWorkerID); workerID != "" {
			return workerID, strings.TrimSpace(payload.StartRun.TargetHost), true
		}
		if host := strings.TrimSpace(payload.StartRun.TargetHost); host != "" {
			return HostWorkerID(host), host, true
		}
		if target := strings.TrimSpace(payload.StartRun.Target); target != "" && target != "local" {
			return target, strings.TrimSpace(payload.StartRun.TargetHost), true
		}
		return defaultWorkerID(), "", false
	}
	if payload.ContinueRun != nil {
		if workerID := strings.TrimSpace(payload.ContinueRun.TargetWorkerID); workerID != "" {
			return workerID, strings.TrimSpace(payload.ContinueRun.TargetHost), true
		}
		if host := strings.TrimSpace(payload.ContinueRun.TargetHost); host != "" {
			return HostWorkerID(host), host, true
		}
		if target := strings.TrimSpace(payload.ContinueRun.Target); target != "" && target != "local" {
			return target, strings.TrimSpace(payload.ContinueRun.TargetHost), true
		}
		return defaultWorkerID(), "", false
	}
	if payload.CaptureSession != nil {
		if workerID := strings.TrimSpace(payload.CaptureSession.TargetWorkerID); workerID != "" {
			return workerID, strings.TrimSpace(payload.CaptureSession.TargetHost), true
		}
		if host := strings.TrimSpace(payload.CaptureSession.TargetHost); host != "" {
			return HostWorkerID(host), host, true
		}
		if target := strings.TrimSpace(payload.CaptureSession.Target); target != "" && target != "local" {
			return target, strings.TrimSpace(payload.CaptureSession.TargetHost), true
		}
		return defaultWorkerID(), "", false
	}
	if payload.SendMessage != nil {
		if workerID := strings.TrimSpace(payload.SendMessage.TargetWorkerID); workerID != "" {
			return workerID, strings.TrimSpace(payload.SendMessage.TargetHost), true
		}
		if host := strings.TrimSpace(payload.SendMessage.TargetHost); host != "" {
			return HostWorkerID(host), host, true
		}
		if target := strings.TrimSpace(payload.SendMessage.Target); target != "" && target != "local" {
			return target, strings.TrimSpace(payload.SendMessage.TargetHost), true
		}
		return defaultWorkerID(), "", false
	}
	if payload.GetDiffStats != nil {
		if workerID := strings.TrimSpace(payload.GetDiffStats.TargetWorkerID); workerID != "" {
			return workerID, strings.TrimSpace(payload.GetDiffStats.TargetHost), true
		}
		if host := strings.TrimSpace(payload.GetDiffStats.TargetHost); host != "" {
			return HostWorkerID(host), host, true
		}
		if target := strings.TrimSpace(payload.GetDiffStats.Target); target != "" && target != "local" {
			return target, strings.TrimSpace(payload.GetDiffStats.TargetHost), true
		}
		return defaultWorkerID(), "", false
	}
	if payload.GetBranchState != nil {
		if workerID := strings.TrimSpace(payload.GetBranchState.TargetWorkerID); workerID != "" {
			return workerID, strings.TrimSpace(payload.GetBranchState.TargetHost), true
		}
		if host := strings.TrimSpace(payload.GetBranchState.TargetHost); host != "" {
			return HostWorkerID(host), host, true
		}
		if target := strings.TrimSpace(payload.GetBranchState.Target); target != "" && target != "local" {
			return target, strings.TrimSpace(payload.GetBranchState.TargetHost), true
		}
		return defaultWorkerID(), "", false
	}
	if payload.GetDiff != nil {
		if workerID := strings.TrimSpace(payload.GetDiff.TargetWorkerID); workerID != "" {
			return workerID, strings.TrimSpace(payload.GetDiff.TargetHost), true
		}
		if host := strings.TrimSpace(payload.GetDiff.TargetHost); host != "" {
			return HostWorkerID(host), host, true
		}
		if target := strings.TrimSpace(payload.GetDiff.Target); target != "" && target != "local" {
			return target, strings.TrimSpace(payload.GetDiff.TargetHost), true
		}
		return defaultWorkerID(), "", false
	}
	if payload.StopRun != nil {
		if workerID := strings.TrimSpace(payload.StopRun.TargetWorkerID); workerID != "" {
			return workerID, strings.TrimSpace(payload.StopRun.TargetHost), true
		}
		if host := strings.TrimSpace(payload.StopRun.TargetHost); host != "" {
			return HostWorkerID(host), host, true
		}
		if target := strings.TrimSpace(payload.StopRun.Target); target != "" && target != "local" {
			return target, strings.TrimSpace(payload.StopRun.TargetHost), true
		}
		return defaultWorkerID(), "", false
	}
	return defaultWorkerID(), "", false
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

	if result.StartRunResult == nil &&
		result.ContinueRunResult == nil &&
		result.CaptureResult == nil &&
		result.DiffStatsResult == nil &&
		result.BranchStateResult == nil &&
		result.DiffResult == nil {
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

func workerLocalProjectMappingError(projectID, workerID string) error {
	projectID = strings.TrimSpace(projectID)
	workerID = strings.TrimSpace(workerID)
	if workerID != "" {
		return fmt.Errorf("no local project mapping for project_id %q on worker %q; run 'orch --remote= daemon repo register /path/to/repo' on that host", projectID, workerID)
	}
	return fmt.Errorf("no local project mapping for project_id %q on this worker; run 'orch --remote= daemon repo register /path/to/repo' on that host", projectID)
}

func runFromLeaseSnapshot(lease *WorkerLease) (*model.Run, error) {
	if lease == nil || lease.Payload == nil {
		return nil, fmt.Errorf("%s run_snapshot missing for leased run %s#%s", strings.TrimSpace(lease.Effect), strings.TrimSpace(lease.IssueID), strings.TrimSpace(lease.RunID))
	}

	var snapshot *RunSnapshot
	switch strings.TrimSpace(lease.Effect) {
	case "stop_run":
		if lease.Payload.StopRun != nil {
			snapshot = lease.Payload.StopRun.RunSnapshot
		}
	case "capture_session":
		if lease.Payload.CaptureSession != nil {
			snapshot = lease.Payload.CaptureSession.RunSnapshot
		}
	case "send_message":
		if lease.Payload.SendMessage != nil {
			snapshot = lease.Payload.SendMessage.RunSnapshot
		}
	case "get_diff_stats":
		if lease.Payload.GetDiffStats != nil {
			snapshot = lease.Payload.GetDiffStats.RunSnapshot
		}
	case "get_branch_state":
		if lease.Payload.GetBranchState != nil {
			snapshot = lease.Payload.GetBranchState.RunSnapshot
		}
	case "get_diff":
		if lease.Payload.GetDiff != nil {
			snapshot = lease.Payload.GetDiff.RunSnapshot
		}
	}

	run, err := modelRunFromSnapshot(snapshot)
	if err != nil {
		return nil, fmt.Errorf("%s run_snapshot invalid for leased run %s#%s: %w", strings.TrimSpace(lease.Effect), strings.TrimSpace(lease.IssueID), strings.TrimSpace(lease.RunID), err)
	}
	return run, nil
}

func (s *SocketServer) executeLeaseEffect(lease *WorkerLease) (*WorkerEffectResult, error) {
	if lease == nil {
		return nil, fmt.Errorf("lease required")
	}
	if err := s.refreshRepoRegistry(); err != nil {
		return nil, fmt.Errorf("failed to refresh local project mappings: %w", err)
	}

	repoCtx := s.ensureRepoContextByID(strings.TrimSpace(lease.ProjectID))
	if repoCtx == nil || repoCtx.Store == nil {
		return nil, workerLocalProjectMappingError(strings.TrimSpace(lease.ProjectID), s.currentWorkerID)
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
		result, err := s.processStartRunCore(repoCtx.Store, repoCtx.ProjectRoot, &optsCopy)
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
		result, err := s.processContinueRunCore(repoCtx.Store, repoCtx.ProjectRoot, &optsCopy)
		if err != nil {
			return nil, err
		}
		return &WorkerEffectResult{ContinueRunResult: result}, nil
	case "stop_run":
		run, err := runFromLeaseSnapshot(lease)
		if err != nil {
			return nil, err
		}
		if err := s.stopRunSession(run); err != nil {
			return nil, err
		}
		return nil, nil
	case "capture_session":
		if lease.Payload == nil || lease.Payload.CaptureSession == nil {
			return nil, fmt.Errorf("capture_session payload missing")
		}
		run, err := runFromLeaseSnapshot(lease)
		if err != nil {
			return nil, err
		}
		lines := lease.Payload.CaptureSession.Lines
		if lines <= 0 {
			lines = 100
		}

		var content string
		var source string
		if run.Agent == "opencode" {
			content, source, err = s.captureOpenCodeSession(run, lines)
		} else {
			content, source, err = captureLocalMultiplexerSession(run, lines)
		}
		if err != nil {
			return nil, err
		}

		return &WorkerEffectResult{
			CaptureResult: &CaptureSessionResult{
				Content:       content,
				TimestampUnix: time.Now().Unix(),
				Source:        source,
			},
		}, nil
	case "send_message":
		if lease.Payload == nil || lease.Payload.SendMessage == nil {
			return nil, fmt.Errorf("send_message payload missing")
		}
		run, err := runFromLeaseSnapshot(lease)
		if err != nil {
			return nil, err
		}
		if err := s.processSendMessageForRun(repoCtx.Store, run, &SendMessageParams{
			IssueID:        lease.IssueID,
			RunID:          lease.RunID,
			Message:        lease.Payload.SendMessage.Message,
			NoEnter:        lease.Payload.SendMessage.NoEnter,
			TargetWorkerID: lease.Payload.SendMessage.TargetWorkerID,
		}); err != nil {
			return nil, err
		}
		return nil, nil
	case "get_diff_stats":
		run, err := runFromLeaseSnapshot(lease)
		if err != nil {
			return nil, err
		}
		stats := git.GetDiffStats(run.WorktreePath, run.Branch, "main")
		return &WorkerEffectResult{
			DiffStatsResult: &GetDiffStatsResult{
				Additions:    stats.Additions,
				Deletions:    stats.Deletions,
				FilesChanged: stats.FilesChanged,
				Files:        append([]string(nil), stats.Files...),
			},
		}, nil
	case "get_branch_state":
		run, err := runFromLeaseSnapshot(lease)
		if err != nil {
			return nil, err
		}
		state := computeBranchStateWithRunner(s.gitRunner, run.WorktreePath, run.Branch, "main")
		return &WorkerEffectResult{
			BranchStateResult: &GetBranchStateResult{State: int32(state)},
		}, nil
	case "get_diff":
		run, err := runFromLeaseSnapshot(lease)
		if err != nil {
			return nil, err
		}

		diff := ""
		if run.WorktreePath != "" && run.Branch != "" {
			output, err := s.gitRunner.Diff(context.Background(), run.WorktreePath, "main..."+run.Branch)
			if err == nil {
				diff = output
			}
		}

		return &WorkerEffectResult{
			DiffResult: &GetDiffResult{Diff: diff},
		}, nil
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
		var leaseSnapshot *WorkerLease
		if lease != nil {
			copy := *lease
			leaseSnapshot = &copy
		}
		s.workerLeasesMu.RUnlock()

		if leaseSnapshot == nil {
			return nil, fmt.Errorf("lease not found: %s", leaseID)
		}
		if leaseSnapshot.Completed {
			if leaseSnapshot.Success {
				return leaseSnapshot, nil
			}
			if strings.TrimSpace(leaseSnapshot.Error) != "" {
				return leaseSnapshot, errors.New(leaseSnapshot.Error)
			}
			return leaseSnapshot, fmt.Errorf("worker lease failed: %s", leaseID)
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("worker lease timed out: %s", leaseID)
		}

		// Fail fast when the executing worker disappears: without this the
		// master silently waits out the full lease timeout for a completion
		// that can never arrive, and the caller sees a generic timeout
		// instead of the real failure. Workers heartbeat on a dedicated
		// goroutine even while executing effects, so an expired heartbeat
		// here means the worker process is genuinely gone.
		s.workersMu.RLock()
		executingWorker := s.workers[leaseSnapshot.WorkerID]
		workerAlive := s.workerIsActive(executingWorker, time.Now())
		s.workersMu.RUnlock()
		if !workerAlive {
			return nil, fmt.Errorf("worker %s lost while executing lease %s (heartbeat expired, effect %s); the effect may still be running on its host — check `orch worker status` there", leaseSnapshot.WorkerID, leaseID, leaseSnapshot.Effect)
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
