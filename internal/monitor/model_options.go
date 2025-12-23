package monitor

import (
	"strings"

	"github.com/s22625/orch/internal/agent"
)

const (
	modelOptionDefault    = "default"
	modelOptionCustom     = "custom..."
	thinkingOptionDefault = "default"
	thinkingOptionCustom  = "custom..."
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

func thinkingOptionsForAgent(agentName string) []string {
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		agentName = defaultRunAgent()
	}

	aType, err := agent.ParseAgentType(agentName)
	if err != nil || aType != agent.AgentCodex {
		return nil
	}

	return []string{
		thinkingOptionDefault,
		"minimal",
		"low",
		"medium",
		"high",
		"xhigh",
		thinkingOptionCustom,
	}
}

func thinkingSelectionValue(selection string) (string, bool) {
	switch selection {
	case thinkingOptionDefault:
		return "", false
	case thinkingOptionCustom:
		return "", true
	default:
		return selection, false
	}
}
