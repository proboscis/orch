package daemon

import (
	"log"
	"os"
	"testing"
)

func TestDefaultWorkerIDUsesStableHostIdentity(t *testing.T) {
	orig := currentHostname
	currentHostname = func() (string, error) { return "zeus.example", nil }
	t.Cleanup(func() { currentHostname = orig })

	if got := defaultWorkerID(); got != "host-zeus.example" {
		t.Fatalf("defaultWorkerID() = %q, want %q", got, "host-zeus.example")
	}
}

func TestStartManagedExternalWorkerWithoutIDIsIdempotentPerHost(t *testing.T) {
	orig := currentHostname
	currentHostname = func() (string, error) { return "zeus", nil }
	t.Cleanup(func() { currentHostname = orig })

	logger := log.New(os.Stdout, "", 0)
	server := NewSocketServer(nil, logger)
	server.workerLaunchConfig = func(workerID string) (string, []string, []string, error) {
		return "/bin/sleep", []string{"/bin/sleep", "30"}, nil, nil
	}

	workerID1, pid1, err := server.startManagedExternalWorker("")
	if err != nil {
		t.Fatalf("first startManagedExternalWorker() error = %v", err)
	}
	workerID2, pid2, err := server.startManagedExternalWorker("")
	if err != nil {
		t.Fatalf("second startManagedExternalWorker() error = %v", err)
	}

	if workerID1 != "host-zeus" {
		t.Fatalf("workerID1 = %q, want %q", workerID1, "host-zeus")
	}
	if workerID2 != workerID1 {
		t.Fatalf("workerID2 = %q, want %q", workerID2, workerID1)
	}
	if pid2 != pid1 {
		t.Fatalf("pid2 = %d, want reuse pid %d", pid2, pid1)
	}

	_, _ = server.stopManagedExternalWorker(workerID1, false)
}
