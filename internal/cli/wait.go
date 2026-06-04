package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/s22625/orch/internal/orchapi"
	"github.com/spf13/cobra"
)

const waitExitTimeout = 124

type waitOptions struct {
	Timeout int
}

type waitAPI interface {
	WaitForRuns(ctx context.Context, refs []string, timeoutSeconds int) (*orchapi.WaitForRunsResult, error)
}

type waitDeps struct {
	getAPI func() (waitAPI, error)
	stdout io.Writer
	stderr io.Writer
	exit   func(int)
}

type waitCommandResult struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
	Issue  string `json:"issue"`
	PRURL  string `json:"pr_url"`
}

func defaultWaitDeps() *waitDeps {
	return &waitDeps{
		getAPI: func() (waitAPI, error) {
			return getAPIForListing()
		},
		stdout: os.Stdout,
		stderr: os.Stderr,
		exit:   os.Exit,
	}
}

func newWaitCmd() *cobra.Command {
	opts := &waitOptions{}

	cmd := &cobra.Command{
		Use:   "wait RUN_REF [RUN_REF...]",
		Short: "Block until any specified run needs attention",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWait(args, opts)
		},
	}

	cmd.Flags().IntVar(&opts.Timeout, "timeout", 0, "Timeout in seconds (0=unlimited)")

	return cmd
}

func runWait(refs []string, opts *waitOptions) error {
	return runWaitWithDeps(context.Background(), refs, opts, defaultWaitDeps())
}

func runWaitWithDeps(ctx context.Context, refs []string, opts *waitOptions, deps *waitDeps) error {
	if opts.Timeout < 0 {
		err := fmt.Errorf("--timeout must be >= 0")
		fmt.Fprintln(deps.stderr, err)
		deps.exit(1)
		return err
	}

	api, err := deps.getAPI()
	if err != nil {
		fmt.Fprintln(deps.stderr, err)
		deps.exit(1)
		return err
	}

	result, err := api.WaitForRuns(ctx, refs, opts.Timeout)
	if err != nil {
		fmt.Fprintln(deps.stderr, err)
		if errors.Is(err, orchapi.ErrTimeout) {
			deps.exit(waitExitTimeout)
		} else {
			deps.exit(1)
		}
		return err
	}

	return json.NewEncoder(deps.stdout).Encode(waitCommandResult{
		RunID:  string(result.RunID),
		Status: string(result.Status),
		Issue:  string(result.IssueID),
		PRURL:  result.PRURL,
	})
}
