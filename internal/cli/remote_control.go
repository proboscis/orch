package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/proboscis/orch/internal/orchapi"
)

var currentControlHostname = os.Hostname

func isLocalControlHost(targetHost string) bool {
	targetHost = strings.TrimSpace(targetHost)
	if targetHost == "" {
		return false
	}
	if targetHost == "localhost" || targetHost == "127.0.0.1" || targetHost == "::1" {
		return true
	}
	if strings.Contains(targetHost, "@") {
		return false
	}
	host, _ := currentControlHostname()
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	short := strings.Split(host, ".")[0]
	targetShort := strings.Split(targetHost, ".")[0]
	return strings.EqualFold(targetHost, host) || strings.EqualFold(targetShort, short)
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func sshScriptArgs(targetHost string, tty bool, script string) []string {
	args := []string{}
	if tty {
		args = append(args, "-t")
	} else {
		args = append(args, "-T")
	}
	args = append(args, targetHost, "sh", "-lc", script)
	return args
}

func buildRemoteOpenCodeAttachScript(info *orchapi.AttachInfo) (string, error) {
	if info == nil {
		return "", fmt.Errorf("attach info required")
	}
	if info.ServerPort > 0 {
		args := []string{
			"exec", "opencode", "attach", shellQuote(fmt.Sprintf("http://127.0.0.1:%d", info.ServerPort)),
		}
		if strings.TrimSpace(info.OpenCodeSessionID) != "" {
			args = append(args, "--session", shellQuote(info.OpenCodeSessionID))
		}
		if strings.TrimSpace(info.WorktreePath) != "" {
			args = append(args, "--dir", shellQuote(info.WorktreePath))
		}
		return strings.Join(args, " "), nil
	}
	if strings.TrimSpace(info.OpenCodeSessionID) == "" || strings.TrimSpace(info.WorktreePath) == "" {
		return "", fmt.Errorf("no opencode server port and no session to resume")
	}
	return "cd " + shellQuote(info.WorktreePath) + " && exec opencode --session " + shellQuote(info.OpenCodeSessionID) + " " + shellQuote(info.WorktreePath), nil
}
