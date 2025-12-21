package monitor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
)

const (
	controlPromptFileName        = "ORCH_CONTROL_PROMPT.md"
	controlPromptFileInstruction = "Please read '" + controlPromptFileName + "' in the current directory and follow the instructions found there."
)

// controlPromptTemplate is the template for the control agent prompt
const controlPromptTemplate = `You are the orch control agent for this repository.
You can run orch commands directly via bash to manage issues and runs.

## Repository Context

- Vault: {{.VaultPath}}
- Working directory: {{.WorkDir}}

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

## Available Orch Commands

Run these commands directly using bash (do not use any special protocol):

### Issue Management
- Create issue: ` + "`orch issue create <id> --title \"<title>\" --body \"<body>\"`" + `
- List issues: ` + "`orch issue list`" + `
- Open issue in editor: ` + "`orch open <issue-id>`" + `

### Run Management
- Start a run: ` + "`orch run <issue-id>`" + `
- List runs: ` + "`orch ps`" + ` (use ` + "`--status running,blocked`" + ` to filter)
- Stop a run: ` + "`orch stop <issue-id>#<run-id>`" + `
- Resolve a run: ` + "`orch resolve <issue-id>#<run-id>`" + `
- Show run details: ` + "`orch show <issue-id>#<run-id>`" + `

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
`

// ControlPromptData contains data for the control agent prompt template
type ControlPromptData struct {
	VaultPath      string
	WorkDir        string
	IssueIDPattern string
	IssueIDExample string
	NextIssueID    string
	Issues         []IssueInfo
	ActiveRuns     []RunInfo
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
	vaultPath := st.VaultPath()

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

	data := ControlPromptData{
		VaultPath:      vaultPath,
		WorkDir:        cwd,
		IssueIDPattern: pattern,
		IssueIDExample: example,
		NextIssueID:    nextID,
		Issues:         issueInfos,
		ActiveRuns:     runInfos,
	}

	tmpl, err := template.New("control-prompt").Parse(controlPromptTemplate)
	if err != nil {
		return buildFallbackControlPrompt(vaultPath, cwd), nil
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return buildFallbackControlPrompt(vaultPath, cwd), nil
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

// buildFallbackControlPrompt creates a simple prompt when template fails
func buildFallbackControlPrompt(vaultPath, cwd string) string {
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
`, vaultPath, cwd)
}

// writeControlPromptFile writes the control agent prompt to a temp file
func writeControlPromptFile(st store.Store) (string, error) {
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
func buildAgentChatPrompt(vaultPath string) string {
	cwd, _ := os.Getwd()
	return buildFallbackControlPrompt(vaultPath, cwd)
}

func fallbackChatCommand(reason string) string {
	msg := "Agent chat unavailable"
	if strings.TrimSpace(reason) != "" {
		msg = fmt.Sprintf("Agent chat unavailable: %s", reason)
	}
	cmd := fmt.Sprintf("echo %s; exec ${SHELL:-sh}", shellQuote(msg))
	return fmt.Sprintf("sh -c %s", shellQuote(cmd))
}
