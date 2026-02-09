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

	api, err := getAPI()
	if err != nil {
		return err
	}

	issuesRoot, err := getIssuesRoot()
	if err != nil {
		return err
	}

	var path string

	ref, err := orchapi.ParseRunRef(refStr)
	if err != nil {
		return err
	}

	if ref.IsShortID() {
		run, err := api.ResolveRun(ctx, ref)
		if err == nil {
			path = filepath.Join(issuesRoot, "runs", run.IssueID, run.RunID+".md")
		} else if len(refStr) == 6 {
			return err
		}
	}

	if path == "" {
		if ref.IsLatest() {
			issue, err := api.GetIssue(ctx, ref.IssueID)
			if err == nil && issue.Path != "" {
				path = issue.Path
			} else if err == nil {
				path = filepath.Join(issuesRoot, "issues", issue.ID+".md")
			} else {
				run, err := api.GetLatestRun(ctx, ref.IssueID)
				if err != nil {
					return fmt.Errorf("not found: %s", refStr)
				}
				path = filepath.Join(issuesRoot, "runs", run.IssueID, run.RunID+".md")
			}
		} else {
			run, err := api.GetRun(ctx, ref.IssueID, ref.RunID)
			if err != nil {
				os.Exit(ExitRunNotFound)
				return err
			}
			path = filepath.Join(issuesRoot, "runs", run.IssueID, run.RunID+".md")
		}
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
