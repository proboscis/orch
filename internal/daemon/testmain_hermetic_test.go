package daemon

import (
	"fmt"
	"os"
	"testing"
)

// TestMain pins the git environment for every test in this package.
//
// The verification machine receives the sources as a plain copy of the tracked
// files: no `.git` around them, no GitHub credentials, and whatever git
// configuration belongs to the person who owns the machine. A test whose verdict
// moves with any of those is measuring the machine, not this package. The
// settings below remove all three inputs once, here, instead of guarding each
// site that shells out to git today:
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
func TestMain(m *testing.M) {
	hermeticGitEnv := map[string]string{
		"GIT_ALLOW_PROTOCOL":  "file",
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_SYSTEM":   os.DevNull,
		"GIT_TERMINAL_PROMPT": "0",
	}
	for name, value := range hermeticGitEnv {
		if err := os.Setenv(name, value); err != nil {
			fmt.Fprintf(os.Stderr, "failed to pin %s for hermetic git: %v\n", name, err)
			os.Exit(2)
		}
	}

	os.Exit(m.Run())
}
