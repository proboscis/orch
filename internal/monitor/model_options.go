package monitor

import (
	"strings"

	"github.com/s22625/orch/internal/agent"
)

const (
	modelOptionDefault = "default"
	modelOptionCustom  = "custom..."
)

func defaultRunAgent() string {
	return string(agent.AgentClaude)
}

func modelOptionsForAgent(agentName string) []string {
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		agentName = defaultRunAgent()
	}

	var models []string
	if aType, err := agent.ParseAgentType(agentName); err == nil {
		models = agent.KnownModels(aType)
	}

	options := make([]string, 0, len(models)+2)
	options = append(options, modelOptionDefault)
	options = append(options, models...)
	options = append(options, modelOptionCustom)
	return options
}

func modelSelectionValue(selection string) (string, bool) {
	switch selection {
	case modelOptionDefault:
		return "", false
	case modelOptionCustom:
		return "", true
	default:
		return selection, false
	}
}
