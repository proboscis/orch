package worker

import (
	"os"
	"strings"
)

var workerMultiplexerEnvKeys = []string{"TMUX", "TMUX_PANE", "ZELLIJ", "ZELLIJ_SESSION_NAME"}

// ScrubInheritedMultiplexerEnv detaches worker startup from any shell UI
// multiplexer the operator happened to run the command inside.
func ScrubInheritedMultiplexerEnv() {
	for _, key := range workerMultiplexerEnvKeys {
		_ = os.Unsetenv(key)
	}
}

func scrubWorkerMultiplexerEnvEntries(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.TrimSpace(entry) == "" || workerEnvEntryHasAnyKey(entry, workerMultiplexerEnvKeys) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func workerEnvEntryHasAnyKey(entry string, keys []string) bool {
	for _, key := range keys {
		if strings.HasPrefix(entry, key+"=") {
			return true
		}
	}
	return false
}
