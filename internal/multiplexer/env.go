package multiplexer

import (
	"os"
	"strings"

	"github.com/proboscis/orch/internal/executor"
)

var tmuxControlEnvVars = []string{"TMUX", "TMUX_PANE"}
var zellijControlEnvVars = []string{"ZELLIJ", "ZELLIJ_SESSION_NAME"}
var multiplexerControlEnvVars = append(append([]string{}, tmuxControlEnvVars...), zellijControlEnvVars...)

func sessionEnv(exec executor.Executor, extra []string) []string {
	filtered := sessionExtraEnv(exec, extra)
	if _, ok := exec.(*executor.SSHExecutor); ok {
		return filtered
	}

	env := scrubEnvEntries(os.Environ(), multiplexerControlEnvVars)
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

func tmuxControlOptions(exec executor.Executor, opts executor.RunOptions) executor.RunOptions {
	opts.Env = controlCommandEnv(exec, opts.Env, tmuxControlEnvVars)
	return opts
}

func zellijControlOptions(exec executor.Executor, opts executor.RunOptions) executor.RunOptions {
	opts.Env = controlCommandEnv(exec, opts.Env, zellijControlEnvVars)
	return opts
}

func controlCommandEnv(exec executor.Executor, env []string, unset []string) []string {
	if _, ok := exec.(*executor.SSHExecutor); ok {
		return sshEnvWithUnsets(env, unset)
	}
	if len(env) == 0 {
		env = os.Environ()
	}
	return scrubEnvEntries(env, unset)
}

func sshEnvWithUnsets(env []string, unset []string) []string {
	filtered := make([]string, 0, len(unset)*2+len(env))
	for _, key := range unset {
		filtered = append(filtered, "-u", key)
	}
	for _, entry := range env {
		entry = strings.TrimSpace(entry)
		if entry == "" || envEntryHasAnyKey(entry, unset) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func scrubEnvEntries(env []string, keys []string) []string {
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.TrimSpace(entry) == "" || envEntryHasAnyKey(entry, keys) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func envEntryHasAnyKey(entry string, keys []string) bool {
	for _, key := range keys {
		if strings.HasPrefix(entry, key+"=") {
			return true
		}
	}
	return false
}
