package monitor

import (
	"fmt"
	"os"
	"strings"
)

const agentChatPromptTemplate = `You are the orch control agent for this repository.
You can run orch commands to manage issues and runs.

Protocol:
- When you need to request a command without running it, output a single line starting with "ORCH_CMD:" followed by the command.

Presets:
- Create issue: orch issue create <id> --title "<title>" --body "<summary/body>"
- List issues: orch issue list
- Start run: orch run <issue-id>
- List runs: orch ps --status running,blocked
- Stop run: orch stop <issue-id>#<run-id>
- Resolve run: orch resolve <issue-id>#<run-id>
- Open issue: orch open <issue-id>

Issue template:
---
type: issue
id: <issue-id>
title: <title>
summary: <one-line summary>
---
# <title>
<details>

Context:
- Vault: %s
- CWD: %s
`

func buildAgentChatPrompt(vaultPath string) string {
	cwd, _ := os.Getwd()
	return fmt.Sprintf(agentChatPromptTemplate, vaultPath, cwd)
}

func fallbackChatCommand(reason string) string {
	msg := "Agent chat unavailable"
	if strings.TrimSpace(reason) != "" {
		msg = fmt.Sprintf("Agent chat unavailable: %s", reason)
	}
	cmd := fmt.Sprintf("echo %s; exec ${SHELL:-sh}", shellQuote(msg))
	return fmt.Sprintf("sh -c %s", shellQuote(cmd))
}
