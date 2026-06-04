package orchapi

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/s22625/orch/internal/model"
)

type IssueStatus string

const (
	IssueStatusOpen     IssueStatus = "open"
	IssueStatusResolved IssueStatus = "resolved"
	IssueStatusClosed   IssueStatus = "closed"
)

type RunStatus string

const (
	RunStatusQueued      RunStatus = "queued"
	RunStatusBooting     RunStatus = "booting"
	RunStatusRunning     RunStatus = "running"
	RunStatusWaiting     RunStatus = "waiting"
	RunStatusRateLimited RunStatus = "rate_limited"
	RunStatusPROpen      RunStatus = "pr_open"
	RunStatusDone        RunStatus = "done"
	RunStatusFailed      RunStatus = "failed"
	RunStatusCanceled    RunStatus = "canceled"
)

func NormalizeRunStatus(s string) RunStatus {
	switch s {
	case "blocked":
		return RunStatusWaiting
	case "blocked_api":
		return RunStatusRateLimited
	default:
		return RunStatus(s)
	}
}

type BranchState string

const (
	BranchStateClean    BranchState = "clean"
	BranchStateDirty    BranchState = "dirty"
	BranchStateMerged   BranchState = "merged"
	BranchStateConflict BranchState = "conflict"
)

type Multiplexer string

const (
	MultiplexerTmux   Multiplexer = "tmux"
	MultiplexerZellij Multiplexer = "zellij"
)

// RunRef identifies a run using one of three formats:
//   - ShortID: 2-6 char hex prefix (e.g., "a3b4c5")
//   - Full: IssueID + RunID (e.g., "my-task#20231220-100000")
//   - Latest: IssueID only (resolves to latest run)
type RunRef struct {
	IssueID model.IssueID
	RunID   model.RunID
	ShortID model.ShortID
}

var shortIDRegex = regexp.MustCompile(`^[0-9a-f]{2,6}$`)

func ParseRunRef(s string) (RunRef, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return RunRef{}, ErrInvalidRef
	}

	if shortIDRegex.MatchString(s) {
		return RunRef{ShortID: model.ShortID(s)}, nil
	}

	lastHash := strings.LastIndex(s, "#")
	if lastHash == -1 {
		return RunRef{IssueID: model.IssueID(s)}, nil
	}

	candidate := s[lastHash+1:]
	if looksLikeRunID(candidate) {
		return RunRef{
			IssueID: model.IssueID(s[:lastHash]),
			RunID:   model.RunID(candidate),
		}, nil
	}
	return RunRef{IssueID: model.IssueID(s)}, nil
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

func (r RunRef) IsShortID() bool {
	return r.ShortID != ""
}

func (r RunRef) IsLatest() bool {
	return r.ShortID == "" && r.RunID == "" && r.IssueID != ""
}

func (r RunRef) IsFull() bool {
	return r.IssueID != "" && r.RunID != ""
}

func (r RunRef) String() string {
	if r.ShortID != "" {
		return string(r.ShortID)
	}
	if r.RunID == "" {
		return string(r.IssueID)
	}
	return fmt.Sprintf("%s#%s", r.IssueID, r.RunID)
}

// ComputeShortID returns the 6-char hex short ID derived from issueID#runID
// via SHA-256. Mirrors model.GenerateShortID so callers (CLI hints,
// notifications) present the same short reference users see in `orch ps`.
func ComputeShortID(issueID model.IssueID, runID model.RunID) model.ShortID {
	h := sha256.Sum256([]byte(string(issueID) + "#" + string(runID)))
	return model.ShortID(hex.EncodeToString(h[:])[:6])
}

// RunEvent is a single state transition observed for a run.
type RunEvent struct {
	Timestamp time.Time
	IssueID   model.IssueID
	RunID     model.RunID
	ShortID   model.ShortID
	From      RunStatus
	To        RunStatus
	Source    string // "user" | "daemon" | "agent"
	ProjectID model.ProjectID
}

// RunEventFilter narrows a subscription to events matching the given fields.
// Empty fields match anything.
type RunEventFilter struct {
	IssueID model.IssueID
	RunID   model.RunID
}

// RunEventStream is a long-lived subscription to run state transitions.
// Events emits frames until Close is called or the daemon disconnects;
// after Events closes, Err returns any terminal error or nil for clean EOF.
type RunEventStream interface {
	Events() <-chan *RunEvent
	Err() error
	Close() error
}

type Issue struct {
	ID          model.IssueID
	Title       string
	Topic       string
	Summary     string
	Status      IssueStatus
	Tags        []string
	Body        string
	Path        string
	BaseBranch  string
	Frontmatter map[string]string
	ModifiedAt  time.Time
}

type Run struct {
	IssueID           model.IssueID
	RunID             model.RunID
	ShortID           model.ShortID
	IssueStatus       string
	IssueTopic        string
	Status            RunStatus
	IsActive          bool
	IsTerminal        bool
	Phase             string
	Agent             string
	Model             string
	ModelVariant      string
	Branch            string
	WorktreePath      string
	Target            string
	TargetHost        string
	TargetWorkerID    string
	SessionName       string
	Multiplexer       Multiplexer
	PRUrl             string
	PRNumber          int
	PRState           string
	ServerPort        int
	OpenCodeSessionID string
	ContinuedFrom     string
	DiffStats         *DiffStats
	BranchState       BranchState
	ElapsedSeconds    int
	ElapsedDisplay    string
	Alive             bool
	AliveKnown        bool
	WorktreeExists    bool
	StartedAt         time.Time
	UpdatedAt         time.Time
	Events            []*Event
}

func (r *Run) Ref() RunRef {
	return RunRef{IssueID: r.IssueID, RunID: r.RunID}
}

type WaitForRunsResult struct {
	RunID   model.RunID
	Status  RunStatus
	IssueID model.IssueID
	PRURL   string
}

type Event struct {
	Timestamp time.Time
	Type      string
	Name      string
	Attrs     map[string]string
}

type DiffStats struct {
	Additions    int
	Deletions    int
	FilesChanged int
	Files        []string
}

type AttachInfo struct {
	IssueID           model.IssueID
	RunID             model.RunID
	ShortID           model.ShortID
	Agent             string
	SessionName       string
	Multiplexer       Multiplexer
	WorktreePath      string
	ServerPort        int
	OpenCodeSessionID string
	Branch            string
	TargetHost        string
	SessionExists     bool
}

type CaptureResult struct {
	Content   string
	Timestamp time.Time
	Source    string
}

type OpenCodeServerInfo struct {
	Port    int
	Healthy bool
}

type MonitorRegistration struct {
	MonitorID string
}

type ListIssuesFilter struct {
	Status     []IssueStatus
	Tags       []string
	TagsMode   string
	TextSearch string
	Limit      int
	Cursor     string
}

type ListIssuesResult struct {
	Issues     []*Issue
	Total      int
	NextCursor string
}

type CreateIssueRequest struct {
	ID         model.IssueID
	Title      string
	Body       string
	Tags       []string
	BaseBranch string
}

type ListRunsFilter struct {
	IssueID    model.IssueID
	Status     []RunStatus
	Agent      string
	TextSearch string
	TimeRange  string
	Limit      int
	Cursor     string
	OlderThan  string
}

type ListRunsResult struct {
	Runs       []*Run
	Total      int
	NextCursor string
}

type StartRunRequest struct {
	IssueID        model.IssueID
	RunID          model.RunID
	Agent          string
	AgentCmd       string
	AgentProfile   string
	CodexProfile   string
	Model          string
	ModelVariant   string
	BaseBranch     string
	Branch         string
	WorktreeDir    string
	NoPR           bool
	PromptTemplate string
	PRTargetBranch string
	DryRun         bool
	Reuse          bool
	Multiplexer    string
	Target         string
	Preset         string
}

type StartRunResult struct {
	RunID        model.RunID
	Branch       string
	WorktreePath string
	SessionName  string
	Status       string
}

type CreateRunRequest struct {
	IssueID  model.IssueID
	RunID    model.RunID
	Metadata map[string]string
}

type CreateRunResult struct {
	IssueID model.IssueID
	RunID   model.RunID
	Path    string
}

type AppendEventResult struct {
	Skipped bool
	Reason  string
}

type DeleteRunOptions struct {
	WithWorktree bool
	WithBranch   bool
	Force        bool
}

type DeleteRunResult struct {
	IssueID         model.IssueID
	RunID           model.RunID
	ShortID         model.ShortID
	WorktreeRemoved bool
	BranchRemoved   bool
	SessionKilled   bool
}

type CleanRunWorktreeResult struct {
	IssueID         model.IssueID
	RunID           model.RunID
	ShortID         model.ShortID
	WorktreePath    string
	WorktreeRemoved bool
	Skipped         bool
	Reason          string
}

type UpdateIssueRequest struct {
	Title   string
	Summary string
	Body    string
	Status  IssueStatus
}

type ValidateIssueFilesResult struct {
	Total      int
	Valid      int
	Errors     []*ValidationResult
	Warnings   []*ValidationResult
	Duplicates []DuplicateID
}

type ValidationResult struct {
	File     string
	IssueID  model.IssueID
	Errors   []ValidationIssue
	Warnings []ValidationIssue
}

type ValidationIssue struct {
	Code    string
	Message string
	Line    int
	Level   string
}

type DuplicateID struct {
	ID    model.IssueID
	Files []string
}

type RepairOptions struct {
	DryRun bool
	Force  bool
}

type RepairResult struct {
	ProblemsFound int
	ProblemsFixed int
	Details       []string
}

type ProviderInfo struct {
	ID     string
	Name   string
	Models []ModelInfo
}

type ModelInfo struct {
	ID       string
	Name     string
	Variants []string
}

type QueryOpenCodeServerResult struct {
	ServerRunning bool
	Providers     []ProviderInfo
}

type SlackConfig struct {
	Enabled    bool
	WebhookURL string
	BotToken   string
	Channel    string
	NotifyOn   []string
}

type OpenCodeConfig struct {
	DefaultModel     string
	DefaultVariant   string
	PromptTemplate   string
	ExtraArgs        []string
	ControlExtraArgs []string
}

type ClaudeConfig struct {
	PromptTemplate   string
	ExtraArgs        []string
	ControlExtraArgs []string
}

type CodexConfig struct {
	PromptTemplate   string
	ExtraArgs        []string
	ControlExtraArgs []string
}

type GeminiConfig struct {
	PromptTemplate   string
	ExtraArgs        []string
	ControlExtraArgs []string
}

type Preset struct {
	Name    string
	Backend string
	Model   string
	Variant string
	Profile string
}

type IssuesConfig struct {
	Backend string
	Path    string
}

type GitHubConfig struct {
	Owner        string
	Repo         string
	LabelFilter  string
	PollInterval int
	StatusLabels map[string]string
}

type MonitorConfig struct {
	PSColumns []string
}

type PSConfig struct {
	DefaultStatuses []string
}

type Config struct {
	Agent               string
	Model               string
	ModelVariant        string
	WorktreeDir         string
	BaseBranch          string
	PRTargetBranch      string
	LogLevel            string
	PromptTemplate      string
	Multiplexer         string
	MonitorMultiplexer  string
	AgentMultiplexer    string
	NoPR                bool
	DefaultPreset       string
	ControlAgent        string
	ControlModel        string
	ControlModelVariant string
	DiffTool            string
	PS                  PSConfig
	Monitor             MonitorConfig
	Presets             []Preset
	OpenCode            OpenCodeConfig
	Claude              ClaudeConfig
	Codex               CodexConfig
	Gemini              GeminiConfig
	Slack               SlackConfig
	Issues              IssuesConfig
	GitHub              GitHubConfig
}

// ResolveControlModelAndVariant returns the effective model and variant for a
// control agent.  Precedence: ControlModel/ControlModelVariant > agent-specific
// config (e.g. opencode defaults) > generic config (Model/ModelVariant).
func (c *Config) ResolveControlModelAndVariant(agent string) (string, string) {
	model := c.ControlModel
	variant := c.ControlModelVariant

	if model == "" {
		switch agent {
		case "opencode":
			model = c.OpenCode.DefaultModel
		}
	}
	if model == "" {
		model = c.Model
	}

	if variant == "" {
		switch agent {
		case "opencode":
			variant = c.OpenCode.DefaultVariant
		}
	}
	if variant == "" {
		variant = c.ModelVariant
	}

	return model, variant
}

type DaemonStatus struct {
	Running bool
	PID     int
	LogPath string
	Version string
}

type ControlAgentConfig struct {
	PromptContent string
	Agent         string
	Model         string
	ModelVariant  string
	ExtraArgs     []string
	// CodexHome is the resolved CODEX_HOME for a codex control agent (from the
	// project's default codex profile). Empty means the agent default (~/.codex).
	CodexHome string
}

type ContinueRunRequest struct {
	IssueID        model.IssueID
	RunID          model.RunID
	ShortID        model.ShortID
	Branch         string
	Agent          string
	AgentCmd       string
	AgentProfile   string
	CodexProfile   string
	WorktreeDir    string
	NoPR           bool
	PromptTemplate string
	PRTargetBranch string
	Multiplexer    string
	SessionName    string
}

type ContinueRunResult struct {
	RunID         model.RunID
	Branch        string
	WorktreePath  string
	SessionName   string
	Status        string
	ContinuedFrom string
	IssueID       model.IssueID
}
