package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/s22625/orch/internal/orchapi"
	"github.com/spf13/cobra"
)

type openOptions struct {
	App       string
	PrintPath bool
}

func newOpenCmd() *cobra.Command {
	opts := &openOptions{}

	cmd := &cobra.Command{
		Use:   "open ISSUE_ID|RUN_REF",
		Short: "Open issue or run in editor",
		Long: `Open an issue or run document in Obsidian or your default editor.

Examples:
  orch open plc124           # Open issue
  orch open plc124#20231220  # Open specific run`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOpen(args[0], opts)
		},
	}

	cmd.Flags().StringVar(&opts.App, "app", "default", "App to open with (obsidian|editor|default)")
	cmd.Flags().BoolVar(&opts.PrintPath, "print-path", false, "Just print the path without opening")

	return cmd
}

func runOpen(refStr string, opts *openOptions) error {
	ctx := context.Background()

	api, err := getAPIForListing()
	if err != nil {
		return err
	}

	issuesRoot := ""
	if projectRoot, _, rootErr := getProjectRootWithSource(); rootErr == nil && projectRoot != "" {
		if resolvedRoot, issuesErr := getIssuesRootForProject(projectRoot); issuesErr == nil {
			issuesRoot = resolvedRoot
		}
	}

	var path string

	ref, err := orchapi.ParseRunRef(refStr)
	if err != nil {
		return err
	}

	if ref.IsShortID() {
		run, err := api.ResolveRun(ctx, ref)
		if err == nil {
			path = deriveRunDocPath(ctx, api, run, issuesRoot)
		} else if len(refStr) == 6 {
			return err
		}
	}

	if path == "" {
		if ref.IsLatest() {
			issue, err := api.GetIssue(ctx, ref.IssueID)
			if err == nil && issue.Path != "" {
				path = issue.Path
			} else if err == nil && issuesRoot != "" {
				path = filepath.Join(issuesRoot, "issues", issue.ID.String()+".md")
			} else {
				run, err := api.GetLatestRun(ctx, ref.IssueID)
				if err != nil {
					return fmt.Errorf("not found: %s", refStr)
				}
				path = deriveRunDocPath(ctx, api, run, issuesRoot)
				if path == "" {
					return fmt.Errorf("run document path unavailable for %s#%s", run.IssueID, run.RunID)
				}
			}
		} else {
			run, err := api.GetRun(ctx, ref.IssueID, ref.RunID)
			if err != nil {
				os.Exit(ExitRunNotFound)
				return err
			}
			path = deriveRunDocPath(ctx, api, run, issuesRoot)
			if path == "" {
				return fmt.Errorf("run document path unavailable for %s#%s", run.IssueID, run.RunID)
			}
		}
	}

	if issuesRoot == "" {
		issuesRoot = deriveIssuesRootFromPath(path)
	}

	if globalOpts.JSON {
		output := struct {
			OK   bool   `json:"ok"`
			Path string `json:"path"`
		}{
			OK:   true,
			Path: path,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(output)
	}

	if opts.PrintPath {
		fmt.Println(path)
		return nil
	}

	return openFile(path, opts.App, issuesRoot)
}

func deriveRunDocPath(ctx context.Context, api orchapi.OrchAPI, run *orchapi.Run, issuesRoot string) string {
	if run == nil {
		return ""
	}
	if issuesRoot != "" {
		return filepath.Join(issuesRoot, "runs", run.IssueID.String(), run.RunID.String()+".md")
	}

	issue, err := api.GetIssue(ctx, run.IssueID)
	if err != nil || issue == nil {
		return ""
	}

	derivedRoot := deriveIssuesRootFromPath(issue.Path)
	if derivedRoot == "" {
		return ""
	}

	return filepath.Join(derivedRoot, "runs", run.IssueID.String(), run.RunID.String()+".md")
}

func deriveIssuesRootFromPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") || strings.HasPrefix(trimmed, "obsidian://") {
		return ""
	}

	clean := filepath.Clean(trimmed)
	dir := filepath.Dir(clean)
	if filepath.Base(dir) == "issues" {
		return filepath.Dir(dir)
	}

	parent := filepath.Dir(dir)
	if filepath.Base(parent) == "runs" {
		return filepath.Dir(parent)
	}

	return ""
}

func openFile(path, app, issuesRoot string) error {
	switch app {
	case "obsidian":
		return openInObsidian(path, issuesRoot)
	case "editor":
		return openInEditor(path)
	case "default":
		// Try Obsidian first, fall back to system open
		if err := openInObsidian(path, issuesRoot); err != nil {
			return openWithSystem(path)
		}
		return nil
	default:
		return fmt.Errorf("unknown app: %s", app)
	}
}

func openInObsidian(path, issuesRoot string) error {
	if strings.TrimSpace(issuesRoot) == "" {
		return fmt.Errorf("issues root not available for obsidian link")
	}

	// Obsidian URI format: obsidian://open?vault=NAME&file=PATH
	// The path should be relative to the vault
	relPath := strings.TrimPrefix(path, issuesRoot)
	relPath = strings.TrimPrefix(relPath, "/")

	// Get vault name from path
	vaultName := filepath.Base(issuesRoot)

	uri := fmt.Sprintf("obsidian://open?vault=%s&file=%s", vaultName, relPath)
	return openWithSystem(uri)
}

func openInEditor(path string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}

	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func openWithSystem(path string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "linux":
		cmd = exec.Command("xdg-open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", path)
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	return cmd.Run()
}
