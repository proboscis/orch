package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/s22625/orch/internal/model"
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
	st, err := getStore()
	if err != nil {
		return err
	}

	var path string

	// Try as short ID first
	if shortIDRegex.MatchString(refStr) {
		run, err := st.GetRunByShortID(refStr)
		if err == nil {
			path = run.Path
		}
	}

	if path == "" {
		// Try to parse as run ref
		ref, err := model.ParseRunRef(refStr)
		if err != nil {
			return err
		}

		if ref.IsLatest() {
			// Could be either issue or latest run
			// First try issue
			issue, err := st.ResolveIssue(ref.IssueID)
			if err == nil {
				path = issue.Path
			} else {
				// Try as latest run
				run, err := st.GetLatestRun(ref.IssueID)
				if err != nil {
					return fmt.Errorf("not found: %s", refStr)
				}
				path = run.Path
			}
		} else {
			// Specific run
			run, err := st.GetRun(ref)
			if err != nil {
				os.Exit(ExitRunNotFound)
				return err
			}
			path = run.Path
		}
	}

	// Output
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

	// Open the file
	return openFile(path, opts.App, st.VaultPath())
}

func openFile(path, app, vaultPath string) error {
	switch app {
	case "obsidian":
		return openInObsidian(path, vaultPath)
	case "editor":
		return openInEditor(path)
	case "default":
		// Try Obsidian first, fall back to system open
		if err := openInObsidian(path, vaultPath); err != nil {
			return openWithSystem(path)
		}
		return nil
	default:
		return fmt.Errorf("unknown app: %s", app)
	}
}

func openInObsidian(path, vaultPath string) error {
	// Obsidian URI format: obsidian://open?vault=NAME&file=PATH
	// The path should be relative to the vault
	relPath := strings.TrimPrefix(path, vaultPath)
	relPath = strings.TrimPrefix(relPath, "/")

	// Get vault name from path
	vaultName := filepath.Base(vaultPath)

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
