package cli

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/proboscis/orch/internal/daemon"
	buildversion "github.com/proboscis/orch/internal/version"
)

type fakeVersionPinger struct {
	version string
	err     error
	calls   int
}

func (f *fakeVersionPinger) PingStatus() (*daemon.PingResponse, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &daemon.PingResponse{OK: true, Version: f.version}, nil
}

func withBuildVersion(t *testing.T, version, commit, buildDate string) {
	t.Helper()
	origVersion := buildversion.Version
	origCommit := buildversion.Commit
	origBuildDate := buildversion.BuildDate
	t.Cleanup(func() {
		buildversion.Version = origVersion
		buildversion.Commit = origCommit
		buildversion.BuildDate = origBuildDate
	})

	buildversion.Version = version
	buildversion.Commit = commit
	buildversion.BuildDate = buildDate
}

func resetDaemonVersionMismatchWarning(t *testing.T) {
	t.Helper()
	orig := daemonVersionMismatchWarned
	daemonVersionMismatchWarned = false
	t.Cleanup(func() {
		daemonVersionMismatchWarned = orig
	})
}

func TestPrintVersionIncludesBuildMetadata(t *testing.T) {
	withBuildVersion(t, "test-version", "abc123", "2026-07-07T00:00:00Z")

	out := captureStdout(t, func() {
		printVersion()
	})

	for _, want := range []string{
		"version test-version",
		"commit abc123",
		"build_date 2026-07-07T00:00:00Z",
		"goos/goarch " + runtime.GOOS + "/" + runtime.GOARCH,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("version output missing %q:\n%s", want, out)
		}
	}
}

func TestPingDaemonWithVersionCheckWarnsOnceOnMismatch(t *testing.T) {
	withBuildVersion(t, "cli-version", "abc123", "2026-07-07T00:00:00Z")
	resetDaemonVersionMismatchWarning(t)
	pinger := &fakeVersionPinger{version: "daemon-version"}

	errOut := captureStderr(t, func() {
		if err := pingDaemonWithVersionCheck(pinger); err != nil {
			t.Fatalf("first ping error = %v", err)
		}
		if err := pingDaemonWithVersionCheck(pinger); err != nil {
			t.Fatalf("second ping error = %v", err)
		}
	})

	want := "warning: orch CLI cli-version / daemon daemon-version version mismatch - run 'orch daemon-restart'"
	if count := strings.Count(errOut, want); count != 1 {
		t.Fatalf("warning count = %d, want 1; stderr:\n%s", count, errOut)
	}
	if pinger.calls != 2 {
		t.Fatalf("ping calls = %d, want 2", pinger.calls)
	}
}

func TestPingDaemonWithVersionCheckPropagatesPingError(t *testing.T) {
	resetDaemonVersionMismatchWarning(t)
	pinger := &fakeVersionPinger{err: errors.New("boom")}

	errOut := captureStderr(t, func() {
		err := pingDaemonWithVersionCheck(pinger)
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("error = %v, want boom", err)
		}
	})

	if errOut != "" {
		t.Fatalf("stderr = %q, want empty", errOut)
	}
}
