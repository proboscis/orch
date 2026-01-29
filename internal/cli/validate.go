package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	ExitValidateOK    = 0
	ExitValidateError = 1
	ExitValidateUsage = 2
)

type ValidationLevel string

const (
	ValidationError   ValidationLevel = "error"
	ValidationWarning ValidationLevel = "warning"
)

type ValidationIssue struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Line    int             `json:"line,omitempty"`
	Level   ValidationLevel `json:"level"`
}

type ValidationResult struct {
	File     string            `json:"file"`
	IssueID  string            `json:"issue_id,omitempty"`
	Errors   []ValidationIssue `json:"errors,omitempty"`
	Warnings []ValidationIssue `json:"warnings,omitempty"`
}

type DuplicateID struct {
	ID    string   `json:"id"`
	Files []string `json:"files"`
}

type ValidateOutput struct {
	Total      int                 `json:"total"`
	Valid      int                 `json:"valid"`
	Errors     []*ValidationResult `json:"errors,omitempty"`
	Warnings   []*ValidationResult `json:"warnings,omitempty"`
	Duplicates []DuplicateID       `json:"duplicates,omitempty"`
}

var validIssueStatuses = map[string]bool{
	"open":     true,
	"resolved": true,
	"closed":   true,
}

var idCharRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func newValidateIssueFilesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate-issue-files [ISSUE_ID]",
		Short: "Validate issue files for proper formatting",
		Long: `Validate all issue files or a specific issue for proper formatting.

Checks for:
  Errors (blocking):
    - Invalid YAML frontmatter syntax
    - Missing required field: id
    - Missing required field: title
    - Invalid status (must be open/resolved/closed)
    - Duplicate issue IDs across files
    - File/ID mismatch (filename doesn't match frontmatter ID)

  Warnings (non-blocking):
    - Missing status field
    - Empty body content
    - Very long title (>100 characters)
    - Missing type field
    - Unusual characters in ID

Examples:
  orch validate-issue-files              # Validate all issues
  orch validate-issue-files orch-123     # Validate specific issue
  orch validate-issue-files --json       # JSON output for tooling`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var issueID string
			if len(args) > 0 {
				issueID = args[0]
			}
			return runValidateIssueFiles(issueID)
		},
	}

	return cmd
}

func runValidateIssueFiles(issueID string) error {
	issuesRoot, err := getIssuesRoot()
	if err != nil {
		return fmt.Errorf("failed to get issues root: %w", err)
	}

	issuesDir := filepath.Join(issuesRoot, "issues")
	if _, err := os.Stat(issuesDir); os.IsNotExist(err) {
		if globalOpts.JSON {
			return outputValidateJSON(&ValidateOutput{})
		}
		fmt.Println("No issues directory found")
		return nil
	}

	var results []*ValidationResult
	idToFiles := make(map[string][]string)

	if issueID != "" {
		result, err := validateSpecificIssue(issuesDir, issueID)
		if err != nil {
			return err
		}
		if result != nil {
			results = append(results, result)
			if result.IssueID != "" {
				idToFiles[result.IssueID] = append(idToFiles[result.IssueID], result.File)
			}
		}
	} else {
		results, idToFiles, err = validateAllIssues(issuesDir)
		if err != nil {
			return err
		}
	}

	var duplicates []DuplicateID
	for id, files := range idToFiles {
		if len(files) > 1 {
			duplicates = append(duplicates, DuplicateID{ID: id, Files: files})
		}
	}

	var errorResults []*ValidationResult
	var warningResults []*ValidationResult
	validCount := 0

	for _, r := range results {
		hasErrors := len(r.Errors) > 0
		hasWarnings := len(r.Warnings) > 0

		if hasErrors {
			errorResults = append(errorResults, r)
		} else if hasWarnings {
			warningResults = append(warningResults, r)
			validCount++
		} else {
			validCount++
		}
	}

	if globalOpts.JSON {
		return outputValidateJSON(&ValidateOutput{
			Total:      len(results),
			Valid:      validCount,
			Errors:     errorResults,
			Warnings:   warningResults,
			Duplicates: duplicates,
		})
	}

	return outputValidatePlain(results, duplicates, len(results), validCount)
}

func validateSpecificIssue(issuesDir, issueID string) (*ValidationResult, error) {
	path := filepath.Join(issuesDir, issueID+".md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		var found string
		filepath.Walk(issuesDir, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if strings.TrimSuffix(info.Name(), ".md") == issueID {
				found = p
				return filepath.SkipAll
			}
			return nil
		})
		if found == "" {
			return nil, fmt.Errorf("issue file not found: %s", issueID)
		}
		path = found
	}

	return validateIssueFile(path, issuesDir)
}

func validateAllIssues(issuesDir string) ([]*ValidationResult, map[string][]string, error) {
	var results []*ValidationResult
	idToFiles := make(map[string][]string)

	err := filepath.Walk(issuesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}

		result, err := validateIssueFile(path, issuesDir)
		if err != nil {
			return nil
		}
		if result != nil {
			results = append(results, result)
			if result.IssueID != "" {
				idToFiles[result.IssueID] = append(idToFiles[result.IssueID], result.File)
			}
		}
		return nil
	})

	return results, idToFiles, err
}

func validateIssueFile(path, issuesDir string) (*ValidationResult, error) {
	api, err := getAPI()
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	content, err := api.ReadFile(ctx, path)
	if err != nil {
		return nil, err
	}

	relPath, _ := filepath.Rel(issuesDir, path)
	if relPath == "" {
		relPath = filepath.Base(path)
	}

	result := &ValidationResult{
		File: relPath,
	}

	lines := strings.Split(string(content), "\n")

	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, nil
	}

	endIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			endIdx = i
			break
		}
	}

	if endIdx == -1 {
		result.Errors = append(result.Errors, ValidationIssue{
			Code:    "invalid_yaml",
			Message: "Frontmatter not closed (missing closing ---)",
			Level:   ValidationError,
		})
		return result, nil
	}

	fmContent := strings.Join(lines[1:endIdx], "\n")
	var fm map[string]interface{}
	if err := yaml.Unmarshal([]byte(fmContent), &fm); err != nil {
		result.Errors = append(result.Errors, ValidationIssue{
			Code:    "invalid_yaml",
			Message: fmt.Sprintf("Invalid YAML: %v", err),
			Level:   ValidationError,
		})
		return result, nil
	}

	typeVal, hasType := fm["type"]
	if !hasType {
		result.Warnings = append(result.Warnings, ValidationIssue{
			Code:    "missing_type",
			Message: "Missing type field (should be 'type: issue')",
			Level:   ValidationWarning,
		})
	} else if typeStr, ok := typeVal.(string); !ok || typeStr != "issue" {
		return nil, nil
	}

	idVal, hasID := fm["id"]
	var issueID string
	if !hasID || idVal == nil || idVal == "" {
		result.Errors = append(result.Errors, ValidationIssue{
			Code:    "missing_id",
			Message: "Missing required field: id",
			Level:   ValidationError,
		})
	} else {
		issueID = fmt.Sprintf("%v", idVal)
		result.IssueID = issueID

		if !idCharRegex.MatchString(issueID) {
			result.Warnings = append(result.Warnings, ValidationIssue{
				Code:    "unusual_id_chars",
				Message: fmt.Sprintf("ID contains unusual characters: %s (recommend alphanumeric with hyphens)", issueID),
				Level:   ValidationWarning,
			})
		}

		expectedFilename := issueID + ".md"
		actualFilename := filepath.Base(path)
		if actualFilename != expectedFilename {
			result.Errors = append(result.Errors, ValidationIssue{
				Code:    "file_id_mismatch",
				Message: fmt.Sprintf("File/ID mismatch: file is '%s' but frontmatter ID is '%s'", actualFilename, issueID),
				Level:   ValidationError,
			})
		}
	}

	titleVal, hasTitle := fm["title"]
	var title string
	if !hasTitle || titleVal == nil || titleVal == "" {
		result.Errors = append(result.Errors, ValidationIssue{
			Code:    "missing_title",
			Message: "Missing required field: title",
			Level:   ValidationError,
		})
	} else {
		title = fmt.Sprintf("%v", titleVal)
		if len(title) > 100 {
			result.Warnings = append(result.Warnings, ValidationIssue{
				Code:    "long_title",
				Message: fmt.Sprintf("Title exceeds 100 characters (%d chars)", len(title)),
				Level:   ValidationWarning,
			})
		}
	}

	statusVal, hasStatus := fm["status"]
	if !hasStatus || statusVal == nil || statusVal == "" {
		result.Warnings = append(result.Warnings, ValidationIssue{
			Code:    "missing_status",
			Message: "Missing status field (defaults to 'open' but should be explicit)",
			Level:   ValidationWarning,
		})
	} else {
		statusStr := fmt.Sprintf("%v", statusVal)
		if !validIssueStatuses[statusStr] {
			result.Errors = append(result.Errors, ValidationIssue{
				Code:    "invalid_status",
				Message: fmt.Sprintf("Invalid status: '%s' (must be open/resolved/closed)", statusStr),
				Level:   ValidationError,
			})
		}
	}

	bodyStart := endIdx + 1
	if bodyStart < len(lines) {
		body := strings.TrimSpace(strings.Join(lines[bodyStart:], "\n"))
		if body == "" || isBodyEmpty(body) {
			result.Warnings = append(result.Warnings, ValidationIssue{
				Code:    "empty_body",
				Message: "Empty body content",
				Level:   ValidationWarning,
			})
		}
	} else {
		result.Warnings = append(result.Warnings, ValidationIssue{
			Code:    "empty_body",
			Message: "Empty body content",
			Level:   ValidationWarning,
		})
	}

	return result, nil
}

func isBodyEmpty(body string) bool {
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "# ") && countNonEmptyLines(lines) == 1 {
			continue
		}
		return false
	}
	return true
}

func countNonEmptyLines(lines []string) int {
	count := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func outputValidateJSON(output *ValidateOutput) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

func outputValidatePlain(results []*ValidationResult, duplicates []DuplicateID, total, valid int) error {
	errorCount := 0
	warningCount := 0

	if !globalOpts.Quiet {
		fmt.Printf("Validating %d issue file(s)...\n\n", total)
	}

	for _, r := range results {
		if len(r.Errors) > 0 {
			errorCount++
			fmt.Printf("ERROR: %s\n", r.File)
			for _, issue := range r.Errors {
				if issue.Line > 0 {
					fmt.Printf("  - %s (line %d)\n", issue.Message, issue.Line)
				} else {
					fmt.Printf("  - %s\n", issue.Message)
				}
			}
			fmt.Println()
		}
	}

	for _, r := range results {
		if len(r.Warnings) > 0 && len(r.Errors) == 0 {
			warningCount++
			fmt.Printf("WARNING: %s\n", r.File)
			for _, issue := range r.Warnings {
				fmt.Printf("  - %s\n", issue.Message)
			}
			fmt.Println()
		}
	}

	for _, dup := range duplicates {
		errorCount++
		fmt.Printf("DUPLICATE ID: %s\n", dup.ID)
		for _, file := range dup.Files {
			fmt.Printf("  - %s\n", file)
		}
		fmt.Println()
	}

	if !globalOpts.Quiet {
		fmt.Printf("Validation complete: %d error(s), %d warning(s), %d valid\n",
			errorCount+len(duplicates), warningCount, valid)
	}

	if errorCount > 0 || len(duplicates) > 0 {
		os.Exit(ExitValidateError)
	}

	return nil
}
