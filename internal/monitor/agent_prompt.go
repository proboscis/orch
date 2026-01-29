package monitor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/orchapi"
	"github.com/s22625/orch/internal/store"
)

const (
	controlPromptFileName        = "ORCH_CONTROL_PROMPT.md"
	controlPromptFileInstruction = "ultrathink Please read '" + controlPromptFileName + "' in the current directory and follow the instructions found there."
)

// controlPromptTemplate is the template for the control agent prompt
const controlPromptTemplate = `You are the orch control agent for this repository.
You can run orch commands directly via bash to manage issues and runs.

## Repository Context

- IssuesRoot: {{.IssuesRoot}}
- Working directory: {{.WorkDir}}

## Git Context
{{if .GitBranch}}
- Current branch: {{.GitBranch}}
{{- end}}
- Uncommitted changes: {{.UncommittedChanges}}
{{- if .LastCommitMessage}}
- Last commit: {{.LastCommitMessage}}
{{- end}}

## Available Agents

- Default: {{.DefaultAgent}}
- Configured backends: {{.AvailableAgents}}

Use ` + "`--agent <name>`" + ` to specify a different agent when starting runs.

## Issue ID Convention

This repository uses the following issue ID naming convention:
- Pattern: {{.IssueIDPattern}}
- Example: {{.IssueIDExample}}
- Next available ID: {{.NextIssueID}}

When creating new issues, always follow this naming convention.

## Existing Issues
{{if .Issues}}
| ID | Status | Title |
|----|--------|-------|
{{- range .Issues}}
| {{.ID}} | {{.Status}} | {{.Title}} |
{{- end}}
{{else}}
No issues found.
{{end}}

## Active Runs
{{if .ActiveRuns}}
| Issue | Run ID | Status |
|-------|--------|--------|
{{- range .ActiveRuns}}
| {{.IssueID}} | {{.ShortID}} | {{.Status}} |
{{- end}}
{{else}}
No active runs.
{{end}}

## Workflows

### Starting New Work
1. Create issue: ` + "`orch issue create <id> --title \"...\" --body \"...\"`" + `
2. Start run: ` + "`orch run <issue-id>`" + `
3. Monitor: Watch the runs table or use ` + "`orch ps`" + `

### Handling Blocked Runs
When a run shows "blocked" status:
1. Capture: ` + "`orch capture <run-ref>`" + ` to see what the agent needs
2. Send feedback: ` + "`orch send <issue-id> \"<message>\"`" + ` to provide input
3. The agent will resume automatically after receiving input

### Continuing Work
- From a branch: ` + "`orch continue <issue> --branch <branch-name>`" + `
- From a run: ` + "`orch continue <issue>#<run-id>`" + `

## Available Orch Commands

Run these commands directly using bash (do not use any special protocol):

### Issue Management
- Create issue: ` + "`orch issue create <id> --title \"<title>\" --body \"<body>\"`" + `
- List issues: ` + "`orch issue list`" + `
- Open issue in editor: ` + "`orch open <issue-id>`" + `

### Run Management
- Start a run: ` + "`orch run <issue-id>`" + `
- Continue from branch: ` + "`orch continue <issue> --branch <branch>`" + `
- List runs: ` + "`orch ps`" + ` (use ` + "`--status running,blocked`" + ` to filter)
- Stop a run: ` + "`orch stop <issue-id>#<run-id>`" + `
- Resolve a run: ` + "`orch resolve <issue-id>#<run-id>`" + `
- Show run details: ` + "`orch show <issue-id>#<run-id>`" + `
- Capture run state: ` + "`orch capture <run-ref>`" + ` - see agent's last message
- Send feedback: ` + "`orch send <issue-id> \"<message>\"`" + ` - provide input to blocked agent

### Interactive Commands (DO NOT USE)
The following commands are interactive and will hang if called by an AI agent:
- ` + "`orch attach`" + ` - interactive tmux session (for humans only)
- ` + "`orch monitor`" + ` - interactive TUI (for humans only)

## Troubleshooting

- Issue not showing in list: ` + "`orch validate-issue-files <issue-id>`" + ` to check for formatting errors
- Validate all issue files: ` + "`orch validate-issue-files`" + ` to find malformed issues
- Orphaned sessions: ` + "`orch repair`" + `
- View daemon logs: Check ` + "`.orch/daemon.log`" + ` in project root
- Force stop all: ` + "`orch stop --all`" + `

## Issue File Template

When creating issues, the file should follow this format:

` + "```markdown" + `
---
type: issue
id: <issue-id>
title: <title>
status: open
summary: <one-line summary>
---

# <title>

<detailed description>
` + "```" + `

## Instructions

- Execute orch commands directly via bash - no special protocol needed
- Follow the issue ID naming convention when creating new issues
- Check the existing issues list above to avoid duplicate IDs
- Use the next available ID ({{.NextIssueID}}) for new issues unless a specific ID is requested
{{if .ExtraPrompt}}

## Custom Instructions

{{.ExtraPrompt}}
{{end}}
`

// ControlPromptData contains data for the control agent prompt template
type ControlPromptData struct {
	IssuesRoot     string
	WorkDir        string
	IssueIDPattern string
	IssueIDExample string
	NextIssueID    string
	Issues         []IssueInfo
	ActiveRuns     []RunInfo

	GitBranch          string
	UncommittedChanges string
	LastCommitMessage  string

	DefaultAgent    string
	AvailableAgents string

	ExtraPrompt string
}

// IssueInfo contains minimal issue information for the prompt
type IssueInfo struct {
	ID     string
	Status string
	Title  string
}

// RunInfo contains minimal run information for the prompt
type RunInfo struct {
	IssueID string
	ShortID string
	Status  string
}

// buildControlAgentPrompt builds the control agent prompt with dynamic repo context
func buildControlAgentPrompt(st store.Store) (string, error) {
	cwd, _ := os.Getwd()
	issuesRoot := st.RootPath()

	// Get existing issues
	issues, err := st.ListIssues()
	if err != nil {
		issues = nil
	}

	// Get active runs
	runs, err := st.ListRuns(&store.ListRunsFilter{
		Status: []model.Status{
			model.StatusRunning,
			model.StatusBlocked,
			model.StatusBlockedAPI,
			model.StatusBooting,
			model.StatusQueued,
			model.StatusPROpen,
		},
		Limit: 20,
	})
	if err != nil {
		runs = nil
	}

	// Detect issue ID pattern from existing issues
	pattern, example, nextID := detectIssueIDConvention(issues)

	// Build issue info list
	issueInfos := make([]IssueInfo, 0, len(issues))
	for _, issue := range issues {
		status := string(issue.Status)
		if status == "" {
			status = string(model.IssueStatusOpen)
		}
		title := issue.Title
		if title == "" {
			title = "-"
		}
		// Truncate long titles
		if len(title) > 50 {
			title = title[:47] + "..."
		}
		issueInfos = append(issueInfos, IssueInfo{
			ID:     issue.ID,
			Status: status,
			Title:  title,
		})
	}

	// Build run info list
	runInfos := make([]RunInfo, 0, len(runs))
	for _, run := range runs {
		runInfos = append(runInfos, RunInfo{
			IssueID: run.IssueID,
			ShortID: run.ShortID(),
			Status:  string(run.Status),
		})
	}

	cfg, _ := config.Load()
	defaultAgent := "opencode"
	if cfg != nil {
		if cfg.ControlAgent != "" {
			defaultAgent = cfg.ControlAgent
		} else if cfg.Agent != "" {
			defaultAgent = cfg.Agent
		}
	}

	data := ControlPromptData{
		IssuesRoot:     issuesRoot,
		WorkDir:        cwd,
		IssueIDPattern: pattern,
		IssueIDExample: example,
		NextIssueID:    nextID,
		Issues:         issueInfos,
		ActiveRuns:     runInfos,

		GitBranch:          getGitBranch(cwd),
		UncommittedChanges: getUncommittedChangesStatus(cwd),
		LastCommitMessage:  getLastCommitMessage(cwd),

		DefaultAgent:    defaultAgent,
		AvailableAgents: getAvailableAgents(),

		ExtraPrompt: loadExtraPrompt(),
	}

	tmpl, err := template.New("control-prompt").Parse(controlPromptTemplate)
	if err != nil {
		return buildFallbackControlPrompt(issuesRoot, cwd), nil
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return buildFallbackControlPrompt(issuesRoot, cwd), nil
	}

	return buf.String(), nil
}

// detectIssueIDConvention analyzes existing issue IDs to detect the naming pattern
func detectIssueIDConvention(issues []*model.Issue) (pattern, example, nextID string) {
	// Default fallback
	pattern = "<prefix>-<number> (e.g., proj-001, issue-42)"
	example = "orch-001"
	nextID = "orch-001"

	if len(issues) == 0 {
		return
	}

	// Extract all issue IDs
	ids := make([]string, 0, len(issues))
	for _, issue := range issues {
		ids = append(ids, issue.ID)
	}

	// Try to detect pattern: prefix-number (most common)
	prefixNumRegex := regexp.MustCompile(`^([a-zA-Z][\w-]*)-(\d+)$`)

	prefixCounts := make(map[string]int)
	maxNums := make(map[string]int)

	for _, id := range ids {
		matches := prefixNumRegex.FindStringSubmatch(id)
		if matches != nil {
			prefix := matches[1]
			num, _ := strconv.Atoi(matches[2])
			prefixCounts[prefix]++
			if num > maxNums[prefix] {
				maxNums[prefix] = num
			}
		}
	}

	// Find most common prefix
	var mostCommonPrefix string
	maxCount := 0
	for prefix, count := range prefixCounts {
		if count > maxCount {
			maxCount = count
			mostCommonPrefix = prefix
		}
	}

	if mostCommonPrefix != "" {
		// Determine padding width from existing IDs
		padWidth := 3 // default
		for _, id := range ids {
			matches := prefixNumRegex.FindStringSubmatch(id)
			if matches != nil && matches[1] == mostCommonPrefix {
				numStr := matches[2]
				if len(numStr) > padWidth {
					padWidth = len(numStr)
				}
			}
		}

		pattern = fmt.Sprintf("%s-<number> (zero-padded to %d digits)", mostCommonPrefix, padWidth)
		example = fmt.Sprintf("%s-%0*d", mostCommonPrefix, padWidth, 1)
		nextNum := maxNums[mostCommonPrefix] + 1
		nextID = fmt.Sprintf("%s-%0*d", mostCommonPrefix, padWidth, nextNum)
	}

	return
}

func getGitBranch(workDir string) string {
	cmd := exec.Command("git", "-C", workDir, "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func getUncommittedChangesStatus(workDir string) string {
	cmd := exec.Command("git", "-C", workDir, "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return "Unknown"
	}
	if len(strings.TrimSpace(string(output))) > 0 {
		return "Yes"
	}
	return "No"
}

func getLastCommitMessage(workDir string) string {
	cmd := exec.Command("git", "-C", workDir, "log", "-1", "--format=%s")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	msg := strings.TrimSpace(string(output))
	if len(msg) > 80 {
		msg = msg[:77] + "..."
	}
	return msg
}

func getAvailableAgents() string {
	return "opencode, claude, codex, gemini, custom"
}

const maxExtraPromptSize = 16 * 1024

func loadExtraPrompt() string {
	configDir := config.RepoConfigDir()
	if configDir == "" {
		return ""
	}
	extraPath := filepath.Join(configDir, "control-prompt-extra.md")
	file, err := os.Open(extraPath)
	if err != nil {
		return ""
	}
	defer file.Close()

	data := make([]byte, maxExtraPromptSize)
	n, err := file.Read(data)
	if err != nil && n == 0 {
		return ""
	}
	return strings.TrimSpace(string(data[:n]))
}

// buildFallbackControlPrompt creates a simple prompt when template fails
func buildFallbackControlPrompt(issuesRoot, cwd string) string {
	return fmt.Sprintf(`You are the orch control agent for this repository.
You can run orch commands directly via bash to manage issues and runs.

Context:
- Vault: %s
- Working directory: %s

Available commands (run directly via bash):
- orch issue create <id> --title "<title>" --body "<body>"
- orch issue list
- orch run <issue-id>
- orch ps
- orch stop <issue-id>#<run-id>
- orch resolve <issue-id>#<run-id>
- orch open <issue-id>
`, issuesRoot, cwd)
}

// WriteControlPromptFile writes the control agent prompt to a temp file
func WriteControlPromptFile(st store.Store) (string, error) {
	prompt, err := buildControlAgentPrompt(st)
	if err != nil {
		return "", err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	promptPath := filepath.Join(cwd, controlPromptFileName)
	if err := os.WriteFile(promptPath, []byte(prompt), 0644); err != nil {
		return "", fmt.Errorf("failed to write control prompt file: %w", err)
	}

	return promptPath, nil
}

// WriteControlPromptFileViaAPI writes the control agent prompt using the daemon API
func WriteControlPromptFileViaAPI(ctx context.Context, api orchapi.OrchAPI, issuesRoot string) (string, error) {
	prompt, err := buildControlAgentPromptViaAPI(ctx, api, issuesRoot)
	if err != nil {
		return "", err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	promptPath := filepath.Join(cwd, controlPromptFileName)
	if err := os.WriteFile(promptPath, []byte(prompt), 0644); err != nil {
		return "", fmt.Errorf("failed to write control prompt file: %w", err)
	}

	return promptPath, nil
}

func buildControlAgentPromptViaAPI(ctx context.Context, api orchapi.OrchAPI, issuesRoot string) (string, error) {
	cwd, _ := os.Getwd()

	issuesResult, _ := api.ListIssues(ctx, nil)
	runsResult, _ := api.ListRuns(ctx, &orchapi.ListRunsFilter{
		Status: []orchapi.RunStatus{
			orchapi.RunStatusRunning,
			orchapi.RunStatusBlocked,
			orchapi.RunStatusBlockedAPI,
			orchapi.RunStatusBooting,
			orchapi.RunStatusQueued,
			orchapi.RunStatusPROpen,
		},
		Limit: 20,
	})

	var issues []*orchapi.Issue
	if issuesResult != nil {
		issues = issuesResult.Issues
	}
	var runs []*orchapi.Run
	if runsResult != nil {
		runs = runsResult.Runs
	}

	pattern, example, nextID := detectIssueIDConventionFromAPI(issues)

	issueInfos := make([]IssueInfo, 0, len(issues))
	for _, issue := range issues {
		status := string(issue.Status)
		if status == "" {
			status = "open"
		}
		title := issue.Title
		if title == "" {
			title = "-"
		}
		if len(title) > 50 {
			title = title[:47] + "..."
		}
		issueInfos = append(issueInfos, IssueInfo{
			ID:     issue.ID,
			Status: status,
			Title:  title,
		})
	}

	runInfos := make([]RunInfo, 0, len(runs))
	for _, run := range runs {
		runInfos = append(runInfos, RunInfo{
			IssueID: run.IssueID,
			ShortID: run.ShortID,
			Status:  string(run.Status),
		})
	}

	cfg, _ := config.Load()
	defaultAgent := "opencode"
	if cfg != nil {
		if cfg.ControlAgent != "" {
			defaultAgent = cfg.ControlAgent
		} else if cfg.Agent != "" {
			defaultAgent = cfg.Agent
		}
	}

	data := ControlPromptData{
		IssuesRoot:     issuesRoot,
		WorkDir:        cwd,
		IssueIDPattern: pattern,
		IssueIDExample: example,
		NextIssueID:    nextID,
		Issues:         issueInfos,
		ActiveRuns:     runInfos,

		GitBranch:          getGitBranch(cwd),
		UncommittedChanges: getUncommittedChangesStatus(cwd),
		LastCommitMessage:  getLastCommitMessage(cwd),

		DefaultAgent:    defaultAgent,
		AvailableAgents: getAvailableAgents(),

		ExtraPrompt: loadExtraPrompt(),
	}

	tmpl, err := template.New("control-prompt").Parse(controlPromptTemplate)
	if err != nil {
		return buildFallbackControlPrompt(issuesRoot, cwd), nil
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return buildFallbackControlPrompt(issuesRoot, cwd), nil
	}

	return buf.String(), nil
}

func detectIssueIDConventionFromAPI(issues []*orchapi.Issue) (pattern, example, nextID string) {
	pattern = "<prefix>-<number> (e.g., proj-001, issue-42)"
	example = "orch-001"
	nextID = "orch-001"

	if len(issues) == 0 {
		return
	}

	ids := make([]string, 0, len(issues))
	for _, issue := range issues {
		ids = append(ids, issue.ID)
	}

	prefixNumRegex := regexp.MustCompile(`^([a-zA-Z][\w-]*)-(\d+)$`)

	prefixCounts := make(map[string]int)
	maxNums := make(map[string]int)

	for _, id := range ids {
		matches := prefixNumRegex.FindStringSubmatch(id)
		if matches != nil {
			prefix := matches[1]
			num, _ := strconv.Atoi(matches[2])
			prefixCounts[prefix]++
			if num > maxNums[prefix] {
				maxNums[prefix] = num
			}
		}
	}

	var mostCommonPrefix string
	maxCount := 0
	for prefix, count := range prefixCounts {
		if count > maxCount {
			maxCount = count
			mostCommonPrefix = prefix
		}
	}

	if mostCommonPrefix != "" {
		padWidth := 3
		for _, id := range ids {
			matches := prefixNumRegex.FindStringSubmatch(id)
			if matches != nil && matches[1] == mostCommonPrefix {
				numStr := matches[2]
				if len(numStr) > padWidth {
					padWidth = len(numStr)
				}
			}
		}

		pattern = fmt.Sprintf("%s-<number> (zero-padded to %d digits)", mostCommonPrefix, padWidth)
		example = fmt.Sprintf("%s-%0*d", mostCommonPrefix, padWidth, 1)
		nextNum := maxNums[mostCommonPrefix] + 1
		nextID = fmt.Sprintf("%s-%0*d", mostCommonPrefix, padWidth, nextNum)
	}

	return
}

// GetControlPromptInstruction returns the instruction for reading the prompt file
func GetControlPromptInstruction() string {
	return controlPromptFileInstruction
}

// sortIssuesByID sorts issues by their numeric ID if they follow prefix-number pattern
func sortIssuesByID(issues []*model.Issue) {
	prefixNumRegex := regexp.MustCompile(`^([a-zA-Z][\w-]*)-(\d+)$`)

	sort.Slice(issues, func(i, j int) bool {
		matchI := prefixNumRegex.FindStringSubmatch(issues[i].ID)
		matchJ := prefixNumRegex.FindStringSubmatch(issues[j].ID)

		// If both match pattern, compare by prefix then number
		if matchI != nil && matchJ != nil {
			if matchI[1] != matchJ[1] {
				return matchI[1] < matchJ[1]
			}
			numI, _ := strconv.Atoi(matchI[2])
			numJ, _ := strconv.Atoi(matchJ[2])
			return numI < numJ
		}

		// Fall back to string comparison
		return issues[i].ID < issues[j].ID
	})
}

// buildAgentChatPrompt is kept for backwards compatibility but now generates dynamic content
// Deprecated: use buildControlAgentPrompt with store access instead
func buildAgentChatPrompt(issuesRoot string) string {
	cwd, _ := os.Getwd()
	return buildFallbackControlPrompt(issuesRoot, cwd)
}

func fallbackChatCommand(reason string) string {
	msg := "Agent chat unavailable"
	if strings.TrimSpace(reason) != "" {
		msg = fmt.Sprintf("Agent chat unavailable: %s", reason)
	}
	cmd := fmt.Sprintf("echo %s; exec ${SHELL:-sh}", shellQuote(msg))
	return fmt.Sprintf("sh -c %s", shellQuote(cmd))
}
