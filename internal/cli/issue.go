package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/daemon"
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
	cmd.AddCommand(newIssueShowCmd())
	cmd.AddCommand(newIssueEditCmd())
	cmd.AddCommand(newIssueCloseCmd())
	cmd.AddCommand(newIssueSyncCmd())
	cmd.AddCommand(newIssueOpenCmd())

	return cmd
}

func newIssueCreateCmd() *cobra.Command {
	opts := &issueCreateOptions{}

	cmd := &cobra.Command{
		Use:   "create [ISSUE_ID]",
		Short: "Create a new issue",
		Long: `Create a new issue in the vault or on GitHub.

For local backend, ISSUE_ID is required.
For GitHub backend, ISSUE_ID is optional (GitHub assigns the number).

Examples:
  orch issue create fix-login-bug --title "Fix login timeout"
  orch issue create plc-123 --title "Add dark mode" --body "Users want dark mode support"
  orch issue create my-issue --edit  # Opens in $EDITOR
  orch issue create --title "GitHub issue"  # GitHub backend only`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			issueID := ""
			if len(args) > 0 {
				issueID = args[0]
			}
			return runIssueCreate(issueID, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.Title, "title", "t", "", "Issue title")
	cmd.Flags().StringVarP(&opts.Summary, "summary", "s", "", "Short summary for display (~50 chars)")
	cmd.Flags().StringVarP(&opts.Body, "body", "b", "", "Issue body/description")
	cmd.Flags().BoolVarP(&opts.Edit, "edit", "e", false, "Open in $EDITOR after creation")

	return cmd
}

func runIssueCreate(issueID string, opts *issueCreateOptions) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if opts.Edit && cfg.IsGitHubBackend() {
		return fmt.Errorf("--edit flag is not supported with GitHub backend")
	}

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

	if opts.Edit || testBypassDaemon {
		return runIssueCreateWithEditor(issueID, title, opts)
	}

	client, err := requireDaemon()
	if err != nil {
		return err
	}

	resp, err := client.CreateIssue(issueID, title, opts.Summary, opts.Body)
	if err != nil {
		if err.Error() == "daemon error: already_exists" {
			return fmt.Errorf("issue already exists: %s", issueID)
		}
		if err.Error() == "daemon error: invalid_request: issue_id contains invalid characters" {
			return fmt.Errorf("invalid issue ID: %s (cannot contain / or ..)", issueID)
		}
		return err
	}

	if globalOpts.JSON {
		output := struct {
			OK      bool   `json:"ok"`
			IssueID string `json:"issue_id"`
			Path    string `json:"path"`
		}{
			OK:      true,
			IssueID: resp.IssueID,
			Path:    resp.Path,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(output)
	}

	if !globalOpts.Quiet {
		fmt.Printf("Created issue: %s\n", resp.IssueID)
		fmt.Printf("  Path: %s\n", resp.Path)
	}

	return nil
}

func runIssueCreateWithEditor(issueID, title string, opts *issueCreateOptions) error {
	issuesRoot, err := getIssuesRoot()
	if err != nil {
		return err
	}

	issuesDir, err := resolveIssuesDir(issuesRoot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(issuesDir, 0755); err != nil {
		return fmt.Errorf("failed to create issues directory: %w", err)
	}

	issuePath := filepath.Join(issuesDir, issueID+".md")

	if _, err := os.Stat(issuePath); err == nil {
		return fmt.Errorf("issue already exists: %s", issueID)
	}

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
	}

	if err := os.WriteFile(issuePath, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("failed to create issue: %w", err)
	}

	if opts.Edit && !testBypassDaemon {
		if err := openInEditor(issuePath); err != nil {
			return fmt.Errorf("failed to open editor: %w", err)
		}
	}

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

func resolveIssuesDir(issuesRoot string) (string, error) {
	if strings.TrimSpace(issuesRoot) == "" {
		return "", fmt.Errorf("vault path is required")
	}

	if strings.EqualFold(filepath.Base(issuesRoot), "issues") {
		return issuesRoot, nil
	}

	issuesDir := filepath.Join(issuesRoot, "issues")
	if dirExists(issuesDir) {
		return issuesDir, nil
	}

	issuesDir = filepath.Join(issuesRoot, "Issues")
	if dirExists(issuesDir) {
		return issuesDir, nil
	}

	return filepath.Join(issuesRoot, "issues"), nil
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
	projectRoot, _ := getProjectRoot()

	client := daemon.NewClient(projectRoot)
	if client.IsAvailable() {
		return runIssueListViaDaemon(client)
	}

	return runIssueListDirect()
}

func runIssueListViaDaemon(client *daemon.Client) error {
	issuesResp, err := client.ListIssues(nil, 0, "")
	if err != nil {
		return err
	}

	runsResp, err := client.ListRuns("", []string{"running", "blocked", "blocked_api", "booting", "queued"}, 0, "")
	if err != nil {
		runsResp = &daemon.ListRunsResponse{Runs: nil}
	}

	runsByIssue := make(map[string][]*daemon.RunSummary)
	for _, run := range runsResp.Runs {
		runsByIssue[run.IssueID] = append(runsByIssue[run.IssueID], run)
	}

	var issueInfos []issueInfo
	for _, issue := range issuesResp.Issues {
		info := issueInfo{
			ID:      issue.ID,
			Title:   issue.Title,
			Summary: issue.Summary,
			Status:  issue.Status,
		}

		for _, run := range runsByIssue[issue.ID] {
			info.Runs = append(info.Runs, runSummary{
				RunID:  run.RunID,
				Status: run.Status,
			})
		}

		issueInfos = append(issueInfos, info)
	}

	return outputIssueList(issueInfos)
}

func runIssueListDirect() error {
	st, err := getStore()
	if err != nil {
		return err
	}

	issues, err := st.ListIssues()
	if err != nil {
		return err
	}

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

	return outputIssueList(issueInfos)
}

func outputIssueList(issueInfos []issueInfo) error {
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

type issueShowOptions struct {
	Web bool
}

func newIssueShowCmd() *cobra.Command {
	opts := &issueShowOptions{}

	cmd := &cobra.Command{
		Use:   "show ISSUE_ID",
		Short: "Show issue details",
		Long: `Show details of an issue.

Examples:
  orch issue show 123          # Show issue #123 details
  orch issue show gh-123       # Show GitHub issue #123
  orch issue show 123 --web    # Open in browser`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIssueShow(args[0], opts)
		},
	}

	cmd.Flags().BoolVarP(&opts.Web, "web", "w", false, "Open in browser")

	return cmd
}

func runIssueShow(issueID string, opts *issueShowOptions) error {
	if model.IsGitHubIssueID(issueID) {
		issueID = model.NormalizeGitHubIssueID(issueID)
	}

	client, err := requireDaemon()
	if err != nil {
		return err
	}

	resp, err := client.GetIssue(issueID)
	if err != nil {
		return err
	}

	if opts.Web && resp.Issue != nil {
		url := resp.Issue.URI
		if fm, ok := resp.Issue.Frontmatter["url"]; ok && fm != "" {
			url = fm
		}
		if url != "" {
			return openWithSystem(url)
		}
		return fmt.Errorf("no URL available for issue %s", issueID)
	}

	if globalOpts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}

	issue := resp.Issue
	if issue == nil {
		return fmt.Errorf("issue not found: %s", issueID)
	}

	fmt.Printf("ID:      %s\n", issue.ID)
	fmt.Printf("Title:   %s\n", issue.Title)
	fmt.Printf("Status:  %s\n", issue.Status)
	if issue.Summary != "" {
		fmt.Printf("Summary: %s\n", issue.Summary)
	}
	if url, ok := issue.Frontmatter["url"]; ok && url != "" {
		fmt.Printf("URL:     %s\n", url)
	}
	if issue.Body != "" {
		fmt.Printf("\n%s\n", issue.Body)
	}

	return nil
}

func newIssueEditCmd() *cobra.Command {
	var title string

	cmd := &cobra.Command{
		Use:   "edit ISSUE_ID",
		Short: "Edit an issue",
		Long: `Edit an issue in $EDITOR, then sync changes back.

For GitHub issues, changes are pushed to GitHub.
For local issues, the file is edited directly.

Examples:
  orch issue edit 123                    # Open issue in $EDITOR
  orch issue edit gh-123 --title "New"   # Update title directly`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIssueEdit(args[0], title)
		},
	}

	cmd.Flags().StringVarP(&title, "title", "t", "", "Update title directly without opening editor")

	return cmd
}

func runIssueEdit(issueID, title string) error {
	if model.IsGitHubIssueID(issueID) {
		issueID = model.NormalizeGitHubIssueID(issueID)
	}

	client, err := requireDaemon()
	if err != nil {
		return err
	}

	resp, err := client.GetIssue(issueID)
	if err != nil {
		return err
	}

	if resp.Issue == nil {
		return fmt.Errorf("issue not found: %s", issueID)
	}

	if title != "" {
		return fmt.Errorf("--title update not yet implemented for local issues; edit the file directly: %s", resp.Issue.URI)
	}

	path := resp.Issue.URI
	if strings.HasPrefix(path, "file://") {
		path = strings.TrimPrefix(path, "file://")
	}

	if strings.HasPrefix(path, "https://") {
		return editGitHubIssue(issueID, resp.Issue)
	}

	return openInEditor(path)
}

func editGitHubIssue(issueID string, issue *daemon.IssueFull) error {
	tmpDir := os.TempDir()
	tmpFile := filepath.Join(tmpDir, fmt.Sprintf("orch-issue-%s.md", strings.ReplaceAll(issueID, "/", "-")))

	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("issue: %s\n", issueID))
	if url, ok := issue.Frontmatter["url"]; ok {
		sb.WriteString(fmt.Sprintf("url: %s\n", url))
	}
	sb.WriteString(fmt.Sprintf("status: %s\n", issue.Status))
	if labels, ok := issue.Frontmatter["labels"]; ok && labels != "" {
		sb.WriteString(fmt.Sprintf("labels: [%s]\n", labels))
	}
	sb.WriteString("---\n\n")
	sb.WriteString(fmt.Sprintf("# %s\n\n", issue.Title))
	sb.WriteString(issue.Body)
	sb.WriteString("\n")

	if err := os.WriteFile(tmpFile, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	if err := openInEditor(tmpFile); err != nil {
		return err
	}

	if !globalOpts.Quiet {
		fmt.Printf("Edited issue saved to: %s\n", tmpFile)
		fmt.Println("Note: Changes to GitHub issues require manual sync via 'gh issue edit'")
	}

	return nil
}

type issueCloseOptions struct {
	Comment string
}

func newIssueCloseCmd() *cobra.Command {
	opts := &issueCloseOptions{}

	cmd := &cobra.Command{
		Use:   "close ISSUE_ID",
		Short: "Close an issue",
		Long: `Close an issue.

For GitHub issues, the issue is closed on GitHub.
For local issues, the status is set to 'closed'.

Examples:
  orch issue close 123
  orch issue close gh-123 --comment "Fixed in #456"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIssueClose(args[0], opts)
		},
	}

	cmd.Flags().StringVarP(&opts.Comment, "comment", "c", "", "Add a closing comment")

	return cmd
}

func runIssueClose(issueID string, opts *issueCloseOptions) error {
	// Normalize GitHub issue IDs at CLI boundary
	if model.IsGitHubIssueID(issueID) {
		issueID = model.NormalizeGitHubIssueID(issueID)
	}

	client, err := requireDaemon()
	if err != nil {
		return err
	}

	resp, err := client.CloseIssue(issueID, opts.Comment)
	if err != nil {
		return err
	}

	if globalOpts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}

	if !globalOpts.Quiet {
		fmt.Printf("Closed issue: %s\n", issueID)
	}
	return nil
}

func newIssueSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync issues from GitHub",
		Long: `Force sync issues from GitHub.

This refreshes the local cache with the latest issues from GitHub.
Normally, the daemon handles syncing automatically.

Examples:
  orch issue sync`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIssueSync()
		},
	}

	return cmd
}

func runIssueSync() error {
	if !globalOpts.Quiet && !globalOpts.JSON {
		fmt.Println("Syncing issues from GitHub...")
	}

	err := runIssueList()
	if err != nil {
		return err
	}

	if !globalOpts.Quiet && !globalOpts.JSON {
		fmt.Println("Sync complete.")
	}
	return nil
}

func newIssueOpenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "open ISSUE_ID",
		Short: "Open issue in browser",
		Long: `Open an issue in the default web browser.

This is an alias for 'orch issue show ISSUE_ID --web'.

Examples:
  orch issue open 123
  orch issue open gh-123`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIssueShow(args[0], &issueShowOptions{Web: true})
		},
	}

	return cmd
}
