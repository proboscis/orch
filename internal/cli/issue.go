package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/s22625/orch/internal/model"
	"github.com/spf13/cobra"
)

type issueCreateOptions struct {
	Title   string
	Summary string
	Body    string
	Edit    bool
}

func newIssueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "issue",
		Short: "Manage issues",
		Long:  `Create and manage issues in the vault.`,
	}

	cmd.AddCommand(newIssueCreateCmd())
	cmd.AddCommand(newIssueListCmd())

	return cmd
}

func newIssueCreateCmd() *cobra.Command {
	opts := &issueCreateOptions{}

	cmd := &cobra.Command{
		Use:   "create ISSUE_ID",
		Short: "Create a new issue",
		Long: `Create a new issue in the vault.

Examples:
  orch issue create fix-login-bug --title "Fix login timeout"
  orch issue create plc-123 --title "Add dark mode" --body "Users want dark mode support"
  orch issue create my-issue --edit  # Opens in $EDITOR`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIssueCreate(args[0], opts)
		},
	}

	cmd.Flags().StringVarP(&opts.Title, "title", "t", "", "Issue title")
	cmd.Flags().StringVarP(&opts.Summary, "summary", "s", "", "Short summary for display (~50 chars)")
	cmd.Flags().StringVarP(&opts.Body, "body", "b", "", "Issue body/description")
	cmd.Flags().BoolVarP(&opts.Edit, "edit", "e", false, "Open in $EDITOR after creation")

	return cmd
}

func runIssueCreate(issueID string, opts *issueCreateOptions) error {
	vaultPath, err := getVaultPath()
	if err != nil {
		return err
	}

	issuesDir, err := resolveIssuesDir(vaultPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		return fmt.Errorf("failed to create issues directory: %w", err)
	}

	issuePath := filepath.Join(issuesDir, issueID+".md")

	// Check if issue already exists
	if _, err := os.Stat(issuePath); err == nil {
		return fmt.Errorf("issue already exists: %s", issueID)
	}

	// If no title provided, prompt for it
	title := opts.Title
	if title == "" && !opts.Edit {
		fmt.Print("Title: ")
		reader := bufio.NewReader(os.Stdin)
		title, _ = reader.ReadString('\n')
		title = strings.TrimSpace(title)
	}
	if title == "" {
		title = issueID
	}

	// Build issue content
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("type: issue\n")
	sb.WriteString(fmt.Sprintf("id: %s\n", issueID))
	sb.WriteString(fmt.Sprintf("title: %s\n", title))
	if opts.Summary != "" {
		sb.WriteString(fmt.Sprintf("summary: %s\n", opts.Summary))
	}
	sb.WriteString("status: open\n")
	sb.WriteString("---\n\n")
	sb.WriteString(fmt.Sprintf("# %s\n\n", title))

	if opts.Body != "" {
		sb.WriteString(opts.Body)
		sb.WriteString("\n")
	} else if !opts.Edit {
		sb.WriteString("<!-- Describe the issue here -->\n")
	}

	// Write the file
	if err := os.WriteFile(issuePath, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("failed to create issue: %w", err)
	}

	// Open in editor if requested
	if opts.Edit {
		if err := openInEditor(issuePath); err != nil {
			return fmt.Errorf("failed to open editor: %w", err)
		}
	}

	// Output
	if globalOpts.JSON {
		output := struct {
			OK      bool   `json:"ok"`
			IssueID string `json:"issue_id"`
			Path    string `json:"path"`
		}{
			OK:      true,
			IssueID: issueID,
			Path:    issuePath,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(output)
	}

	if !globalOpts.Quiet {
		fmt.Printf("Created issue: %s\n", issueID)
		fmt.Printf("  Path: %s\n", issuePath)
	}

	return nil
}

func resolveIssuesDir(vaultPath string) (string, error) {
	if strings.TrimSpace(vaultPath) == "" {
		return "", fmt.Errorf("vault path is required")
	}

	if strings.EqualFold(filepath.Base(vaultPath), "issues") {
		return vaultPath, nil
	}

	issuesDir := filepath.Join(vaultPath, "issues")
	if dirExists(issuesDir) {
		return issuesDir, nil
	}

	issuesDir = filepath.Join(vaultPath, "Issues")
	if dirExists(issuesDir) {
		return issuesDir, nil
	}

	return filepath.Join(vaultPath, "issues"), nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func newIssueListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIssueList()
		},
	}

	return cmd
}

type runSummary struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
}

type issueInfo struct {
	ID      string       `json:"id"`
	Title   string       `json:"title"`
	Summary string       `json:"summary,omitempty"`
	Status  string       `json:"status"`
	Path    string       `json:"path"`
	Runs    []runSummary `json:"runs,omitempty"`
}

func runIssueList() error {
	st, err := getStore()
	if err != nil {
		return err
	}

	issues, err := st.ListIssues()
	if err != nil {
		return err
	}

	// Get all runs to match with issues
	allRuns, _ := st.ListRuns(nil)
	runsByIssue := make(map[string][]*model.Run)
	for _, run := range allRuns {
		runsByIssue[run.IssueID] = append(runsByIssue[run.IssueID], run)
	}

	var issueInfos []issueInfo
	for _, issue := range issues {
		info := issueInfo{
			ID:      issue.ID,
			Title:   issue.Title,
			Summary: issue.Summary,
			Status:  string(issue.Status),
			Path:    issue.Path,
		}

		// Add active runs (non-terminal states)
		for _, run := range runsByIssue[issue.ID] {
			if run.Status == model.StatusRunning ||
				run.Status == model.StatusBlocked ||
				run.Status == model.StatusBlockedAPI ||
				run.Status == model.StatusBooting ||
				run.Status == model.StatusQueued {
				info.Runs = append(info.Runs, runSummary{
					RunID:  run.RunID,
					Status: string(run.Status),
				})
			}
		}

		issueInfos = append(issueInfos, info)
	}

	if globalOpts.JSON {
		output := struct {
			OK     bool        `json:"ok"`
			Issues []issueInfo `json:"issues"`
		}{
			OK:     true,
			Issues: issueInfos,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(output)
	}

	if len(issueInfos) == 0 {
		if !globalOpts.Quiet {
			fmt.Println("No issues found")
		}
		return nil
	}

	// Print with tabwriter for alignment
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSTATUS\tSUMMARY\tRUNS")
	for _, issue := range issueInfos {
		runsSummary := "-"
		if len(issue.Runs) > 0 {
			runsSummary = formatRunsSummary(issue.Runs)
		}
		status := issue.Status
		if status == "" {
			status = "-"
		}
		summary := issue.Summary
		if summary == "" {
			summary = "-"
		} else if len(summary) > 40 {
			summary = summary[:37] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", issue.ID, status, summary, runsSummary)
	}
	w.Flush()

	return nil
}

// formatRunsSummary formats runs into a summary like "1 running, 1 blocked"
func formatRunsSummary(runs []runSummary) string {
	counts := make(map[string]int)
	for _, r := range runs {
		counts[r.Status]++
	}

	var parts []string
	for status, count := range counts {
		parts = append(parts, fmt.Sprintf("%d %s", count, status))
	}

	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}
