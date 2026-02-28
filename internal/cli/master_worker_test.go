package cli

import (
	"strings"
	"testing"
)

func TestRootRegistersMasterAndWorkerCommands(t *testing.T) {
	if rootCmd.Commands() == nil {
		t.Fatal("expected root commands to be initialized")
	}

	hasMaster := false
	hasWorker := false
	for _, cmd := range rootCmd.Commands() {
		switch cmd.Name() {
		case "master":
			hasMaster = true
		case "worker":
			hasWorker = true
		}
	}

	if !hasMaster {
		t.Fatal("expected root command to include 'master'")
	}
	if !hasWorker {
		t.Fatal("expected root command to include 'worker'")
	}
}

func TestRunWorkerStatusWithoutProjectRoot(t *testing.T) {
	setIsolatedXDG(t)
	resetGlobalOpts(t)

	out := captureStdout(t, func() {
		if err := runWorkerStatus(); err != nil {
			t.Fatalf("runWorkerStatus() error = %v", err)
		}
	})

	if !strings.Contains(out, "Status: not running") {
		t.Fatalf("expected not running output, got: %s", out)
	}
}
