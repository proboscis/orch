package multiplexer

import (
	"os"
	"strings"

	"github.com/s22625/orch/internal/executor"
)

func sessionEnv(exec executor.Executor, extra []string) []string {
	filtered := sessionExtraEnv(exec, extra)
	if _, ok := exec.(*executor.SSHExecutor); ok {
		return filtered
	}

	env := append([]string{}, os.Environ()...)
	return append(env, filtered...)
}

func sessionExtraEnv(exec executor.Executor, extra []string) []string {
	filtered := make([]string, 0, len(extra))
	_, isSSH := exec.(*executor.SSHExecutor)
	for _, entry := range extra {
		entry = strings.TrimSpace(entry)
		switch {
		case entry == "":
			continue
		case isSSH && strings.HasPrefix(entry, "HOME="):
			continue
		case isSSH && strings.HasPrefix(entry, "PATH="):
			continue
		default:
			filtered = append(filtered, entry)
		}
	}
	return filtered
}
