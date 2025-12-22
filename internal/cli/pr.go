package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
	"github.com/spf13/cobra"
)

type prCreateOptions struct {
	Title string
	Body  string
	Base  string
	Draft bool
}

type prCreateResult struct {
	OK       bool   `json:"ok"`
	IssueID  string `json:"issue_id"`
	RunID    string `json:"run_id"`
	Branch   string `json:"branch"`
	PRUrl    string `json:"pr_url"`
	Existing bool   `json:"existing,omitempty"`
	Error    string `json:"error,omitempty"`
}

var prURLRegex = regexp.MustCompile(`https://(?:github\.com|gitlab\.com)/[^\s]+/(?:pull|merge_requests)/\d+`)

func newPrCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pr",
		Short: "Pull request helpers",
	}

	cmd.AddCommand(newPrCreateCmd())
	return cmd
}

func newPrCreateCmd() *cobra.Command {
	opts := &prCreateOptions{}

	cmd := &cobra.Command{
		Use:   "create RUN_REF",
		Short: "Create a pull request for a run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPrCreate(args[0], opts)
		},
	}

	cmd.Flags().StringVar(&opts.Title, "title", "", "PR title (default: issue title)")
	cmd.Flags().StringVar(&opts.Body, "body", "", "PR body (default: auto-generated)")
	cmd.Flags().StringVar(&opts.Base, "base", "", "Base branch (default: repo default)")
	cmd.Flags().BoolVar(&opts.Draft, "draft", false, "Create as draft pull request")

	return cmd
}

func runPrCreate(refStr string, opts *prCreateOptions) error {
	st, err := getStore()
	if err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	run, err := resolveRun(st, refStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run not found: %s\n", refStr)
		os.Exit(ExitRunNotFound)
		return err
	}

	if run.WorktreePath == "" {
		fmt.Fprintf(os.Stderr, "run has no worktree: %s\n", refStr)
		os.Exit(ExitWorktreeError)
		return fmt.Errorf("run has no worktree: %s", refStr)
	}

	worktreePath := run.WorktreePath
	if !filepath.IsAbs(worktreePath) {
		repoRoot, err := git.FindMainRepoRoot("")
		if err != nil {
			return exitWithCode(fmt.Errorf("could not find git repository: %w", err), ExitWorktreeError)
		}
		worktreePath = filepath.Join(repoRoot, worktreePath)
	}

	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "worktree does not exist: %s\n", worktreePath)
		os.Exit(ExitWorktreeError)
		return fmt.Errorf("worktree does not exist: %s", worktreePath)
	}

	if _, err := exec.LookPath("gh"); err != nil {
		return exitWithCode(fmt.Errorf("gh CLI not found in PATH"), ExitInternalError)
	}

	issue, err := st.ResolveIssue(run.IssueID)
	if err != nil {
		return exitWithCode(fmt.Errorf("issue not found: %s", run.IssueID), ExitIssueNotFound)
	}

	branch := run.Branch
	if branch == "" {
		branch, err = git.GetCurrentBranch(worktreePath)
		if err != nil {
			return exitWithCode(fmt.Errorf("failed to detect branch: %w", err), ExitWorktreeError)
		}
	}

	if run.PRUrl != "" {
		return outputPrCreateResult(&prCreateResult{
			OK:       true,
			IssueID:  run.IssueID,
			RunID:    run.RunID,
			Branch:   branch,
			PRUrl:    run.PRUrl,
			Existing: true,
		})
	}

	if prURL, err := lookupExistingPR(worktreePath, branch); err != nil {
		return exitWithCode(err, ExitInternalError)
	} else if prURL != "" {
		if err := recordPR(st, run, prURL); err != nil {
			return exitWithCode(err, ExitInternalError)
		}
		return outputPrCreateResult(&prCreateResult{
			OK:       true,
			IssueID:  run.IssueID,
			RunID:    run.RunID,
			Branch:   branch,
			PRUrl:    prURL,
			Existing: true,
		})
	}

	title := opts.Title
	if title == "" {
		if issue.Title != "" {
			title = issue.Title
		} else {
			title = run.IssueID
		}
	}

	base := opts.Base
	if base == "" {
		base = detectDefaultBase(worktreePath)
	}

	body := opts.Body
	if body == "" {
		body = buildPRBody(issue, worktreePath, base)
	}

	if err := pushBranch(worktreePath, branch); err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	prURL, err := createPR(worktreePath, branch, base, title, body, opts.Draft)
	if err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	if err := recordPR(st, run, prURL); err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	return outputPrCreateResult(&prCreateResult{
		OK:      true,
		IssueID: run.IssueID,
		RunID:   run.RunID,
		Branch:  branch,
		PRUrl:   prURL,
	})
}

func outputPrCreateResult(result *prCreateResult) error {
	if globalOpts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	if !globalOpts.Quiet {
		if result.Existing {
			fmt.Printf("PR already exists: %s\n", result.PRUrl)
		} else {
			fmt.Printf("PR created: %s\n", result.PRUrl)
		}
	}
	return nil
}

func lookupExistingPR(worktreePath, branch string) (string, error) {
	cmd := exec.Command("gh", "pr", "list", "--head", branch, "--state", "all", "--json", "url", "--limit", "1")
	cmd.Dir = worktreePath
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to query existing PRs: %w", err)
	}

	var prs []struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(output, &prs); err != nil {
		return "", fmt.Errorf("failed to parse PR list: %w", err)
	}
	if len(prs) == 0 {
		return "", nil
	}
	return prs[0].URL, nil
}

func pushBranch(worktreePath, branch string) error {
	cmd := exec.Command("git", "-C", worktreePath, "push", "-u", "origin", branch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git push failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func createPR(worktreePath, branch, base, title, body string, draft bool) (string, error) {
	args := []string{
		"pr", "create",
		"--title", title,
		"--body", body,
		"--head", branch,
	}
	if base != "" {
		args = append(args, "--base", base)
	}
	if draft {
		args = append(args, "--draft")
	}
	cmd := exec.Command("gh", args...)
	cmd.Dir = worktreePath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gh pr create failed: %s", strings.TrimSpace(string(output)))
	}

	prURL := prURLRegex.FindString(string(output))
	if prURL == "" {
		return "", fmt.Errorf("gh pr create did not return a PR URL")
	}
	return prURL, nil
}

func recordPR(st store.Store, run *model.Run, prURL string) error {
	if err := st.AppendEvent(run.Ref(), model.NewArtifactEvent("pr", map[string]string{
		"url": prURL,
	})); err != nil {
		return err
	}
	if err := st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusPROpen)); err != nil {
		return err
	}
	return nil
}

func detectDefaultBase(worktreePath string) string {
	cmd := exec.Command("git", "-C", worktreePath, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "main"
	}
	ref := strings.TrimSpace(string(output))
	if strings.HasPrefix(ref, "origin/") {
		ref = strings.TrimPrefix(ref, "origin/")
	}
	if ref == "" {
		return "main"
	}
	return ref
}

func buildPRBody(issue *model.Issue, worktreePath, baseBranch string) string {
	summary := commitSummary(worktreePath, baseBranch)
	if len(summary) == 0 {
		if issue.Summary != "" {
			summary = []string{issue.Summary}
		} else if issue.Title != "" {
			summary = []string{issue.Title}
		} else {
			summary = []string{"Summary pending."}
		}
	}

	var sb strings.Builder
	sb.WriteString("## Summary\n")
	for _, line := range summary {
		sb.WriteString("- ")
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString("## Issue\n")
	sb.WriteString("- ")
	sb.WriteString(issue.ID)
	sb.WriteString("\n")
	return sb.String()
}

func commitSummary(worktreePath, baseBranch string) []string {
	if baseBranch == "" {
		return nil
	}
	refs := []string{
		fmt.Sprintf("%s..HEAD", baseBranch),
		fmt.Sprintf("origin/%s..HEAD", baseBranch),
	}

	for _, ref := range refs {
		cmd := exec.Command("git", "-C", worktreePath, "log", "--oneline", "--no-decorate", ref)
		output, err := cmd.Output()
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		var summary []string
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, " ", 2)
			if len(parts) == 2 {
				summary = append(summary, parts[1])
			}
			if len(summary) >= 10 {
				break
			}
		}
		if len(summary) > 0 {
			return summary
		}
	}

	return nil
}
