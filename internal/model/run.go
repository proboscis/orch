package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// RunRef represents a reference to a run (ISSUE_ID#RUN_ID)
type RunRef struct {
	IssueID IssueID
	RunID   RunID
}

// ParseRunRef parses a RUN_REF string (ISSUE_ID#RUN_ID or just ISSUE_ID for latest)
func ParseRunRef(ref string) (*RunRef, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("empty run reference")
	}

	lastHash := strings.LastIndex(ref, "#")
	if lastHash == -1 {
		return &RunRef{IssueID: NewIssueID(ref)}, nil
	}

	candidate := ref[lastHash+1:]
	if looksLikeRunID(candidate) {
		return &RunRef{
			IssueID: NewIssueID(ref[:lastHash]),
			RunID:   NewRunID(candidate),
		}, nil
	}
	return &RunRef{IssueID: NewIssueID(ref)}, nil
}

func looksLikeRunID(s string) bool {
	if len(s) < 8 {
		return false
	}
	for i := 0; i < 8; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// String returns the canonical RUN_REF format
func (r *RunRef) String() string {
	if r.RunID == "" {
		return r.IssueID.String()
	}
	return r.IssueID.String() + "#" + r.RunID.String()
}

// IsLatest returns true if this ref points to the latest run (no RunID specified)
func (r *RunRef) IsLatest() bool {
	return r.RunID == ""
}

// Run represents a single execution of an issue
type Run struct {
	IssueID IssueID
	RunID   RunID
	Path    string // File path to run document

	// Derived from events
	Status    Status
	Phase     Phase
	Events    []*Event
	StartedAt time.Time
	UpdatedAt time.Time

	// Artifacts (from events)
	Agent             string
	Model             string
	ModelVariant      string
	Branch            string
	WorktreePath      string
	Target            string
	TargetHost        string
	TargetWorkerID    string
	SessionName       string
	MuxWindowID       string
	Multiplexer       string
	PRUrl             string
	PRNumber          int
	PRState           string
	ServerPort        int
	OpenCodeSessionID string

	// Frontmatter metadata
	ContinuedFrom string

	BranchState string

	// Daemon-computed fields
	Alive          bool
	AliveKnown     bool
	WorktreeExists bool
}

// Ref returns the RunRef for this run
func (r *Run) Ref() *RunRef {
	return &RunRef{
		IssueID: r.IssueID,
		RunID:   r.RunID,
	}
}

// ShortID returns a 6-character hex identifier for the run (git-style)
func (r *Run) ShortID() ShortID {
	return GenerateShortID(r.IssueID, r.RunID)
}

// GenerateShortID generates a 6-char hex ID from issue and run IDs
func GenerateShortID(issueID IssueID, runID RunID) ShortID {
	h := sha256.Sum256([]byte(issueID.String() + "#" + runID.String()))
	return ShortID(hex.EncodeToString(h[:])[:6])
}

// GenerateWorktreeName generates a worktree directory name using a short ID.
func GenerateWorktreeName(issueID IssueID, runID RunID, agent string) string {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		agent = "unknown"
	}
	return fmt.Sprintf("%s_%s_%s", GenerateShortID(issueID, runID), agent, runID)
}

// GetStatus derives status from events (last status event wins)
func (r *Run) GetStatus() Status {
	for i := len(r.Events) - 1; i >= 0; i-- {
		e := r.Events[i]
		if e.Type == EventTypeStatus {
			return NormalizeStatus(e.Name)
		}
	}
	return StatusQueued
}

// GetPhase derives phase from events (last phase event wins).
func (r *Run) GetPhase() Phase {
	for i := len(r.Events) - 1; i >= 0; i-- {
		e := r.Events[i]
		if e.Type == EventTypePhase {
			return Phase(e.Name)
		}
	}
	return ""
}

// GetArtifacts extracts artifacts from events
func (r *Run) GetArtifacts() map[string]map[string]string {
	artifacts := make(map[string]map[string]string)
	for _, e := range r.Events {
		if e.Type == EventTypeArtifact {
			artifacts[e.Name] = e.Attrs
		}
	}
	return artifacts
}

// DeriveState updates Status and artifacts from events
func (r *Run) DeriveState() {
	r.Status = r.GetStatus()
	r.Phase = r.GetPhase()

	artifacts := r.GetArtifacts()
	if worktree, ok := artifacts["worktree"]; ok {
		r.WorktreePath = worktree["path"]
	}
	if branch, ok := artifacts["branch"]; ok {
		r.Branch = branch["name"]
	}
	if target, ok := artifacts["target"]; ok {
		r.Target = target["name"]
		if host := target["host"]; host != "" {
			r.TargetHost = host
		} else if r.TargetHost == "" {
			r.TargetHost = r.lastArtifactAttr("target", "host")
		}
		if workerID := target["worker_id"]; workerID != "" {
			r.TargetWorkerID = workerID
		} else if r.TargetWorkerID == "" {
			r.TargetWorkerID = r.lastArtifactAttr("target", "worker_id")
		}
	}
	if session, ok := artifacts["session"]; ok {
		if sessionName := session["name"]; sessionName != "" {
			r.SessionName = sessionName
		}
		if mux := session["multiplexer"]; mux != "" {
			r.Multiplexer = mux
		} else if r.Multiplexer == "" {
			r.Multiplexer = r.lastArtifactAttr("session", "multiplexer")
		}
		if r.TargetHost == "" {
			if host := session["host"]; host != "" {
				r.TargetHost = host
			} else {
				r.TargetHost = r.lastArtifactAttr("session", "host")
			}
		}
		if r.TargetWorkerID == "" {
			if workerID := session["worker_id"]; workerID != "" {
				r.TargetWorkerID = workerID
			} else {
				r.TargetWorkerID = r.lastArtifactAttr("session", "worker_id")
			}
		}
	}
	if window, ok := artifacts["window"]; ok {
		r.MuxWindowID = window["id"]
	}
	if pr, ok := artifacts["pr"]; ok {
		r.PRUrl = pr["url"]
	}
	if server, ok := artifacts["server"]; ok {
		if portStr, ok := server["port"]; ok {
			if port, err := strconv.Atoi(portStr); err == nil {
				r.ServerPort = port
			}
		}
	}
	if opencodeSession, ok := artifacts["opencode_session"]; ok {
		r.OpenCodeSessionID = opencodeSession["id"]
	}
	if agentModel, ok := artifacts["agent_model"]; ok {
		if r.Model == "" {
			r.Model = agentModel["model"]
		}
		if r.ModelVariant == "" {
			r.ModelVariant = agentModel["variant"]
		}
	}

	// Derive timestamps
	if len(r.Events) > 0 {
		r.StartedAt = r.Events[0].Timestamp
		r.UpdatedAt = r.Events[len(r.Events)-1].Timestamp
	}
}

func (r *Run) lastArtifactAttr(name, key string) string {
	for i := len(r.Events) - 1; i >= 0; i-- {
		e := r.Events[i]
		if e == nil || e.Type != EventTypeArtifact || e.Name != name || e.Attrs == nil {
			continue
		}
		if v := e.Attrs[key]; v != "" {
			return v
		}
	}
	return ""
}

// GenerateRunID generates a run ID using the convention YYYYMMDD-HHMMSS
func GenerateRunID() RunID {
	return RunID(time.Now().Format("20060102-150405"))
}

// GenerateBranchName generates a branch name using the convention
func GenerateBranchName(issueID IssueID, runID RunID) string {
	return fmt.Sprintf("issue/%s/run-%s", issueID, runID)
}

// GenerateSessionName generates a session name using the convention
func GenerateSessionName(issueID IssueID, runID RunID) string {
	return fmt.Sprintf("run-%s-%s", issueID, runID)
}
