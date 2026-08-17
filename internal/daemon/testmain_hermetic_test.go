package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// hermeticStateMarker records that this process tree already has its state
// directories pinned.
//
// TestExternalWorkerHelperProcess re-execs the test binary as a worker helper,
// and that helper connects to the daemon socket the parent test created — which
// lives under the parent's XDG_RUNTIME_DIR and reaches the child through
// os.Environ(). Pinning unconditionally would hand the child a directory of its
// own, it would find no socket, and every helper-process test would fail. So the
// pin happens once, in the outermost binary, and every child inherits it.
const hermeticStateMarker = "ORCH_TEST_HERMETIC_STATE"

// TestMain pins the git and state environment for every test in this package.
//
// The verification machine receives the sources as a plain copy of the tracked
// files: no `.git` around them, no GitHub credentials, whatever git configuration
// belongs to the person who owns the machine, and a live orch daemon with real run
// state. A test whose verdict moves with any of those is measuring the machine,
// not this package. Pinning them once, here, is what keeps that true for tests
// that have not been written yet — the alternative is guarding each site that
// shells out to git or touches state, which only ever covers the sites someone
// remembered.
func TestMain(m *testing.M) {
	pinHermeticGitEnv()
	releaseState := pinHermeticStateEnv()

	code := m.Run()

	releaseState()
	os.Exit(code)
}

// pinHermeticGitEnv removes the three machine-supplied git inputs:
//
//   - GIT_ALLOW_PROTOCOL=file — git refuses every transport except local paths,
//     so a fixture repository that names a remote the test does not own fails
//     immediately and offline. Without it, CreateWorktree's pre-add fetch talks
//     to github.com, which on a credential-less machine stalls on
//     "could not read Username for 'https://github.com'".
//   - GIT_CONFIG_GLOBAL / GIT_CONFIG_SYSTEM=/dev/null — the developer's own
//     config (url rewrites, signing, default branch, hooks) stops leaking in.
//     This is why the same fixture failed differently on two machines: a global
//     `insteadOf` rewrote https to ssh on one of them.
//   - GIT_TERMINAL_PROMPT=0 — no git invocation may ever wait for a human.
//
// Fixtures may still use local-path remotes (initGitRepoWithCommit points origin
// at the repository itself), which is what "file" keeps allowed.
func pinHermeticGitEnv() {
	setOrDie(map[string]string{
		"GIT_ALLOW_PROTOCOL":  "file",
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_SYSTEM":   os.DevNull,
		"GIT_TERMINAL_PROMPT": "0",
	})
}

// pinHermeticStateEnv moves the XDG directories to a directory owned by this test
// binary, so no test can read the run and issue state of the daemon actually
// running on the machine, and none can write into it either. setupXDGTestEnv
// still narrows this per test where cross-test independence matters; the
// difference is that a test which forgets to call it now lands in the binary's own
// directory instead of the operator's live state.
//
// The returned function removes the directory, and does nothing when this process
// inherited the pin from its parent (see hermeticStateMarker) — the parent owns
// what it created.
func pinHermeticStateEnv() func() {
	if os.Getenv(hermeticStateMarker) != "" {
		return func() {}
	}

	stateRoot, err := os.MkdirTemp("", "orch-daemon-tests-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create hermetic state root: %v\n", err)
		os.Exit(2)
	}

	setOrDie(map[string]string{
		hermeticStateMarker: stateRoot,
		"XDG_RUNTIME_DIR":   stateRoot,
		"XDG_STATE_HOME":    filepath.Join(stateRoot, "state"),
		"XDG_DATA_HOME":     filepath.Join(stateRoot, "data"),
		"XDG_CONFIG_HOME":   filepath.Join(stateRoot, "config"),
		"XDG_CACHE_HOME":    filepath.Join(stateRoot, "cache"),
	})

	return func() { _ = os.RemoveAll(stateRoot) }
}

// setOrDie exits rather than run the suite with a half-applied environment: a
// partially pinned process is exactly the machine-dependent state these pins
// exist to remove.
func setOrDie(env map[string]string) {
	for name, value := range env {
		if err := os.Setenv(name, value); err != nil {
			fmt.Fprintf(os.Stderr, "failed to pin %s for hermetic tests: %v\n", name, err)
			os.Exit(2)
		}
	}
}
