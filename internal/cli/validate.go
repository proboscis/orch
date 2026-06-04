package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/orchapi"
	"github.com/spf13/cobra"
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
	api, err := getAPIForListing()
	if err != nil {
		return err
	}

	apiResult, err := api.ValidateIssueFiles(context.Background(), model.NewIssueID(issueID))
	if err != nil {
		return err
	}

	output := validateOutputFromAPI(apiResult)

	if globalOpts.JSON {
		return outputValidateJSON(output)
	}

	return outputValidatePlain(output)
}

func validateOutputFromAPI(result *orchapi.ValidateIssueFilesResult) *ValidateOutput {
	output := &ValidateOutput{}
	if result == nil {
		return output
	}

	output.Total = result.Total
	output.Valid = result.Valid
	output.Errors = convertValidationResults(result.Errors)
	output.Warnings = convertValidationResults(result.Warnings)
	output.Duplicates = convertDuplicateIDs(result.Duplicates)

	return output
}

func convertValidationResults(results []*orchapi.ValidationResult) []*ValidationResult {
	if len(results) == 0 {
		return nil
	}

	out := make([]*ValidationResult, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		out = append(out, &ValidationResult{
			File:     result.File,
			IssueID:  result.IssueID.String(),
			Errors:   convertValidationIssues(result.Errors),
			Warnings: convertValidationIssues(result.Warnings),
		})
	}

	return out
}

func convertValidationIssues(issues []orchapi.ValidationIssue) []ValidationIssue {
	if len(issues) == 0 {
		return nil
	}

	out := make([]ValidationIssue, 0, len(issues))
	for _, issue := range issues {
		out = append(out, ValidationIssue{
			Code:    issue.Code,
			Message: issue.Message,
			Line:    issue.Line,
			Level:   ValidationLevel(issue.Level),
		})
	}

	return out
}

func convertDuplicateIDs(duplicates []orchapi.DuplicateID) []DuplicateID {
	if len(duplicates) == 0 {
		return nil
	}

	out := make([]DuplicateID, 0, len(duplicates))
	for _, duplicate := range duplicates {
		out = append(out, DuplicateID{ID: duplicate.ID.String(), Files: duplicate.Files})
	}

	return out
}

func outputValidateJSON(output *ValidateOutput) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

func outputValidatePlain(output *ValidateOutput) error {
	if output == nil {
		output = &ValidateOutput{}
	}

	errorResults := append([]*ValidationResult(nil), output.Errors...)
	warningResults := append([]*ValidationResult(nil), output.Warnings...)
	duplicates := append([]DuplicateID(nil), output.Duplicates...)

	sort.Slice(errorResults, func(i, j int) bool {
		return errorResults[i].File < errorResults[j].File
	})
	sort.Slice(warningResults, func(i, j int) bool {
		return warningResults[i].File < warningResults[j].File
	})
	sort.Slice(duplicates, func(i, j int) bool {
		return duplicates[i].ID < duplicates[j].ID
	})

	errorCount := 0
	warningCount := 0

	if !globalOpts.Quiet {
		fmt.Printf("Validating %d issue file(s)...\n\n", output.Total)
	}

	for _, r := range errorResults {
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

	for _, r := range warningResults {
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
			errorCount+len(duplicates), warningCount, output.Valid)
	}

	if errorCount > 0 || len(duplicates) > 0 {
		os.Exit(ExitValidateError)
	}

	return nil
}
