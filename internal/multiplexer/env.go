package multiplexer

import (
	"os"

	"github.com/s22625/orch/internal/executor"
)

func sessionEnv(exec executor.Executor, extra []string) []string {
	if _, ok := exec.(*executor.SSHExecutor); ok {
		filtered := make([]string, 0, len(extra))
		for _, entry := range extra {
			switch {
			case entry == "":
				continue
			case len(entry) > 5 && entry[:5] == "HOME=":
				continue
			case len(entry) > 5 && entry[:5] == "PATH=":
				continue
			default:
				filtered = append(filtered, entry)
			}
		}
		return filtered
	}

	env := append([]string{}, os.Environ()...)
	return append(env, extra...)
}
