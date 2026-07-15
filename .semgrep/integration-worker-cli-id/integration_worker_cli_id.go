//go:build semgrepfixture

// Semgrep test fixture for integration-worker-cli-explicit-id and
// integration-worker-stop-all-host-sweep.
// Parsed by `semgrep test`, never compiled.
package fixture

func badWorkerStartDefaultID(t T, tmp, remoteAddr string) {
	// ruleid: integration-worker-cli-explicit-id
	runOrchOutsideRepo(t, tmp, "--remote", remoteAddr, "worker", "start", "--json")
}

func badWorkerStatusDefaultID(t T, tmp, remoteAddr string) {
	// ruleid: integration-worker-cli-explicit-id
	runOrchOutsideRepo(t, tmp, "--remote", remoteAddr, "worker", "status", "--json")
}

func badWorkerStopDefaultID(t T, tmp, remoteAddr string) {
	// ruleid: integration-worker-cli-explicit-id
	runOrchOutsideRepo(t, tmp, "--remote", remoteAddr, "worker", "stop", "--json")
}

func badWorkerStopAll(t T, tmp, remoteAddr string) {
	// ruleid: integration-worker-cli-explicit-id, integration-worker-stop-all-host-sweep
	runOrchOutsideRepo(t, tmp, "--remote", remoteAddr, "worker", "stop", "--all", "--json")
}

func badWorkerStartDirectExec(binary, remoteAddr string) {
	// ruleid: integration-worker-cli-explicit-id
	execCommand(binary, "--remote", remoteAddr, "worker", "start", "--json")
}

func okWorkerStartExplicitID(t T, tmp, remoteAddr, workerID string) {
	// ok: integration-worker-cli-explicit-id
	runOrchOutsideRepo(t, tmp, "--remote", remoteAddr, "worker", "start", "--worker-id", workerID, "--json")
}

func okWorkerStatusExplicitIDEquals(t T, tmp, remoteAddr, workerID string) {
	// ok: integration-worker-cli-explicit-id
	runOrchOutsideRepo(t, tmp, "--remote", remoteAddr, "worker", "status", "--worker-id="+workerID, "--json")
}

func okWorkerStopExplicitID(t T, tmp, remoteAddr, workerID string) {
	// ok: integration-worker-cli-explicit-id, integration-worker-stop-all-host-sweep
	runOrchOutsideRepo(t, tmp, "--remote", remoteAddr, "worker", "stop", "--worker-id", workerID, "--json")
}

func okNonWorkerCommand(t T, tmp string) {
	// ok: integration-worker-cli-explicit-id
	runOrchOutsideRepo(t, tmp, "ps", "--json")
}
