package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/s22625/orch/internal/orchapi"
	"github.com/spf13/cobra"
)

type waitOptions struct {
	Until   string
	Timeout int
}

type waitDeps struct {
	getAPI func() (orchapi.OrchAPI, error)
}

func defaultWaitDeps() *waitDeps {
	return &waitDeps{getAPI: getAPI}
}

func newWaitCmd() *cobra.Command {
	opts := &waitOptions{}

	cmd := &cobra.Command{
		Use:   "wait <RUN_REF>",
		Short: "Block until a run reaches a status",
		Long: `Wait for a run to reach a target status.

The daemon owns the blocking wait and status polling. This command exits 0
when the target status is observed.

Examples:
  orch wait 2f668e --until pr_open --timeout 3600
  orch wait orch-123#20260412-101500 --until done
  orch wait orch-123 --until waiting`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWaitWithDeps(cmd.Context(), args[0], opts, defaultWaitDeps())
		},
	}

	cmd.Flags().StringVar(&opts.Until, "until", "", "Target status to wait for (pr_open|done|waiting|failed)")
	cmd.Flags().IntVar(&opts.Timeout, "timeout", 0, "Maximum seconds to wait (0 means no timeout)")
	_ = cmd.MarkFlagRequired("until")

	return cmd
}

func runWait(refStr string, opts *waitOptions) error {
	return runWaitWithDeps(context.Background(), refStr, opts, defaultWaitDeps())
}

func runWaitWithDeps(ctx context.Context, refStr string, opts *waitOptions, deps *waitDeps) error {
	if opts == nil {
		opts = &waitOptions{}
	}
	if deps == nil {
		deps = defaultWaitDeps()
	}

	until, err := parseWaitUntilStatus(opts.Until)
	if err != nil {
		return err
	}
	if opts.Timeout < 0 {
		return fmt.Errorf("--timeout must be >= 0")
	}

	ref, err := orchapi.ParseRunRef(refStr)
	if err != nil {
		return err
	}

	api, err := deps.getAPI()
	if err != nil {
		return err
	}

	run, err := api.WaitForStatus(ctx, ref, until, time.Duration(opts.Timeout)*time.Second)
	if err != nil {
		if errors.Is(err, orchapi.ErrNotFound) {
			fmt.Fprintf(os.Stderr, "run not found: %s\n", refStr)
			os.Exit(ExitRunNotFound)
			return err
		}
		return err
	}

	if globalOpts.JSON {
		result := map[string]any{
			"ok":       true,
			"issue_id": run.IssueID,
			"run_id":   run.RunID,
			"status":   run.Status,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	if !globalOpts.Quiet {
		fmt.Printf("matched: %s#%s is %s\n", run.IssueID, run.RunID, run.Status)
	}

	return nil
}

func parseWaitUntilStatus(raw string) (orchapi.RunStatus, error) {
	switch strings.TrimSpace(raw) {
	case string(orchapi.RunStatusPROpen):
		return orchapi.RunStatusPROpen, nil
	case string(orchapi.RunStatusDone):
		return orchapi.RunStatusDone, nil
	case string(orchapi.RunStatusWaiting):
		return orchapi.RunStatusWaiting, nil
	case string(orchapi.RunStatusFailed):
		return orchapi.RunStatusFailed, nil
	default:
		return "", fmt.Errorf("--until must be one of: pr_open, done, waiting, failed")
	}
}
