package agent

import (
	"strings"
)

// AgentDisplayName returns a shortened display name for an agent.
// For non-opencode agents, returns the agent name as-is.
// For opencode agents, returns a shortened form like "oc:opus4.5" based on model and variant.
//
// Examples:
//   - agent="opencode", model="anthropic/claude-opus-4-5", variant="max" → "oc:opus4.5"
//   - agent="opencode", model="anthropic/claude-opus-4-5", variant="high" → "oc:opus4.5h"
//   - agent="opencode", model="openai/gpt-5-2", variant="" → "oc:gpt5.2"
//   - agent="opencode", model="openai/gpt-5-2", variant="codex" → "oc:gpt5.2c"
//   - agent="opencode", model="google/gemini-3-pro", variant="" → "oc:gemini3-pro"
//   - agent="claude" → "claude"
func AgentDisplayName(agent, model, variant string) string {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return "-"
	}

	if agent != "opencode" {
		return agent
	}

	model = strings.TrimSpace(model)
	if model == "" {
		return "oc"
	}

	shortModel := shortenModelName(model)
	if shortModel == "" {
		return "oc"
	}

	variant = strings.TrimSpace(variant)
	suffix := variantSuffix(variant)

	return "oc:" + shortModel + suffix
}

func shortenModelName(model string) string {
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}

	model = strings.ToLower(model)

	if strings.HasPrefix(model, "claude-") {
		name := strings.TrimPrefix(model, "claude-")
		return formatModelVersion(name)
	}

	if strings.HasPrefix(model, "gpt-") {
		name := strings.TrimPrefix(model, "gpt-")
		return "gpt" + formatVersion(name)
	}

	if strings.HasPrefix(model, "o") && len(model) >= 2 {
		rest := model[1:]
		if len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
			return model
		}
	}

	if strings.HasPrefix(model, "gemini-") {
		name := strings.TrimPrefix(model, "gemini-")
		return "gemini" + formatVersion(name)
	}

	if len(model) > 15 {
		return model[:15]
	}
	return model
}

func formatModelVersion(name string) string {
	parts := strings.Split(name, "-")
	if len(parts) < 2 {
		return name
	}

	modelType := parts[0]
	version := formatVersion(strings.Join(parts[1:], "-"))

	return modelType + version
}

func formatVersion(version string) string {
	if version == "" {
		return ""
	}

	parts := strings.Split(version, "-")
	if len(parts) == 1 {
		return version
	}

	allNumeric := true
	for _, p := range parts {
		if !isNumeric(p) {
			allNumeric = false
			break
		}
	}

	if allNumeric {
		return strings.Join(parts, ".")
	}

	return version
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func variantSuffix(variant string) string {
	variant = strings.ToLower(strings.TrimSpace(variant))
	switch variant {
	case "", "max":
		return ""
	case "high":
		return "h"
	case "codex":
		return "c"
	case "mini":
		return "m"
	case "low":
		return "l"
	default:
		if len(variant) > 0 {
			return string(variant[0])
		}
		return ""
	}
}

const MaxAgentDisplayWidth = 15
