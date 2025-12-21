package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/s22625/orch/internal/git"
	"github.com/spf13/cobra"
)

type execOptions struct {
	Env       []string // Additional environment variables (KEY=VALUE)
	NoOrchEnv bool     // Skip ORCH_* environment variables
	Shell     bool     // Run through sh -c
	Quiet     bool     // Suppress human-readable output
}

func newExecCmd() *cobra.Command {
	opts := &execOptions{}

	cmd := &cobra.Command{
		Use:   "exec RUN_REF -- COMMAND [ARGS...]",
		Short: "Execute a command in a run's worktree",
		Long: `Execute an arbitrary command within a specific run's worktree environment.

The command runs in the worktree directory with ORCH_* environment variables set.

Examples:
  # Run tests in a run's isolated worktree
  orch exec 66ff6 -- uv run pytest

  # Execute a script in run's environment
  orch exec 66ff6 -- uv run python new_impl_for_issue.py

  # Check git status in worktree
  orch exec orch-010 -- git status

  # Run with shell interpretation
  orch exec orch-010 --shell -- "echo $ORCH_ISSUE_ID && pwd"

  # Add custom environment variables
  orch exec orch-010 --env DEBUG=1 --env VERBOSE=true -- ./script.sh`,
		Args:               cobra.MinimumNArgs(1),
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Find the "--" separator
			dashIdx := -1
			for i, arg := range os.Args {
				if arg == "--" {
					dashIdx = i
					break
				}
			}

			if dashIdx == -1 || dashIdx >= len(os.Args)-1 {
				return fmt.Errorf("missing command after '--'")
			}

			// First positional arg before -- is the run ref
			runRef := args[0]
			// Everything after -- is the command to execute
			cmdArgs := os.Args[dashIdx+1:]

			return runExec(runRef, cmdArgs, opts)
		},
	}

	cmd.Flags().StringArrayVar(&opts.Env, "env", nil, "Additional environment variables (KEY=VALUE)")
	cmd.Flags().BoolVar(&opts.NoOrchEnv, "no-orch-env", false, "Skip ORCH_* environment variables")
	cmd.Flags().BoolVar(&opts.Shell, "shell", false, "Run command through sh -c")
	cmd.Flags().BoolVar(&opts.Quiet, "quiet", false, "Suppress human-readable output (script-friendly)")

	return cmd
}

func runExec(refStr string, cmdArgs []string, opts *execOptions) error {
	st, err := getStore()
	if err != nil {
		return err
	}

	// Resolve run by short ID or run ref
	run, err := resolveRun(st, refStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run not found: %s\n", refStr)
		os.Exit(ExitRunNotFound)
		return err
	}

	// Verify worktree exists
	if run.WorktreePath == "" {
		fmt.Fprintf(os.Stderr, "run has no worktree: %s\n", refStr)
		os.Exit(ExitWorktreeError)
		return fmt.Errorf("run has no worktree: %s", refStr)
	}

	// Resolve worktree path - may be stored as relative path
	worktreePath := run.WorktreePath
	if !filepath.IsAbs(worktreePath) {
		// Find main repo root (not worktree root) to resolve relative path
		repoRoot, err := git.FindMainRepoRoot("")
		if err != nil {
			return fmt.Errorf("could not find git repository: %w", err)
		}
		worktreePath = filepath.Join(repoRoot, worktreePath)
	}

	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "worktree does not exist: %s\n", worktreePath)
		os.Exit(ExitWorktreeError)
		return fmt.Errorf("worktree does not exist: %s", worktreePath)
	}

	// Get vault path for environment
	vaultPath, err := getVaultPath()
	if err != nil {
		return err
	}

	// Build environment
	env := os.Environ()

	// Add ORCH_* variables unless disabled
	if !opts.NoOrchEnv {
		orchEnv := []string{
			fmt.Sprintf("ORCH_ISSUE_ID=%s", run.IssueID),
			fmt.Sprintf("ORCH_RUN_ID=%s", run.RunID),
			fmt.Sprintf("ORCH_RUN_PATH=%s", run.Path),
			fmt.Sprintf("ORCH_WORKTREE_PATH=%s", worktreePath),
			fmt.Sprintf("ORCH_BRANCH=%s", run.Branch),
			fmt.Sprintf("ORCH_VAULT=%s", vaultPath),
		}
		env = append(env, orchEnv...)
	}

	// Add user-specified environment variables
	for _, e := range opts.Env {
		if !strings.Contains(e, "=") {
			return fmt.Errorf("invalid environment variable format (expected KEY=VALUE): %s", e)
		}
		env = append(env, e)
	}

	// Print info unless quiet
	if !opts.Quiet && !globalOpts.Quiet {
		fmt.Fprintf(os.Stderr, "Executing in %s#%s\n", run.IssueID, run.RunID)
		fmt.Fprintf(os.Stderr, "Worktree: %s\n", worktreePath)
	}

	// Build and execute command
	var execCmd *exec.Cmd
	if opts.Shell {
		// Run through shell
		shellCmd := strings.Join(cmdArgs, " ")
		execCmd = exec.Command("sh", "-c", shellCmd)
	} else {
		execCmd = exec.Command(cmdArgs[0], cmdArgs[1:]...)
	}

	execCmd.Dir = worktreePath
	execCmd.Env = env
	execCmd.Stdin = os.Stdin
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr

	// Run the command and pass through the exit code
	if err := execCmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}

	return nil
}
