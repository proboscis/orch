package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/orchapi"
)

type stopTestAPI struct {
	orchapi.OrchAPI
	result    *orchapi.StopRunResult
	err       error
	forceSeen bool
}

func (a *stopTestAPI) ResolveRun(context.Context, orchapi.RunRef) (*orchapi.Run, error) {
	return &orchapi.Run{IssueID: model.IssueID("stop-issue"), RunID: model.RunID("20260713-195500")}, nil
}

func (a *stopTestAPI) StopRun(_ context.Context, _ orchapi.RunRef, force bool) (*orchapi.StopRunResult, error) {
	a.forceSeen = force
	return a.result, a.err
}

func TestRunStopReturnsSessionKillError(t *testing.T) {
	resetGlobalOpts(t)
	globalOpts.Quiet = true
	wantErr := errors.New("daemon error: kill session failed")
	api := &stopTestAPI{err: wantErr}
	deps := &stopDeps{getAPI: func() (orchapi.OrchAPI, error) { return api, nil }}

	err := runStopWithDeps(context.Background(), "stop-issue#20260713-195500", &stopOptions{}, deps)
	if !errors.Is(err, wantErr) {
		t.Fatalf("runStopWithDeps() error = %v, want %v", err, wantErr)
	}
	if api.forceSeen {
		t.Fatal("default stop unexpectedly set force")
	}
}

func TestRunStopForceSurfacesKillWarning(t *testing.T) {
	resetGlobalOpts(t)
	globalOpts.Quiet = true
	api := &stopTestAPI{result: &orchapi.StopRunResult{
		Warning: "session kill failed for run stop-issue#20260713-195500; run marked canceled because --force",
	}}
	deps := &stopDeps{getAPI: func() (orchapi.OrchAPI, error) { return api, nil }}

	var stopErr error
	stderr := captureStderr(t, func() {
		stopErr = runStopWithDeps(context.Background(), "stop-issue#20260713-195500", &stopOptions{Force: true}, deps)
	})
	if stopErr != nil {
		t.Fatalf("runStopWithDeps() error = %v", stopErr)
	}
	if !api.forceSeen {
		t.Fatal("forced stop did not pass force to daemon API")
	}
	for _, want := range []string{"warning:", "session kill failed", "marked canceled because --force"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr = %q, want substring %q", stderr, want)
		}
	}
}
