package monitor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/multiplexer"
	"github.com/s22625/orch/internal/orchapi"
	"github.com/s22625/orch/internal/xdg"
)

const (
	defaultSessionName  = "orch-monitor"
	dashboardWindowName = "dashboard"
	dashboardWindowIdx  = 0
)

const (
	runsPaneTitle   = "runs"
	issuesPaneTitle = "issues"
	chatPaneTitle   = "chat"
)

const (
	runsPaneOption   = "@orch_runs_pane"
	issuesPaneOption = "@orch_issues_pane"
	chatPaneOption   = "@orch_chat_pane"
)

type Options struct {
	Session         string
	Issue           string
	Statuses        []model.Status
	RunSort         SortKey
	IssueSort       SortKey
	Agent           string
	Attach          bool
	ForceNew        bool
	NewControlAgent bool // If true, also restart the control agent session
	OrchPath        string
	GlobalFlags     []string
	ShowResolved    bool
	ShowClosed      bool
	UISettings      *UISettings
	ProjectRoot     string
}

type Monitor struct {
	session         string
	runFilter       RunFilter
	runSort         SortKey
	runSortDir      SortDirection
	issueSort       SortKey
	issueSortDir    SortDirection
	api             orchapi.OrchAPI
	issuesRoot      string
	orchPath        string
	globalFlags     []string
	agent           string
	attach          bool
	forceNew        bool
	newControlAgent bool
	runs            []*RunWindow
	dashboard       *Dashboard
	showResolved    bool
	showClosed      bool
	uiSettings      *UISettings
	orchDir         string
	presets         []config.Preset
	projectRoot     string
	logger          *log.Logger
	mux             multiplexer.Multiplexer

	monitorID     string
	heartbeatStop chan struct{}
}

// RunWindow links a run to a dashboard index.
type RunWindow struct {
	Index        int
	Run          *model.Run
	AgentSession string
}

type issueDisplay struct {
	status  string
	topic   string
	summary string
}

func New(api orchapi.OrchAPI, issuesRoot string, opts Options) *Monitor {
	projectRoot := opts.ProjectRoot
	if projectRoot == "" {
		if pr, err := config.GetProjectRoot(); err == nil {
			projectRoot = pr
		}
	}
	session := opts.Session
	if session == "" {
		session = sessionNameForProject(projectRoot)
	}
	orchPath := resolveOrchPath(opts.OrchPath)
	runSort := opts.RunSort
	if !IsValidSortKey(runSort) {
		runSort = SortByUpdated
	}
	issueSort := opts.IssueSort
	if !IsValidSortKey(issueSort) {
		issueSort = SortByName
	}
	uiSettings := opts.UISettings
	if uiSettings == nil {
		uiSettings = DefaultUISettings()
	}
	runSortDir := uiSettings.RunSortDir
	if runSort != uiSettings.RunSort {
		runSortDir = DefaultSortDirection(runSort)
	}
	issueSortDir := uiSettings.IssueSortDir
	if issueSort != uiSettings.IssueSort {
		issueSortDir = DefaultSortDirection(issueSort)
	}
	orchDir := GetOrchDir(projectRoot)
	var presets []config.Preset
	if cfg, err := config.Load(); err == nil {
		presets = cfg.GetAllPresets()
	}

	var logger *log.Logger
	if err := xdg.EnsureStateDir(); err == nil {
		logPath := filepath.Join(xdg.StateDir(), "monitor.log")
		if logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
			logger = log.New(logFile, "", log.LstdFlags)
			logger.Printf("monitor starting: projectRoot=%q issuesRoot=%q", projectRoot, issuesRoot)
		}
	}
	if logger == nil {
		logger = log.New(os.Stderr, "[monitor] ", log.LstdFlags)
	}

	mux := multiplexer.GetDefault()

	return &Monitor{
		session:         session,
		runFilter:       newRunFilter(opts),
		runSort:         runSort,
		runSortDir:      runSortDir,
		issueSort:       issueSort,
		issueSortDir:    issueSortDir,
		api:             api,
		issuesRoot:      issuesRoot,
		orchPath:        orchPath,
		globalFlags:     opts.GlobalFlags,
		agent:           opts.Agent,
		attach:          opts.Attach,
		forceNew:        opts.ForceNew,
		newControlAgent: opts.NewControlAgent,
		showResolved:    opts.ShowResolved,
		showClosed:      opts.ShowClosed,
		uiSettings:      uiSettings,
		orchDir:         orchDir,
		presets:         presets,
		projectRoot:     projectRoot,
		logger:          logger,
		mux:             mux,
	}
}

func (m *Monitor) RunSort() SortKey {
	return m.runSort
}

func (m *Monitor) RunSortDir() SortDirection {
	return m.runSortDir
}

func (m *Monitor) IssueSort() SortKey {
	return m.issueSort
}

func (m *Monitor) IssueSortDir() SortDirection {
	return m.issueSortDir
}

func (m *Monitor) CycleRunSort() SortKey {
	m.runSort = NextRunSortKey(m.runSort)
	m.runSortDir = DefaultSortDirection(m.runSort)
	m.saveUISettings()
	return m.runSort
}

func (m *Monitor) CycleIssueSort() SortKey {
	m.issueSort = NextIssueSortKey(m.issueSort)
	m.issueSortDir = DefaultSortDirection(m.issueSort)
	m.saveUISettings()
	return m.issueSort
}

func (m *Monitor) SetRunSort(key SortKey) {
	if IsValidRunSortKey(key) {
		if m.runSort != key {
			m.runSort = key
			m.runSortDir = DefaultSortDirection(key)
		}
		m.saveUISettings()
	}
}

func (m *Monitor) SetIssueSort(key SortKey) {
	if IsValidIssueSortKey(key) {
		if m.issueSort != key {
			m.issueSort = key
			m.issueSortDir = DefaultSortDirection(key)
		}
		m.saveUISettings()
	}
}

func (m *Monitor) ToggleRunSortDir() SortDirection {
	if m.runSortDir == SortAsc {
		m.runSortDir = SortDesc
	} else {
		m.runSortDir = SortAsc
	}
	m.saveUISettings()
	return m.runSortDir
}

func (m *Monitor) ToggleIssueSortDir() SortDirection {
	if m.issueSortDir == SortAsc {
		m.issueSortDir = SortDesc
	} else {
		m.issueSortDir = SortAsc
	}
	m.saveUISettings()
	return m.issueSortDir
}

// ShowResolved returns whether resolved issues should be shown.
func (m *Monitor) ShowResolved() bool {
	return m.showResolved
}

// ShowClosed returns whether closed issues should be shown.
func (m *Monitor) ShowClosed() bool {
	return m.showClosed
}

// SetShowResolved sets whether resolved issues should be shown and saves the setting.
func (m *Monitor) SetShowResolved(show bool) {
	m.showResolved = show
	m.saveUISettings()
}

// SetShowClosed sets whether closed issues should be shown and saves the setting.
func (m *Monitor) SetShowClosed(show bool) {
	m.showClosed = show
	m.saveUISettings()
}

func (m *Monitor) saveUISettings() {
	if m.orchDir == "" {
		return
	}
	settings := &UISettings{
		RunSort:      m.runSort,
		RunSortDir:   m.runSortDir,
		IssueSort:    m.issueSort,
		IssueSortDir: m.issueSortDir,
		ShowResolved: m.showResolved,
		ShowClosed:   m.showClosed,
	}
	// Ignore errors - settings persistence is best-effort
	_ = SaveUISettings(m.orchDir, settings)
}

// sessionNameForProject generates a unique monitor session name based on the project root path.
// This ensures each project has its own monitor session.
func sessionNameForProject(projectRoot string) string {
	if projectRoot == "" {
		return defaultSessionName
	}

	// Normalize the path to handle symlinks and relative paths
	absPath, err := filepath.Abs(projectRoot)
	if err != nil {
		absPath = projectRoot
	}
	// Try to resolve symlinks for consistent naming
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = resolved
	}

	// Generate a short hash of the path for uniqueness
	hash := sha256.Sum256([]byte(absPath))
	shortHash := hex.EncodeToString(hash[:])[:6]

	// Use the last directory component for readability
	baseName := filepath.Base(absPath)
	// Clean up the base name for tmux session naming
	baseName = strings.ReplaceAll(baseName, ".", "-")
	baseName = strings.ReplaceAll(baseName, " ", "-")

	return fmt.Sprintf("orch-%s-%s", baseName, shortHash)
}

func (m *Monitor) ensureDaemonHealthy() error {
	if m.api == nil {
		return fmt.Errorf("api not initialized")
	}
	ctx := context.Background()
	if repairAPI, ok := m.api.(interface{ EnsureDaemonHealthy(context.Context) error }); ok {
		if err := repairAPI.EnsureDaemonHealthy(ctx); err != nil {
			return fmt.Errorf("daemon health check failed: %w\nRun 'orch repair' to diagnose further", err)
		}
		return nil
	}

	if err := m.api.Ping(ctx); err != nil {
		return fmt.Errorf("daemon health check failed: %w\nRun 'orch repair' to diagnose further", err)
	}
	return nil
}

func (m *Monitor) checkDaemonHealth() error {
	if m.api == nil {
		return fmt.Errorf("api not initialized")
	}
	ctx := context.Background()
	return m.api.Ping(ctx)
}

func (m *Monitor) isDaemonHealthy() bool {
	return m.checkDaemonHealth() == nil
}

func (m *Monitor) Start() error {
	if err := m.ensureDaemonHealthy(); err != nil {
		return fmt.Errorf("daemon health check failed: %w", err)
	}

	if !m.mux.IsAvailable() {
		return fmt.Errorf("%s is not available", m.mux.Type())
	}

	// forceNew restarts the layout (kills session)
	// newControlAgent additionally restarts the control agent (clears session file)
	if m.forceNew || m.newControlAgent {
		// Only clear control session if explicitly requested
		if m.newControlAgent {
			if err := ClearControlSession(m.orchDir); err != nil {
				return fmt.Errorf("failed to clear control session: %w", err)
			}
		}
		if m.mux.HasSession(m.session) {
			if m.mux.IsInsideSession() {
				if current, err := m.mux.CurrentSession(); err == nil && current == m.session {
					return fmt.Errorf("cannot use --new from inside %s; detach and rerun", m.session)
				}
			}
			if err := m.mux.KillSession(m.session); err != nil {
				return fmt.Errorf("failed to kill existing monitor session: %w", err)
			}
		}
	}

	sessionExists := m.mux.HasSession(m.session)
	if sessionExists && m.attach {
		m.registerWithDaemon()
		return m.attachSession()
	}

	if !sessionExists {
		if err := m.createSession(); err != nil {
			return err
		}
	}

	if err := m.ensurePaneLayout(); err != nil {
		return err
	}
	if err := m.repairSwappedMonitorChat(); err != nil {
		return err
	}
	m.refreshChatPaneTitle()

	runs, err := m.loadRuns()
	if err != nil {
		return err
	}
	m.runs = runs

	m.registerWithDaemon()

	return m.attachSession()
}

// RunDashboard launches the bubbletea dashboard.
func (m *Monitor) RunDashboard() error {
	d := NewDashboard(m)
	m.dashboard = d
	return d.Run()
}

// RunIssuesDashboard launches the issues dashboard.
func (m *Monitor) RunIssuesDashboard() error {
	d := NewIssueDashboard(m)
	return d.Run()
}

// Refresh reloads runs and syncs tmux windows.
func (m *Monitor) Refresh() ([]RunRow, error) {
	runs, err := m.loadRuns()
	if err != nil {
		return nil, err
	}
	m.runs = runs
	rows, err := m.buildRunRows(runs)
	if err != nil {
		return nil, err
	}
	filtered := m.runFilter.FilterRows(rows, time.Now())
	reindexRunRows(filtered)
	return filtered, nil
}

// RunFilter returns the active run filter.
func (m *Monitor) RunFilter() RunFilter {
	return m.runFilter.Clone()
}

// SetRunFilter updates the active run filter.
func (m *Monitor) SetRunFilter(filter RunFilter) {
	m.runFilter = normalizeRunFilter(filter)
}

func (m *Monitor) RefreshIssues() ([]IssueRow, error) {
	ctx := context.Background()
	issuesResult, err := m.api.ListIssues(ctx, nil)
	if err != nil {
		return nil, err
	}
	runsResult, err := m.api.ListRuns(ctx, nil)
	if err != nil {
		return nil, err
	}
	issues := apiIssuesToModel(issuesResult.Issues)
	runs := apiRunsToModel(runsResult.Runs)
	return m.buildIssueRows(issues, runs), nil
}

// SwitchWindow selects a window by index.
func (m *Monitor) SwitchWindow(index int) error {
	return m.mux.SelectWindow(m.session, index)
}

// SwitchRuns switches to the runs dashboard window.
func (m *Monitor) SwitchRuns() error {
	if err := m.mux.SelectWindow(m.session, dashboardWindowIdx); err != nil {
		return err
	}
	return m.selectPaneByOption(runsPaneOption, runsPaneTitle)
}

// SwitchIssues switches to the issues dashboard window.
func (m *Monitor) SwitchIssues() error {
	if err := m.mux.SelectWindow(m.session, dashboardWindowIdx); err != nil {
		return err
	}
	return m.selectPaneByOption(issuesPaneOption, issuesPaneTitle)
}

// SwitchChat switches to the agent chat window.
func (m *Monitor) SwitchChat() error {
	if err := m.mux.SelectWindow(m.session, dashboardWindowIdx); err != nil {
		return err
	}
	pane, err := m.findChatPane()
	if err != nil {
		return err
	}
	return m.mux.SelectPane(pane)
}

func (m *Monitor) OpenRun(run *model.Run) error {
	if run == nil {
		return fmt.Errorf("run not found")
	}
	attacher := GetRunAttacher(run.Agent)
	return attacher.Attach(m, run)
}

// CloseRun returns to the dashboard window.
func (m *Monitor) CloseRun() error {
	return m.mux.SelectWindow(m.session, dashboardWindowIdx)
}

func (m *Monitor) Quit() error {
	m.unregisterFromDaemon()
	return m.mux.KillSession(m.session)
}

// StopRun kills the run session and marks the run canceled.
func (m *Monitor) StopRun(run *model.Run) error {
	if run.Status.IsTerminal() {
		return nil
	}

	sessionName := run.SessionName
	if sessionName == "" {
		sessionName = model.GenerateSessionName(run.IssueID, run.RunID)
	}

	if m.mux.HasSession(sessionName) {
		_ = m.mux.KillSession(sessionName)
	}

	ctx := context.Background()
	ref := orchapi.RunRef{IssueID: run.IssueID, RunID: run.RunID}
	_, err := m.api.AppendEvent(ctx, ref, &orchapi.Event{Type: "status", Name: string(model.StatusCanceled)})
	return err
}

func (m *Monitor) StartRun(issueID string, agentType string) (string, error) {
	args := append([]string{}, m.globalFlags...)
	args = append(args, "run", issueID)
	if agentType != "" {
		agentName, model, variant, profile := m.parseAgentPreset(agentType)
		args = append(args, "--agent", agentName)
		if model != "" {
			args = append(args, "--model", model)
		}
		if variant != "" {
			args = append(args, "--model-variant", variant)
		}
		if profile != "" {
			args = append(args, "--profile", profile)
		}
	}

	cmd := exec.Command(m.orchPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	output := strings.TrimSpace(strings.TrimSpace(stdout.String()) + "\n" + strings.TrimSpace(stderr.String()))
	if err != nil {
		if strings.TrimSpace(output) == "" {
			output = err.Error()
		}
		return output, err
	}
	if strings.TrimSpace(output) == "" {
		output = "run started"
	}
	return output, nil
}

func (m *Monitor) parseAgentPreset(agentType string) (agentName, model, variant, profile string) {
	idx := strings.Index(agentType, ":")
	if idx == -1 {
		return agentType, "", "", ""
	}

	agentName = agentType[:idx]
	presetName := agentType[idx+1:]

	for _, preset := range m.presets {
		if preset.Name == presetName && preset.EffectiveBackend() == agentName {
			return agentName, preset.Model, preset.Variant, preset.Profile
		}
	}

	return agentName, "", presetName, ""
}

func (m *Monitor) GetAvailableAgents() []string {
	agents := []string{
		string(agent.AgentClaude),
		string(agent.AgentCodex),
		string(agent.AgentGemini),
		string(agent.AgentOpenCode),
		string(agent.AgentCustom),
	}

	available := make([]string, 0, len(agents))
	for _, agentName := range agents {
		aType, err := agent.ParseAgentType(agentName)
		if err != nil {
			continue
		}
		adapter, err := agent.GetAdapter(aType)
		if err != nil {
			continue
		}
		if adapter.IsAvailable() {
			available = append(available, agentName)
			for _, preset := range m.presets {
				if preset.EffectiveBackend() == agentName {
					available = append(available, agentName+":"+preset.Name)
				}
			}
		}
	}

	return available
}

// OpenIssue opens an issue via orch open.
func (m *Monitor) OpenIssue(issueID string) (string, error) {
	args := append([]string{}, m.globalFlags...)
	args = append(args, "open", issueID)

	cmd := exec.Command(m.orchPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	output := strings.TrimSpace(strings.TrimSpace(stdout.String()) + "\n" + strings.TrimSpace(stderr.String()))
	if err != nil {
		if strings.TrimSpace(output) == "" {
			output = err.Error()
		}
		return output, err
	}
	if strings.TrimSpace(output) == "" {
		output = fmt.Sprintf("opened %s", issueID)
	}
	return output, nil
}

// CreateIssue creates a new issue via orch issue create.
func (m *Monitor) CreateIssue(issueID, title string) (string, error) {
	args := append([]string{}, m.globalFlags...)
	args = append(args, "issue", "create", issueID)
	if strings.TrimSpace(title) != "" {
		args = append(args, "--title", title)
	}

	cmd := exec.Command(m.orchPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	output := strings.TrimSpace(strings.TrimSpace(stdout.String()) + "\n" + strings.TrimSpace(stderr.String()))
	if err != nil {
		if strings.TrimSpace(output) == "" {
			output = err.Error()
		}
		return output, err
	}
	if strings.TrimSpace(output) == "" {
		output = fmt.Sprintf("created issue %s", issueID)
	}
	return output, nil
}

func (m *Monitor) SetIssueStatus(issueID string, status model.IssueStatus) error {
	ctx := context.Background()
	return m.api.SetIssueStatus(ctx, issueID, orchapi.IssueStatus(status))
}

func (m *Monitor) ResolveRun(run *model.Run) error {
	if run == nil {
		return fmt.Errorf("run not found")
	}

	ctx := context.Background()
	ref := orchapi.RunRef{IssueID: run.IssueID, RunID: run.RunID}

	if !run.Status.IsTerminal() {
		if _, err := m.api.AppendEvent(ctx, ref, &orchapi.Event{Type: "status", Name: string(model.StatusDone)}); err != nil {
			return fmt.Errorf("failed to mark run as done: %w", err)
		}
	}

	if err := m.api.SetIssueStatus(ctx, run.IssueID, orchapi.IssueStatusResolved); err != nil {
		return fmt.Errorf("failed to resolve issue: %w", err)
	}

	return nil
}

func (m *Monitor) ListIssues() ([]*model.Issue, error) {
	ctx := context.Background()
	result, err := m.api.ListIssues(ctx, nil)
	if err != nil {
		return nil, err
	}
	return apiIssuesToModel(result.Issues), nil
}

func (m *Monitor) ListRunsForIssue(issueID string) ([]*model.Run, error) {
	if strings.TrimSpace(issueID) == "" {
		return nil, fmt.Errorf("issue id is required")
	}
	ctx := context.Background()
	result, err := m.api.ListRuns(ctx, &orchapi.ListRunsFilter{IssueID: issueID})
	if err != nil {
		return nil, err
	}
	runs := apiRunsToModel(result.Runs)
	sortRuns(runs, m.runSort)
	return runs, nil
}

// ListBranchesForIssue returns branches for an issue derived from run data.
// Uses daemon API to get runs and extracts branch info from them.
func (m *Monitor) ListBranchesForIssue(issueID string) ([]branchInfo, error) {
	if strings.TrimSpace(issueID) == "" {
		return nil, fmt.Errorf("issue id is required")
	}

	ctx := context.Background()
	result, err := m.api.ListRuns(ctx, &orchapi.ListRunsFilter{IssueID: issueID})
	if err != nil {
		return nil, fmt.Errorf("failed to list runs: %w", err)
	}

	seen := make(map[string]bool)
	var branches []branchInfo
	for _, r := range result.Runs {
		branch := strings.TrimSpace(r.Branch)
		if branch == "" || seen[branch] {
			continue
		}
		seen[branch] = true
		branches = append(branches, branchInfo{
			name:       branch,
			commitTime: r.UpdatedAt,
		})
	}

	sort.Slice(branches, func(i, j int) bool {
		return branches[i].commitTime.After(branches[j].commitTime)
	})
	return branches, nil
}

func (m *Monitor) ContinueRun(issueID, branch, agentType, prompt string) (string, error) {
	args := append([]string{}, m.globalFlags...)
	args = append(args, "restart-from", "--issue", issueID, "--branch", branch)
	if agentType != "" {
		agentName, model, variant, profile := m.parseAgentPreset(agentType)
		args = append(args, "--agent", agentName)
		if model != "" {
			args = append(args, "--model", model)
		}
		if variant != "" {
			args = append(args, "--model-variant", variant)
		}
		if profile != "" {
			args = append(args, "--profile", profile)
		}
	}

	cmd := exec.Command(m.orchPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// If prompt is provided, we need to inject it after the run starts
	// For now, we'll pass it via environment variable or stdin
	// The restart-from command doesn't have a --prompt flag, so we'll handle this differently
	if prompt != "" {
		// Store the prompt to be injected via tmux after the session starts
		cmd.Env = append(os.Environ(), fmt.Sprintf("ORCH_CONTINUE_PROMPT=%s", prompt))
	}

	err := cmd.Run()

	output := strings.TrimSpace(strings.TrimSpace(stdout.String()) + "\n" + strings.TrimSpace(stderr.String()))
	if err != nil {
		if strings.TrimSpace(output) == "" {
			output = err.Error()
		}
		return output, err
	}
	if strings.TrimSpace(output) == "" {
		output = "run restarted"
	}
	return output, nil
}

func (m *Monitor) createSession() error {
	cmd := m.runsDashboardCommand()
	cfg := &multiplexer.SessionConfig{
		SessionName: m.session,
		Command:     cmd,
		WindowName:  dashboardWindowName,
	}
	if err := m.mux.NewSession(cfg); err != nil {
		return fmt.Errorf("failed to create monitor session: %w", err)
	}
	return nil
}

func (m *Monitor) attachSession() error {
	if m.mux.IsInsideSession() {
		return m.mux.SwitchClient(m.session)
	}
	return m.mux.AttachSession(m.session)
}

func (m *Monitor) ensurePaneLayout() error {
	if !m.mux.HasSession(m.session) {
		return nil
	}

	target := fmt.Sprintf("%s:%d", m.session, dashboardWindowIdx)
	panes, err := m.mux.ListPanes(target)
	if err != nil {
		return err
	}

	if hasPaneLayout(panes) {
		m.syncPaneOptions(panes)
		return nil
	}

	if len(panes) > 0 {
		base := panes[0]
		for _, p := range panes {
			if p.Index < base.Index {
				base = p
			}
		}
		for _, p := range panes {
			if p.ID != base.ID {
				_ = m.mux.KillPane(p.ID)
			}
		}
		_ = m.mux.SetPaneTitle(base.ID, runsPaneTitle)
		if chatPane, err := m.mux.SplitWindow(base.ID, true, 25); err == nil {
			_ = m.mux.SetPaneTitle(chatPane, chatPaneTitle)
			launch := m.agentChatLaunch()
			_ = m.mux.SendKeys(chatPane, launch.command)
			m.sendAgentChatPrompt(chatPane, launch)
			_ = m.mux.SetOption(m.session, chatPaneOption, chatPane)
		} else {
			return err
		}
		if issuesPane, err := m.mux.SplitWindow(base.ID, false, 0); err == nil {
			_ = m.mux.SetPaneTitle(issuesPane, issuesPaneTitle)
			_ = m.mux.SendKeys(issuesPane, m.issuesDashboardCommand())
			_ = m.mux.SetOption(m.session, issuesPaneOption, issuesPane)
		} else {
			return err
		}
		_ = m.mux.SetOption(m.session, runsPaneOption, base.ID)
		return nil
	}

	return fmt.Errorf("failed to initialize panes")
}

func (m *Monitor) loadRuns() ([]*RunWindow, error) {
	if len(m.runFilter.Statuses) == 0 {
		return []*RunWindow{}, nil
	}

	filter := &orchapi.ListRunsFilter{
		Limit: 100,
	}
	filter.Status = statusSliceAPI(m.runFilter.Statuses)
	if !m.runFilter.IsDefault() {
		filter.Limit = 0
	}

	ctx := context.Background()
	result, err := m.api.ListRuns(ctx, filter)
	if err != nil {
		return nil, err
	}
	runs := apiRunsToModel(result.Runs)

	runWindows := make([]*RunWindow, 0, len(runs))
	for i, run := range runs {
		sessionName := run.SessionName
		if sessionName == "" {
			sessionName = model.GenerateSessionName(run.IssueID, run.RunID)
		}
		runWindows = append(runWindows, &RunWindow{
			Index:        i + 1,
			Run:          run,
			AgentSession: sessionName,
		})
	}

	return runWindows, nil
}

func (m *Monitor) ensureRunSession(w *RunWindow) error {
	if m.mux.HasSession(w.AgentSession) {
		return nil
	}
	if w.Run.WorktreePath == "" {
		return nil
	}
	return m.mux.NewSession(&multiplexer.SessionConfig{
		SessionName: w.AgentSession,
		WorkDir:     w.Run.WorktreePath,
	})
}

func (m *Monitor) buildRunRows(windows []*RunWindow) ([]RunRow, error) {
	issueInfo := make(map[string]issueDisplay)
	ctx := context.Background()
	if issuesResult, err := m.api.ListIssues(ctx, nil); err == nil {
		issueInfo = buildIssueDisplayMap(apiIssuesToModel(issuesResult.Issues))
	}

	rows := make([]RunRow, 0, len(windows))
	for _, w := range windows {
		if w == nil || w.Run == nil {
			continue
		}
		info := issueInfo[w.Run.IssueID]
		issueStatus := info.status
		if issueStatus == "" {
			issueStatus = "-"
		}
		topic := info.topic
		if topic == "" {
			topic = "-"
		}

		agentDisplay := agent.AgentDisplayName(w.Run.Agent, w.Run.Model, w.Run.ModelVariant)

		prDisplay := "-"
		prState := ""
		if w.Run.PRNumber > 0 {
			prDisplay = fmt.Sprintf("#%d", w.Run.PRNumber)
			prState = w.Run.PRState
		} else if w.Run.PRUrl != "" || w.Run.Status == model.StatusPROpen {
			prDisplay = "yes"
		}

		merged := w.Run.BranchState
		if merged == "" {
			merged = "-"
		}
		shortID := w.Run.ShortID()
		if w.Run.WorktreePath != "" && !w.Run.WorktreeExists {
			shortID += "*"
		}
		branch := formatBranchDisplay(w.Run.Branch, runTableBranchWidth)
		worktree := formatWorktreeDisplay(w.Run.WorktreePath, runTableWorktreeWidth)

		modelDisplay := agent.ModelDisplayName(w.Run.Model, w.Run.ModelVariant)

		rows = append(rows, RunRow{
			Index:        w.Index,
			ShortID:      shortID,
			IssueID:      w.Run.IssueID,
			IssueStatus:  issueStatus,
			IssueSummary: info.summary,
			Agent:        agentDisplay,
			Model:        modelDisplay,
			Status:       w.Run.Status,
			Alive:        runAliveLabel(w.Run),
			Branch:       branch,
			Worktree:     worktree,
			PR:           prDisplay,
			PRState:      prState,
			Merged:       merged,
			Started:      w.Run.StartedAt,
			Updated:      w.Run.UpdatedAt,
			Topic:        topic,
			Run:          w.Run,
		})
	}

	sortRunRowsWithDirection(rows, m.runSort, m.runSortDir)
	return rows, nil
}

func buildIssueDisplayMap(issues []*model.Issue) map[string]issueDisplay {
	result := make(map[string]issueDisplay, len(issues))
	for _, issue := range issues {
		if issue == nil || strings.TrimSpace(issue.ID) == "" {
			continue
		}

		status := string(issue.Status)
		if status == "" {
			status = "-"
		}
		topic := formatIssueTopic(issue)
		if topic == "" {
			topic = "-"
		}
		summary := issue.Summary
		if summary == "" {
			summary = "-"
		}

		result[issue.ID] = issueDisplay{
			status:  status,
			topic:   topic,
			summary: summary,
		}
	}
	return result
}

func runAliveLabel(run *model.Run) string {
	if run == nil {
		return "-"
	}
	if !run.AliveKnown {
		return "-"
	}
	if run.Alive {
		return "yes"
	}
	return "no"
}

func (m *Monitor) buildIssueRows(issues []*model.Issue, runs []*model.Run) []IssueRow {
	runsByIssue := make(map[string][]*model.Run)
	for _, run := range runs {
		runsByIssue[run.IssueID] = append(runsByIssue[run.IssueID], run)
	}

	rows := make([]IssueRow, 0, len(issues))
	for i, issue := range issues {
		status := string(issue.Status)
		if status == "" {
			status = string(model.IssueStatusOpen)
		}

		summary := issue.Summary
		if strings.TrimSpace(summary) == "" {
			summary = issue.Title
		}
		if strings.TrimSpace(summary) == "" {
			summary = "-"
		}

		var latest *model.Run
		activeCount := 0
		for _, run := range runsByIssue[issue.ID] {
			if latest == nil || run.UpdatedAt.After(latest.UpdatedAt) {
				latest = run
			}
			if run.Status.IsActive() {
				activeCount++
			}
		}

		row := IssueRow{
			Index:      i + 1,
			ID:         issue.ID,
			Status:     status,
			Summary:    summary,
			ActiveRuns: activeCount,
			Issue:      issue,
		}
		if latest != nil {
			row.LatestRunID = latest.RunID
			row.LatestStatus = latest.Status
			row.LatestUpdated = latest.UpdatedAt
		}
		rows = append(rows, row)
	}

	sortIssueRowsWithDirection(rows, m.issueSort, m.issueSortDir)
	return rows
}

func (m *Monitor) runsDashboardCommand() string {
	args := append([]string{m.orchPath}, m.globalFlags...)
	args = append(args, "monitor", "--dashboard")
	if m.runFilter.IssueQuery != "" {
		args = append(args, "--issue", m.runFilter.IssueQuery)
	}
	for _, status := range statusSlice(m.runFilter.Statuses) {
		args = append(args, "--status", string(status))
	}
	if m.runSort != "" {
		args = append(args, "--sort-runs", string(m.runSort))
	}
	if m.issueSort != "" {
		args = append(args, "--sort-issues", string(m.issueSort))
	}
	return shellJoin(args)
}

func (m *Monitor) issuesDashboardCommand() string {
	args := append([]string{m.orchPath}, m.globalFlags...)
	args = append(args, "monitor", "--issues-dashboard")
	if m.runSort != "" {
		args = append(args, "--sort-runs", string(m.runSort))
	}
	if m.issueSort != "" {
		args = append(args, "--sort-issues", string(m.issueSort))
	}
	// Pass filter settings from persisted UI settings
	if m.showResolved {
		args = append(args, "--show-resolved")
	}
	if !m.showClosed {
		args = append(args, "--show-closed=false")
	}
	return shellJoin(args)
}

type agentChatLaunch struct {
	command        string
	prompt         string
	promptEmbedded bool
	injection      agent.InjectionMethod
	readyPattern   string
	port           int
	model          string
	modelVariant   string
}

func (m *Monitor) agentChatLaunch() agentChatLaunch {
	ctx := context.Background()
	_, err := WriteControlPromptFileViaAPI(ctx, m.api, m.issuesRoot)
	if err != nil {
		return agentChatLaunch{command: fallbackChatCommand(fmt.Sprintf("failed to write prompt file: %v", err))}
	}

	prompt := GetControlPromptInstruction()

	agentName := strings.TrimSpace(m.agent)
	var modelName, modelVariant string
	cfg, cfgErr := config.Load()
	if cfgErr == nil {
		if agentName == "" {
			agentName = cfg.ControlAgent
			if agentName == "" {
				agentName = cfg.Agent
			}
		}
		modelName = cfg.ControlModel
		if modelName == "" {
			modelName = cfg.Model
		}
		modelVariant = cfg.ControlModelVariant
		if modelVariant == "" {
			modelVariant = cfg.ModelVariant
		}
	}
	if agentName == "" {
		agentName = "opencode"
	}
	aType, err := agent.ParseAgentType(agentName)
	if err != nil {
		return agentChatLaunch{command: fallbackChatCommand(err.Error())}
	}
	adapter, err := agent.GetAdapter(aType)
	if err != nil {
		return agentChatLaunch{command: fallbackChatCommand(err.Error())}
	}
	if !adapter.IsAvailable() {
		return agentChatLaunch{command: fallbackChatCommand(fmt.Sprintf("%s CLI not available", agentName))}
	}

	if adapter.PromptInjection() == agent.InjectionHTTP {
		if err := m.api.Ping(ctx); err != nil {
			return agentChatLaunch{command: fallbackChatCommand("daemon not running; opencode requires daemon")}
		}
		resp, err := m.api.EnsureOpenCodeServer(ctx, m.projectRoot)
		if err != nil {
			m.logger.Printf("daemon server request failed: %v", err)
			return agentChatLaunch{command: fallbackChatCommand(fmt.Sprintf("daemon server error: %v", err))}
		}

		m.logger.Printf("using daemon-managed opencode server on port %d", resp.Port)
		attachCmd := fmt.Sprintf("opencode attach http://127.0.0.1:%d", resp.Port)
		return agentChatLaunch{
			command:        attachCmd,
			prompt:         prompt,
			promptEmbedded: false,
			injection:      agent.InjectionHTTP,
			readyPattern:   "",
			port:           resp.Port,
			model:          modelName,
			modelVariant:   modelVariant,
		}
	}

	cmd, err := adapter.LaunchCommand(&agent.LaunchConfig{
		Type:            aType,
		IssuesRoot:      m.issuesRoot,
		Prompt:          prompt,
		ContinueSession: true,
		Port:            agent.OpenCodeServerPortStart,
		Model:           modelName,
		ModelVariant:    modelVariant,
	})
	if err != nil {
		return agentChatLaunch{command: fallbackChatCommand(err.Error())}
	}

	return agentChatLaunch{
		command:        cmd,
		prompt:         prompt,
		promptEmbedded: true && prompt != "",
		injection:      adapter.PromptInjection(),
		readyPattern:   adapter.ReadyPattern(),
		port:           agent.OpenCodeServerPortStart,
		model:          modelName,
		modelVariant:   modelVariant,
	}
}

func (m *Monitor) sendAgentChatPrompt(pane string, launch agentChatLaunch) {
	if launch.prompt == "" {
		return
	}
	if launch.promptEmbedded {
		return
	}

	switch launch.injection {
	case agent.InjectionTmux:
		paneID := pane
		prompt := launch.prompt
		pattern := launch.readyPattern
		mux := m.mux
		go func() {
			if pattern != "" {
				_ = mux.WaitForReady(paneID, pattern, 30*time.Second)
			}
			_ = mux.SendKeys(paneID, prompt)
		}()

	case agent.InjectionHTTP:
		go m.sendPromptViaHTTP(launch)
	}
}

func (m *Monitor) sendPromptViaHTTP(launch agentChatLaunch) {
	port := launch.port
	if port == 0 {
		port = agent.OpenCodeServerPortStart
	}

	client := agent.NewOpenCodeClient(port)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := client.WaitForHealthy(ctx, 60*time.Second); err != nil {
		return
	}

	sessionID := m.getOrCreateControlSession(ctx, client, port)
	if sessionID == "" {
		return
	}

	var modelRef *agent.ModelRef
	if launch.model != "" {
		modelRef = agent.ParseModel(launch.model)
	}

	_ = client.SendMessageAsync(ctx, sessionID, launch.prompt, m.issuesRoot, modelRef, launch.modelVariant)
}

func (m *Monitor) getOrCreateControlSession(ctx context.Context, client *agent.OpenCodeClient, port int) string {
	stored := LoadControlSession(m.orchDir)
	if stored != nil && stored.SessionID != "" {
		session, err := client.GetSession(ctx, stored.SessionID, m.issuesRoot)
		if err == nil && session != nil {
			if stored.Port != port {
				_ = SaveControlSession(m.orchDir, &ControlSession{
					SessionID: stored.SessionID,
					Port:      port,
				})
			}
			return stored.SessionID
		}
	}

	session, err := client.CreateSession(ctx, "monitor-chat", m.issuesRoot)
	if err != nil {
		return ""
	}

	_ = SaveControlSession(m.orchDir, &ControlSession{
		SessionID: session.ID,
		Port:      port,
	})

	return session.ID
}

func defaultStatuses() []model.Status {
	return []model.Status{
		model.StatusRunning,
		model.StatusWaiting,
		model.StatusRateLimited,
		model.StatusBooting,
		model.StatusQueued,
		model.StatusPROpen,
		model.StatusUnknown,
	}
}

func shellJoin(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"\\$&;|<>*?[]{}()!") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func resolveOrchPath(path string) string {
	if path != "" && filepath.IsAbs(path) {
		return path
	}
	execPath, err := os.Executable()
	if err == nil && execPath != "" {
		if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
			execPath = resolved
		}
		return execPath
	}
	if path != "" {
		if abs, err := filepath.Abs(path); err == nil {
			return abs
		}
	}
	if path != "" {
		return path
	}
	return os.Args[0]
}

func (m *Monitor) selectPaneByTitle(title string) error {
	pane, err := m.findPaneByTitle(m.session, title)
	if err != nil {
		return err
	}
	return m.mux.SelectPane(pane)
}

func (m *Monitor) selectPaneByOption(option, fallbackTitle string) error {
	pane, err := m.findPaneByOption(option)
	if err == nil {
		return m.mux.SelectPane(pane)
	}
	return m.selectPaneByTitle(fallbackTitle)
}

func (m *Monitor) findChatPane() (string, error) {
	if pane, err := m.findPaneByOption(chatPaneOption); err == nil {
		return pane, nil
	}
	target := fmt.Sprintf("%s:%d", m.session, dashboardWindowIdx)
	panes, err := m.mux.ListPanes(target)
	if err != nil {
		return "", err
	}
	if len(panes) == 0 {
		return "", fmt.Errorf("no panes found in %s", target)
	}
	for _, pane := range panes {
		if pane.Title == chatPaneTitle {
			return pane.ID, nil
		}
	}
	for _, pane := range panes {
		if pane.Title != runsPaneTitle && pane.Title != issuesPaneTitle {
			return pane.ID, nil
		}
	}
	return "", fmt.Errorf("pane not found: %s", chatPaneTitle)
}

func (m *Monitor) findPaneByOption(option string) (string, error) {
	value, err := m.mux.GetOption(m.session, option)
	if err == nil && value != "" {
		if m.paneExists(value) {
			return value, nil
		}
	}
	return "", fmt.Errorf("pane not found for option: %s", option)
}

func (m *Monitor) findPaneByTitle(session, title string) (string, error) {
	window := dashboardWindowIdx
	if session != m.session {
		window = 0
	}
	target := fmt.Sprintf("%s:%d", session, window)
	panes, err := m.mux.ListPanes(target)
	if err != nil {
		return "", err
	}
	if title == "" {
		if len(panes) == 0 {
			return "", fmt.Errorf("no panes found in %s", target)
		}
		return panes[0].ID, nil
	}
	for _, pane := range panes {
		if pane.Title == title {
			return pane.ID, nil
		}
	}
	return "", fmt.Errorf("pane not found: %s", title)
}

func hasPaneLayout(panes []multiplexer.Pane) bool {
	if len(panes) != 3 {
		return false
	}
	foundRuns := false
	foundIssues := false
	for _, pane := range panes {
		if pane.Title == runsPaneTitle {
			foundRuns = true
		}
		if pane.Title == issuesPaneTitle {
			foundIssues = true
		}
	}
	return foundRuns && foundIssues
}

func (m *Monitor) paneExists(id string) bool {
	target := fmt.Sprintf("%s:%d", m.session, dashboardWindowIdx)
	panes, err := m.mux.ListPanes(target)
	if err != nil {
		return false
	}
	for _, pane := range panes {
		if pane.ID == id {
			return true
		}
	}
	return false
}

func (m *Monitor) syncPaneOptions(panes []multiplexer.Pane) {
	var runsID, issuesID, chatID string
	for _, pane := range panes {
		switch pane.Title {
		case runsPaneTitle:
			runsID = pane.ID
		case issuesPaneTitle:
			issuesID = pane.ID
		}
	}
	for _, pane := range panes {
		if pane.ID != runsID && pane.ID != issuesID {
			chatID = pane.ID
			break
		}
	}
	if runsID != "" {
		_ = m.mux.SetOption(m.session, runsPaneOption, runsID)
	}
	if issuesID != "" {
		_ = m.mux.SetOption(m.session, issuesPaneOption, issuesID)
	}
	if chatID != "" {
		_ = m.mux.SetOption(m.session, chatPaneOption, chatID)
	}
}

func (m *Monitor) ensureChatPaneTitle(panes []multiplexer.Pane) {
	var runsID, issuesID string
	for _, pane := range panes {
		switch pane.Title {
		case runsPaneTitle:
			runsID = pane.ID
		case issuesPaneTitle:
			issuesID = pane.ID
		}
	}
	for _, pane := range panes {
		if pane.ID == runsID || pane.ID == issuesID {
			continue
		}
		if pane.Title != chatPaneTitle {
			_ = m.mux.SetPaneTitle(pane.ID, chatPaneTitle)
		}
		return
	}
}

func (m *Monitor) refreshChatPaneTitle() {
	target := fmt.Sprintf("%s:%d", m.session, dashboardWindowIdx)
	panes, err := m.mux.ListPanes(target)
	if err != nil {
		return
	}
	m.ensureChatPaneTitle(panes)
}

func (m *Monitor) repairSwappedMonitorChat() error {
	target := fmt.Sprintf("%s:%d", m.session, dashboardWindowIdx)
	panes, err := m.mux.ListPanes(target)
	if err != nil {
		return err
	}
	if len(panes) == 0 {
		return nil
	}

	var runsID, issuesID string
	var chatPane multiplexer.Pane
	for _, pane := range panes {
		switch pane.Title {
		case runsPaneTitle:
			runsID = pane.ID
		case issuesPaneTitle:
			issuesID = pane.ID
		}
	}
	for _, pane := range panes {
		if pane.ID != runsID && pane.ID != issuesID {
			chatPane = pane
			break
		}
	}
	if chatPane.ID == "" || chatPane.Title == chatPaneTitle {
		return nil
	}

	ref, err := model.ParseRunRef(chatPane.Title)
	if err != nil || ref.RunID == "" {
		_ = m.mux.SetPaneTitle(chatPane.ID, chatPaneTitle)
		return nil
	}
	ctx := context.Background()
	apiRun, err := m.api.GetRun(ctx, ref.IssueID, ref.RunID)
	if err != nil {
		_ = m.mux.SetPaneTitle(chatPane.ID, chatPaneTitle)
		return nil
	}
	run := apiRunToModel(apiRun)
	sessionName := run.SessionName
	if sessionName == "" {
		sessionName = model.GenerateSessionName(run.IssueID, run.RunID)
	}
	if !m.mux.HasSession(sessionName) {
		_ = m.mux.SetPaneTitle(chatPane.ID, chatPaneTitle)
		return nil
	}
	if err := m.repairSwappedRunSession(run, sessionName); err != nil {
		return err
	}
	return nil
}

func (m *Monitor) repairSwappedRunSession(run *model.Run, sessionName string) error {
	if run == nil {
		return nil
	}
	target := fmt.Sprintf("%s:%d", sessionName, 0)
	panes, err := m.mux.ListPanes(target)
	if err != nil {
		return err
	}
	if len(panes) == 0 {
		return nil
	}
	runPane := panes[0]
	if runPane.Title != chatPaneTitle {
		return nil
	}

	chatPaneID, err := m.findChatPane()
	if err != nil {
		return nil
	}
	monitorTarget := fmt.Sprintf("%s:%d", m.session, dashboardWindowIdx)
	monitorPanes, err := m.mux.ListPanes(monitorTarget)
	if err != nil {
		return err
	}
	var monitorChatPane multiplexer.Pane
	for _, pane := range monitorPanes {
		if pane.ID == chatPaneID {
			monitorChatPane = pane
			break
		}
	}
	if monitorChatPane.ID == "" || monitorChatPane.Title == chatPaneTitle {
		return nil
	}
	if err := m.mux.SwapPane(runPane.ID, monitorChatPane.ID); err != nil {
		return err
	}
	_ = m.mux.SetPaneTitle(monitorChatPane.ID, chatPaneTitle)
	_ = m.mux.SetPaneTitle(runPane.ID, run.Ref().String())
	return nil
}

func (m *Monitor) resolveRunWindowID(run *model.Run, sessionName string) (string, error) {
	windows, err := m.mux.ListWindows(sessionName)
	if err != nil {
		return "", err
	}
	if run != nil && run.MuxWindowID != "" {
		if _, ok := windowIndexByID(windows, run.MuxWindowID); ok {
			return run.MuxWindowID, nil
		}
	}
	for _, window := range windows {
		if window.Index == 0 {
			return window.ID, nil
		}
	}
	if len(windows) > 0 {
		return windows[0].ID, nil
	}
	return "", nil
}

func windowIndexByID(windows []multiplexer.Window, id string) (int, bool) {
	for _, window := range windows {
		if window.ID == id {
			return window.Index, true
		}
	}
	return 0, false
}

func nextAvailableWindowIndex(windows []multiplexer.Window, start int) int {
	used := make(map[int]bool, len(windows))
	for _, window := range windows {
		used[window.Index] = true
	}
	for idx := start; ; idx++ {
		if !used[idx] {
			return idx
		}
	}
}

func (m *Monitor) registerWithDaemon() {
	ctx := context.Background()
	if err := m.api.Ping(ctx); err != nil {
		return
	}
	registerAPI, ok := m.api.(interface {
		RegisterMonitor(context.Context, int, string, string, string, string) (*orchapi.MonitorRegistration, error)
	})
	if !ok {
		return
	}

	resp, err := registerAPI.RegisterMonitor(
		ctx,
		os.Getpid(),
		"go",
		"dashboard",
		m.projectRoot,
		m.session,
	)
	if err != nil {
		return
	}

	m.monitorID = resp.MonitorID
	m.startHeartbeat()
}

func (m *Monitor) startHeartbeat() {
	if m.monitorID == "" {
		return
	}

	m.heartbeatStop = make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-m.heartbeatStop:
				return
			case <-ticker.C:
				if m.monitorID != "" {
					heartbeatAPI, ok := m.api.(interface {
						MonitorHeartbeat(context.Context, string) error
					})
					if !ok {
						return
					}
					err := heartbeatAPI.MonitorHeartbeat(context.Background(), m.monitorID)
					if err != nil && strings.Contains(err.Error(), "not_found") {
						m.reregisterWithDaemon()
					}
				}
			}
		}
	}()
}

func (m *Monitor) reregisterWithDaemon() {
	ctx := context.Background()
	if err := m.api.Ping(ctx); err != nil {
		return
	}
	registerAPI, ok := m.api.(interface {
		RegisterMonitor(context.Context, int, string, string, string, string) (*orchapi.MonitorRegistration, error)
	})
	if !ok {
		return
	}

	resp, err := registerAPI.RegisterMonitor(
		ctx,
		os.Getpid(),
		"go",
		"dashboard",
		m.projectRoot,
		m.session,
	)
	if err != nil {
		return
	}

	m.monitorID = resp.MonitorID
}

func (m *Monitor) unregisterFromDaemon() {
	if m.heartbeatStop != nil {
		close(m.heartbeatStop)
		m.heartbeatStop = nil
	}

	if m.monitorID == "" {
		return
	}
	unregisterAPI, ok := m.api.(interface {
		UnregisterMonitor(context.Context, string) error
	})
	if !ok {
		return
	}

	_ = unregisterAPI.UnregisterMonitor(context.Background(), m.monitorID)
	m.monitorID = ""
}

func apiIssuesToModel(issues []*orchapi.Issue) []*model.Issue {
	result := make([]*model.Issue, 0, len(issues))
	for _, i := range issues {
		result = append(result, &model.Issue{
			ID:          i.ID,
			Title:       i.Title,
			Topic:       i.Topic,
			Summary:     i.Summary,
			Status:      model.IssueStatus(i.Status),
			Tags:        i.Tags,
			Body:        i.Body,
			Path:        i.Path,
			Frontmatter: i.Frontmatter,
			ModifiedAt:  i.ModifiedAt,
		})
	}
	return result
}

func apiRunsToModel(runs []*orchapi.Run) []*model.Run {
	result := make([]*model.Run, 0, len(runs))
	for _, r := range runs {
		result = append(result, apiRunToModel(r))
	}
	return result
}

func apiRunToModel(r *orchapi.Run) *model.Run {
	if r == nil {
		return nil
	}
	return &model.Run{
		IssueID:           r.IssueID,
		RunID:             r.RunID,
		Status:            model.NormalizeStatus(string(r.Status)),
		Agent:             r.Agent,
		Model:             r.Model,
		ModelVariant:      r.ModelVariant,
		Branch:            r.Branch,
		WorktreePath:      r.WorktreePath,
		SessionName:       r.SessionName,
		Multiplexer:       string(r.Multiplexer),
		PRUrl:             r.PRUrl,
		PRNumber:          r.PRNumber,
		PRState:           r.PRState,
		ServerPort:        r.ServerPort,
		OpenCodeSessionID: r.OpenCodeSessionID,
		ContinuedFrom:     r.ContinuedFrom,
		StartedAt:         r.StartedAt,
		UpdatedAt:         r.UpdatedAt,
		BranchState:       string(r.BranchState),
		Alive:             r.Alive,
		AliveKnown:        r.AliveKnown,
		WorktreeExists:    r.WorktreeExists,
	}
}

func apiIssueToModel(i *orchapi.Issue) *model.Issue {
	if i == nil {
		return nil
	}
	return &model.Issue{
		ID:          i.ID,
		Title:       i.Title,
		Topic:       i.Topic,
		Summary:     i.Summary,
		Status:      model.IssueStatus(i.Status),
		Tags:        i.Tags,
		Body:        i.Body,
		Path:        i.Path,
		Frontmatter: i.Frontmatter,
		ModifiedAt:  i.ModifiedAt,
	}
}

func statusSliceAPI(set map[model.Status]bool) []orchapi.RunStatus {
	result := make([]orchapi.RunStatus, 0, len(set))
	for s := range set {
		result = append(result, orchapi.NormalizeRunStatus(string(s)))
	}
	return result
}
