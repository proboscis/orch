package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

type issueCreateOptions struct {
	Title string
	Body  string
	Edit  bool
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
	cmd.Flags().StringVarP(&opts.Body, "body", "b", "", "Issue body/description")
	cmd.Flags().BoolVarP(&opts.Edit, "edit", "e", false, "Open in $EDITOR after creation")

	return cmd
}

func runIssueCreate(issueID string, opts *issueCreateOptions) error {
	vaultPath, err := getVaultPath()
	if err != nil {
		return err
	}

	issuesDir := filepath.Join(vaultPath, "issues")
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

func runIssueList() error {
	st, err := getStore()
	if err != nil {
		return err
	}

	issues, err := st.ListIssues()
	if err != nil {
		return err
	}

	type issueInfo struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Path  string `json:"path"`
	}

	var issueInfos []issueInfo
	for _, issue := range issues {
		issueInfos = append(issueInfos, issueInfo{
			ID:    issue.ID,
			Title: issue.Title,
			Path:  issue.Path,
		})
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

	for _, issue := range issueInfos {
		fmt.Printf("%s\t%s\n", issue.ID, issue.Title)
	}

	return nil
}
